// Package otlp reads what a LangChain app or a tool posts to the OTLP/HTTP endpoint (#243): the body in either of
// OTLP's encodings, the checks on each span, the answers OTLP clients expect and the per-sender rates.
package otlp

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"strings"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Format is the encoding a request came in, which its answer is written in too.
type Format int

const (
	Protobuf Format = iota
	JSON
)

// The media types OTLP/HTTP sends, and the encodings of a body Rotor unpacks.
const (
	MediaProtobuf = "application/x-protobuf"
	MediaJSON     = "application/json"
	encodingGzip  = "gzip"
	encodingNone  = "identity"
)

// What a body can be refused for, before any span in it is looked at.
var (
	ErrMediaType = errors.New("the body is neither application/x-protobuf nor application/json")
	ErrEncoding  = errors.New("the body is encoded neither with gzip nor not at all")
	ErrTooLarge  = errors.New("the body is past what one request may carry")
	ErrMalformed = errors.New("the body is not an ExportTraceServiceRequest")
)

// FormatOf reads a request's Content-Type and Content-Encoding, or refuses either.
func FormatOf(contentType, contentEncoding string) (Format, error) {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return JSON, ErrMediaType
	}
	var f Format
	switch media {
	case MediaProtobuf:
		f = Protobuf
	case MediaJSON:
		f = JSON
	default:
		return JSON, ErrMediaType
	}
	if enc := strings.ToLower(strings.TrimSpace(contentEncoding)); enc != "" && enc != encodingNone && enc != encodingGzip {
		return f, ErrEncoding
	}
	return f, nil
}

// Read takes a body as sent, reading no more than config.MaxOTLPBodyBytes of it, and unpacks it to at most
// config.MaxOTLPDecodedBytes, so neither a large body nor a packed one is held whole.
func Read(body io.Reader, contentEncoding string) ([]byte, error) {
	sent, err := io.ReadAll(io.LimitReader(body, config.MaxOTLPBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(sent) > config.MaxOTLPBodyBytes {
		return nil, ErrTooLarge
	}
	if !strings.EqualFold(strings.TrimSpace(contentEncoding), encodingGzip) {
		return sent, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(sent))
	if err != nil {
		return nil, ErrMalformed
	}
	unpacked, err := io.ReadAll(io.LimitReader(zr, config.MaxOTLPDecodedBytes+1))
	if err != nil {
		return nil, ErrMalformed
	}
	if len(unpacked) > config.MaxOTLPDecodedBytes {
		return nil, ErrTooLarge
	}
	return unpacked, nil
}

// Decode reads an ExportTraceServiceRequest in either encoding. OTLP's JSON writes trace and span ids as hex where
// proto3's JSON would write base64, and its receivers ignore fields they do not know.
func Decode(f Format, body []byte) (*coltracepb.ExportTraceServiceRequest, error) {
	req := &coltracepb.ExportTraceServiceRequest{}
	if f == Protobuf {
		if err := proto.Unmarshal(body, req); err != nil {
			return nil, ErrMalformed
		}
		return req, nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	// Numbers stay as written, so a timestamp in nanoseconds keeps every digit on its way through.
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, ErrMalformed
	}
	if _, ok := tree.(map[string]any); !ok {
		return nil, ErrMalformed
	}
	hexToBase64(tree)
	rewritten, err := json.Marshal(tree)
	if err != nil {
		return nil, ErrMalformed
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(rewritten, req); err != nil {
		return nil, ErrMalformed
	}
	return req, nil
}

// idFields are the fields OTLP's JSON writes as hex, in both of the spellings proto3's JSON reads.
var idFields = map[string]bool{"traceId": true, "trace_id": true, "spanId": true, "span_id": true,
	"parentSpanId": true, "parent_span_id": true}

// hexToBase64 rewrites the ids in a decoded JSON body as proto3's JSON reads bytes. An id that is not hex becomes
// empty, so its span fails the id check alone rather than costing the whole request.
func hexToBase64(node any) {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if s, ok := v.(string); ok && idFields[k] {
				raw, err := hex.DecodeString(s)
				if err != nil {
					raw = nil
				}
				n[k] = base64.StdEncoding.EncodeToString(raw)
				continue
			}
			hexToBase64(v)
		}
	case []any:
		for _, v := range n {
			hexToBase64(v)
		}
	}
}
