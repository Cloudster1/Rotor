// Package hooks takes the events Armature, Exciter and Stator deliver (#35): it checks each tool's signature, reads
// both envelope versions into one event, keeps its id against repeats and queues the event triggers it matches.
package hooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// The signing below is Exciter's (backend/internal/webhook/webhook.go at 6706bd8), which all three tools share.

// Sign is the header value for a body: sha256= and the hex HMAC-SHA256 with the secret.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// SignTimestamped signs "{timestamp}.{body}", the time in Unix seconds as the timestamp header carries it.
func SignTimestamped(body []byte, secret string, at time.Time) string {
	return Sign(append([]byte(strconv.FormatInt(at.Unix(), 10)+"."), body...), secret)
}

// VerifyTimestamped says whether the timestamped signature matches, in constant time, and the time it names is within
// tolerance of now either way; a timestamp not written as the sender writes it fails too.
func VerifyTimestamped(body []byte, secret, timestamp, header string, tolerance time.Duration, now time.Time) bool {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || strconv.FormatInt(seconds, 10) != timestamp || secret == "" {
		return false
	}
	at := time.Unix(seconds, 0)
	if drift := now.Sub(at); drift > tolerance || drift < -tolerance {
		return false
	}
	return hmac.Equal([]byte(SignTimestamped(body, secret, at)), []byte(header))
}
