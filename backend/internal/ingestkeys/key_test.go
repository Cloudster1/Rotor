package ingestkeys

import (
	"bytes"
	"crypto/sha256"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/redact"
	"github.com/cloudster1/rotor/backend/migrations"
)

// keyLen is the whole key: the prefix and 32 bytes in unpadded base64url.
const keyLen = len(Prefix) + 43

func TestANewKeyHasThePrefixItsRandomnessAndIsKeptOnlyAsItsHash(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := New()
	if !strings.HasPrefix(a.Value, Prefix) || len(a.Value) != keyLen || a.Value == b.Value {
		t.Errorf("keys %q and %q, want %d characters each, different", a.Value, b.Value, keyLen)
	}
	sum := sha256.Sum256([]byte(a.Value))
	if !bytes.Equal(a.Hash, sum[:]) || bytes.Equal(a.Hash, b.Hash) {
		t.Errorf("the hash is not the key's SHA-256")
	}
	if a.Hint != a.Value[keyLen-config.IngestKeyHintChars:] || bytes.Contains(a.Hash, []byte(a.Value)) {
		t.Errorf("the hint %q is not the key's last characters", a.Hint)
	}
	if !WellFormed(a.Value) || !IsKey(a.Value) {
		t.Errorf("a new key %q is not well formed", a.Value)
	}
}

func TestOnlyAKeyOfTheShapeNewMakesIsLookedUp(t *testing.T) {
	good, _ := New()
	for name, credential := range map[string]string{
		"empty":                 "",
		"the prefix alone":      Prefix,
		"one character short":   good.Value[:keyLen-1],
		"one character long":    good.Value + "A",
		"padded":                good.Value[:keyLen-1] + "=",
		"not base64url":         good.Value[:keyLen-1] + "+",
		"an assistant's token":  "rotor_oat_" + good.Value[len(Prefix):],
		"another case's prefix": "ROTOR_IK_" + good.Value[len(Prefix):],
	} {
		if WellFormed(credential) {
			t.Errorf("%s (%q) is taken as well formed", name, credential)
		}
	}
	if IsKey("rotor_oat_x") || !IsKey(Prefix+"x") {
		t.Error("IsKey reads another prefix as a key's, or a key's as another")
	}
}

// Redaction cuts a key from every log line, event and audit record it might reach.
func TestAKeyIsRedacted(t *testing.T) {
	key, _ := New()
	if got := redact.Text("sent with " + key.Value); strings.Contains(got, key.Value[len(Prefix):]) {
		t.Errorf("redaction left the key in %q", got)
	}
}

func TestAKeyExpiresAtItsTime(t *testing.T) {
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	k := Key{ExpiresAt: &at}
	if k.Expired(at.Add(-time.Second)) || !k.Expired(at) || (Key{}).Expired(at.AddDate(100, 0, 0)) {
		t.Error("a key expires before its time, not at it, or one without expiry does")
	}
}

func TestANameAndAnExpiryAreChecked(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	_, err := check(Input{Name: "  ", ExpiresAt: &past}, now)
	invalid, ok := err.(*Invalid)
	if !ok || invalid.Fields["name"] == "" || invalid.Fields["expiresAt"] == "" {
		t.Errorf("a blank name and a past expiry = %v", err)
	}
	if _, err := check(Input{Name: strings.Repeat("k", config.MaxIngestKeyNameChars+1)}, now); err == nil {
		t.Error("a name past the limit is taken")
	}
	in, err := check(Input{Name: " Billing service ", AccountEmail: " svc@example.test ", ExpiresAt: &future}, now)
	if err != nil || in.Name != "Billing service" || in.AccountEmail != "svc@example.test" {
		t.Errorf("a good input = %+v, %v", in, err)
	}
}

// The database refuses what Go refuses only while both say the same numbers.
func TestTheMigrationsNumbersAreTheConstants(t *testing.T) {
	raw, err := migrations.FS.ReadFile("01560_ingest_keys.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- +goose Down")
	cases := map[string]struct {
		pattern string
		want    int
	}{
		"MaxIngestKeys":         {`FROM ingest_key\) >= (\d+)`, config.MaxIngestKeys},
		"MaxIngestKeyNameChars": {`char_length\(name\) <= (\d+)`, config.MaxIngestKeyNameChars},
		"IngestKeyHintChars":    {`char_length\(hint\) = (\d+)`, config.IngestKeyHintChars},
		"the hash's bytes":      {`octet_length\(secret_hash\) = (\d+)`, sha256.Size},
	}
	for name, c := range cases {
		match := regexp.MustCompile(c.pattern).FindStringSubmatch(up)
		if match == nil {
			t.Errorf("%s: the migration no longer says %q", name, c.pattern)
			continue
		}
		if got, _ := strconv.Atoi(match[1]); got != c.want {
			t.Errorf("%s: the migration says %d, config says %d", name, got, c.want)
		}
	}
}
