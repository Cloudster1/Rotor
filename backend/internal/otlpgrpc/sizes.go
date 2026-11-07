package otlpgrpc

import (
	"context"

	"google.golang.org/grpc/stats"
)

// sizes keeps what each call's message measured as sent, which grpc-go reports to a stats handler alone: its own
// limit holds the message as sent and unpacked to one size, and the HTTP endpoint's are two.
type sizes struct{}

// sent is one call's message as it came, before gzip was undone.
type sent struct{ bytes int }

type ctxKeySent struct{}

func (sizes) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return context.WithValue(ctx, ctxKeySent{}, &sent{})
}

func (sizes) HandleRPC(ctx context.Context, s stats.RPCStats) {
	if in, ok := s.(*stats.InPayload); ok {
		if at, ok := ctx.Value(ctxKeySent{}).(*sent); ok {
			at.bytes = in.CompressedLength
		}
	}
}

func (sizes) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }

func (sizes) HandleConn(context.Context, stats.ConnStats) {}

// sentBytes is the size of the call's message as sent, once it was read.
func sentBytes(ctx context.Context) int {
	if at, ok := ctx.Value(ctxKeySent{}).(*sent); ok {
		return at.bytes
	}
	return 0
}
