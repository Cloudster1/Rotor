package redact

import (
	"fmt"
	"log/slog"
)

// Attr cuts credentials from a log line's message and attributes; it is a slog.HandlerOptions ReplaceAttr, so no
// line the control plane writes carries a token a caller or a tool handed it.
func Attr(_ []string, a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		s := a.Value.String()
		if s != "" && SecretKey(a.Key) && !states[s] {
			return slog.String(a.Key, Redacted)
		}
		if r := Text(s); r != s {
			return slog.String(a.Key, r)
		}
	case slog.KindAny:
		v := a.Value.Any()
		var s string
		switch v := v.(type) {
		case error:
			s = v.Error()
		case fmt.Stringer:
			s = v.String()
		default:
			return a
		}
		if r := Text(s); r != s {
			return slog.String(a.Key, r)
		}
	}
	return a
}
