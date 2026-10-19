// Package redact cuts credentials from what Rotor keeps or says: events, run output, log lines, spans and audit
// records (#41). The patterns are api/secret-patterns.json, which the runner applies too.
package redact

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed secret-patterns.json
var patternsFile []byte

// Patterns is api/secret-patterns.json as read.
type Patterns struct {
	Redacted     string   `json:"redacted"`
	Patterns     []string `json:"patterns"`
	SecretKeys   []string `json:"secretKeys"`
	SecretStates []string `json:"secretStates"`
}

var (
	patterns = mustLoad()
	// Redacted stands in for every credential cut out.
	Redacted = patterns.Redacted
	inText   = mustCompile(patterns.Patterns)
	states   = toSet(patterns.SecretStates)
)

func mustLoad() Patterns {
	var p Patterns
	if err := json.Unmarshal(patternsFile, &p); err != nil || p.Redacted == "" || len(p.Patterns) == 0 {
		panic(fmt.Sprintf("redact: secret-patterns.json does not read: %v", err))
	}
	return p
}

func mustCompile(each []string) *regexp.Regexp {
	return regexp.MustCompile("(?:" + strings.Join(each, ")|(?:") + ")")
}

func toSet(words []string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}

// Text is s with every credential it holds replaced.
func Text(s string) string {
	if s == "" {
		return s
	}
	return inText.ReplaceAllLiteralString(s, Redacted)
}

// SecretKey says whether a field of this name holds a credential whatever its text looks like.
func SecretKey(key string) bool {
	k := strings.ToLower(key)
	for _, word := range patterns.SecretKeys {
		if strings.Contains(k, word) {
			return true
		}
	}
	return false
}

// Value is v with every credential replaced, in place: the text of a field a credential's name marks, and any
// credential in other text. Words that say a secret's state, such as "set", stay.
func Value(key string, v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			v[k] = Value(k, inner)
		}
		return v
	case []any:
		for i, inner := range v {
			v[i] = Value(key, inner)
		}
		return v
	case string:
		if v != "" && SecretKey(key) && !states[v] {
			return Redacted
		}
		return Text(v)
	}
	return v
}

// JSON is a JSON document with every credential replaced; what does not parse comes back as it is, for the
// caller to refuse.
func JSON(raw []byte) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(Value("", v))
	if err != nil {
		return raw
	}
	return out
}

// Data is a record's fields as JSON with every credential replaced, so an act never fails over its record.
func Data(data map[string]any) ([]byte, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return JSON(raw), nil
}
