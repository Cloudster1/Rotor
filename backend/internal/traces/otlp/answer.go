package otlp

import (
	"net/http"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// MediaType is the Content-Type an answer in f is written with.
func (f Format) MediaType() string {
	if f == Protobuf {
		return MediaProtobuf
	}
	return MediaJSON
}

func (f Format) marshal(m proto.Message) ([]byte, error) {
	if f == Protobuf {
		return proto.Marshal(m)
	}
	return protojson.Marshal(m)
}

// Answer is the ExportTraceServiceResponse to a request that was read, with partial_success when some spans were
// not taken, so the client logs why and sends them no more.
func Answer(f Format, rejected Rejections) ([]byte, error) {
	resp := &coltracepb.ExportTraceServiceResponse{}
	if n := rejected.Total(); n > 0 {
		resp.PartialSuccess = &coltracepb.ExportTracePartialSuccess{RejectedSpans: int64(n), ErrorMessage: rejected.Sentence()}
	}
	return f.marshal(resp)
}

// statusCodes are the google.rpc codes that go with the endpoint's refusals, over HTTP and over gRPC (#262).
var statusCodes = map[int]codes.Code{
	http.StatusBadRequest:            codes.InvalidArgument,
	http.StatusUnauthorized:          codes.Unauthenticated,
	http.StatusForbidden:             codes.PermissionDenied,
	http.StatusRequestEntityTooLarge: codes.ResourceExhausted,
	http.StatusUnsupportedMediaType:  codes.InvalidArgument,
	http.StatusTooManyRequests:       codes.ResourceExhausted,
	http.StatusInternalServerError:   codes.Internal,
	http.StatusNotImplemented:        codes.Unimplemented,
	http.StatusServiceUnavailable:    codes.Unavailable,
}

// CodeOf is the google.rpc code of a refusal the endpoint answers over HTTP with httpStatus.
func CodeOf(httpStatus int) codes.Code {
	if code, ok := statusCodes[httpStatus]; ok {
		return code
	}
	return codes.Unknown
}

// Status is the google.rpc.Status OTLP answers a refused request with, saying in a sentence what to do.
func Status(f Format, httpStatus int, message string) ([]byte, error) {
	return f.marshal(&statuspb.Status{Code: int32(CodeOf(httpStatus)), Message: message})
}
