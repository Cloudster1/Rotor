package otlpgrpc

import (
	"google.golang.org/grpc/encoding"
	// The proto codec is registered by its package, which this one delegates to.
	_ "google.golang.org/grpc/encoding/proto"
	"google.golang.org/grpc/mem"
)

// rawMessage is a call's message unparsed, for the service to parse once the sender is let in: grpc-go answers a parse
// of its own that fails as INTERNAL before any handler could say otherwise.
type rawMessage []byte

// rawCodec keeps a rawMessage as it came and reads and writes every other message as protobuf.
type rawCodec struct{ proto encoding.CodecV2 }

func newRawCodec() rawCodec { return rawCodec{proto: encoding.GetCodecV2("proto")} }

func (c rawCodec) Marshal(v any) (mem.BufferSlice, error) { return c.proto.Marshal(v) }

func (c rawCodec) Unmarshal(data mem.BufferSlice, v any) error {
	if raw, ok := v.(*rawMessage); ok {
		*raw = data.Materialize()
		return nil
	}
	return c.proto.Unmarshal(data, v)
}

func (c rawCodec) Name() string { return c.proto.Name() }
