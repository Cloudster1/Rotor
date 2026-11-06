package otlpgrpc

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/traces/otlp"
)

// authorizationKey is the metadata an exporter's headers become; gRPC writes metadata keys in lower case.
const authorizationKey = "authorization"

// What the service answers a call it does not take, in a sentence an exporter's log shows.
var (
	sayTooLarge = fmt.Sprintf("The request is past %d MiB as sent or %d MiB unpacked. Send smaller batches, such as with "+
		"a lower OTEL_BSP_MAX_EXPORT_BATCH_SIZE.", config.MaxOTLPBodyBytes/config.Mebibyte, config.MaxOTLPDecodedBytes/config.Mebibyte)
	sayMalformed = "The message is not an OTLP ExportTraceServiceRequest. Send what an OpenTelemetry OTLP/gRPC exporter sends."
	sayRequests  = fmt.Sprintf("You sent more than %d requests in a minute. Send again after the retry delay, in larger batches.",
		config.OTLPRequestsPerMinute)
	saySpans = fmt.Sprintf("You sent more than %d spans in a minute. Send again after the retry delay, or sample fewer spans.",
		config.OTLPSpansPerMinute)
)

// traceService serves Export as a stream of one message each way, a unary call on the wire, so the token is checked
// before the message is read, and the message is parsed here, so one that does not parse is INVALID_ARGUMENT.
var traceService = grpc.ServiceDesc{
	ServiceName: coltracepb.TraceService_ServiceDesc.ServiceName,
	HandlerType: (*any)(nil),
	Streams: []grpc.StreamDesc{{
		StreamName: "Export",
		Handler: func(srv any, stream grpc.ServerStream) error {
			return srv.(*exporter).export(stream)
		},
	}},
	Metadata: coltracepb.TraceService_ServiceDesc.Metadata,
}

type exporter struct {
	intake Intake
	log    *slog.Logger
}

// export checks in the HTTP endpoint's order: the token, the requests a minute, the message's size and body, the
// spans a minute, then the spans one by one, which partial_success answers.
func (e *exporter) export(stream grpc.ServerStream) error {
	ctx := stream.Context()
	var authorization string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get(authorizationKey); len(values) > 0 {
			authorization = values[0]
		}
	}
	ctx, err := e.intake.OTLPSender(ctx, authorization)
	if err != nil {
		return e.refuse(err, sayRequests)
	}
	if err := e.intake.OTLPAdmit(ctx); err != nil {
		return e.refuse(err, sayRequests)
	}
	// grpc-go answers a message it could not read itself, past MaxOTLPDecodedBytes unpacked among them.
	var body rawMessage
	if err := stream.RecvMsg(&body); err != nil {
		return err
	}
	if sentBytes(ctx) > config.MaxOTLPBodyBytes {
		return status.Error(codes.ResourceExhausted, sayTooLarge)
	}
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(body, req); err != nil {
		return status.Error(codes.InvalidArgument, sayMalformed)
	}
	rejected, err := e.intake.OTLPTake(ctx, req)
	if err != nil {
		return e.refuse(err, saySpans)
	}
	resp := &coltracepb.ExportTraceServiceResponse{}
	if n := rejected.Total(); n > 0 {
		resp.PartialSuccess = &coltracepb.ExportTracePartialSuccess{RejectedSpans: int64(n), ErrorMessage: rejected.Sentence()}
	}
	return stream.SendMsg(resp)
}

// refuse answers a refusal with the code its HTTP status maps to, and a sender over its rate with RESOURCE_EXHAUSTED
// and the wait as RetryInfo, the one sign on which OTLP's exporters retry that code.
func (e *exporter) refuse(err error, busySays string) error {
	var refusal *otlp.Refusal
	var busy *otlp.Busy
	switch {
	case errors.As(err, &refusal):
		return status.Error(otlp.CodeOf(refusal.Status), refusal.Message)
	case errors.As(err, &busy):
		st, detailErr := status.New(codes.ResourceExhausted, busySays).
			WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(busy.Wait)})
		if detailErr != nil {
			return status.Error(codes.ResourceExhausted, busySays)
		}
		return st.Err()
	}
	e.log.Error("could not take an OTLP export over gRPC", "error", err)
	return status.Error(otlp.CodeOf(http.StatusInternalServerError), "Something went wrong on Rotor's side. Try again.")
}
