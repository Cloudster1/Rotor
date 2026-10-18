package runnerctl

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/audit"
)

// tokenPrefixes start credentials an event must never carry, beyond those the audit log knows: the tool double's,
// OpenBao's own tokens and the model providers' keys.
var tokenPrefixes = []string{"stub_oat_", "stub_ort_", "stub_ocs_", "stub_pat_", "hvs.", "hvb.", "sk-"}

// secretInText finds a credential anywhere in a string: a bearer header's value, or a token by its prefix.
var secretInText = func() *regexp.Regexp {
	prefixes := []string{}
	for _, p := range append(append([]string{}, audit.SecretPrefixes...), tokenPrefixes...) {
		prefixes = append(prefixes, regexp.QuoteMeta(p))
	}
	return regexp.MustCompile(`(?i:bearer)\s+[A-Za-z0-9._~+/=-]+|(?:` + strings.Join(prefixes, "|") + `)[A-Za-z0-9._~+/=-]+`)
}()

// secretKeys mark a field whose text is a credential whatever it looks like.
var secretKeys = []string{"authorization", "token", "secret", "password", "api_key", "apikey", "credential"}

// redact is the runner's event payload with every credential replaced, the runner having cut those it knows of
// already; what does not parse is left for appendEvent to refuse.
func redact(payload []byte) []byte {
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return payload
	}
	out, err := json.Marshal(redactValue("", v))
	if err != nil {
		return payload
	}
	return out
}

func redactValue(key string, v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			v[k] = redactValue(k, inner)
		}
		return v
	case []any:
		for i, inner := range v {
			v[i] = redactValue(key, inner)
		}
		return v
	case string:
		if v != "" && secretKey(key) {
			return audit.Redacted
		}
		return secretInText.ReplaceAllString(v, audit.Redacted)
	}
	return v
}

func secretKey(key string) bool {
	k := strings.ToLower(key)
	for _, word := range secretKeys {
		if strings.Contains(k, word) {
			return true
		}
	}
	return false
}
