// Package otlpgrpc takes OTLP over gRPC (#262) beside OTLP/HTTP: the trace service alone, its own port, and the same
// tokens, limits, store and partial success as the HTTP endpoint, which it asks through Intake.
package otlpgrpc

import (
	"context"
	"log/slog"
	"net"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	// OTLP's exporters offer gzip; registering it lets the server read and answer it.
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// Intake is what the OTLP endpoint does for either transport; *httpapi.Server is the real one.
type Intake interface {
	// OTLPSender lets in who an authorization value names, or refuses with an *otlp.Refusal.
	OTLPSender(ctx context.Context, authorization string) (context.Context, error)
	// OTLPAdmit counts a request of the sender on ctx, or refuses with an *otlp.Refusal or an *otlp.Busy.
	OTLPAdmit(ctx context.Context) error
	// OTLPTake keeps a request's spans as the sender on ctx, or refuses with an *otlp.Busy.
	OTLPTake(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (otlp.Rejections, error)
}

// Serve takes OTLP over gRPC on addr until ctx ends. The port carries no TLS, as the api's HTTP port carries none:
// it ends at the Ingress or the load balancer in front, since no SDK trusts the runner port's OpenBao certificate.
func Serve(ctx context.Context, addr string, intake Intake, log *slog.Logger) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	server := NewServer(intake, log)
	go func() {
		<-ctx.Done()
		server.GracefulStop()
	}()
	log.Info("OTLP over gRPC listening", "addr", addr)
	return server.Serve(listener)
}

// NewServer is the gRPC server with the trace service registered and nothing else, for Serve and for tests.
func NewServer(intake Intake, log *slog.Logger) *grpc.Server {
	server := grpc.NewServer(
		// A message unpacked past the HTTP endpoint's limit is refused by grpc-go before it is held; the limit as sent
		// is checked from the stats.
		grpc.MaxRecvMsgSize(config.MaxOTLPDecodedBytes),
		grpc.MaxConcurrentStreams(config.OTLPGRPCMaxStreams),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionAge: config.OTLPGRPCConnectionAge, MaxConnectionAgeGrace: config.OTLPGRPCConnectionGrace,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: config.RunnerKeepaliveMin, PermitWithoutStream: true}),
		grpc.StatsHandler(sizes{}),
		grpc.ForceServerCodecV2(newRawCodec()),
		grpc.UnknownServiceHandler(func(any, grpc.ServerStream) error {
			return status.Error(codes.Unimplemented, otlp.SayTracesAlone)
		}),
	)
	server.RegisterService(&traceService, &exporter{intake: intake, log: log})
	return server
}
