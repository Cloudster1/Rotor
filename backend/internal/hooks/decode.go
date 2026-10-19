package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Envelope versions as the tools' X-<P>-Webhook-Version header names them.
const (
	versionV1          = "1"
	versionCloudEvents = "2"
	cloudEventsSpec    = "1.0"
	dataVersion        = "1"
)

// Reading a delivery fails with one of these; both answer 422, so the tool keeps the delivery failing visibly.
var (
	ErrUnknownVersion = errors.New("the delivery is in an envelope version Rotor does not read")
	ErrNotEnvelope    = errors.New("the delivery is not a webhook envelope")
)

// Event is a delivery as an event trigger matches it and a run gets it, whichever envelope it came in.
type Event struct {
	// Tool is the product that sent it: armature, exciter or stator.
	Tool       string          `json:"tool"`
	Topic      string          `json:"topic"`
	ID         string          `json:"id"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

type envelopeV1 struct {
	Version    int             `json:"version"`
	ID         string          `json:"id"`
	Topic      string          `json:"topic"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

type cloudEvent struct {
	SpecVersion string          `json:"specversion"`
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Time        time.Time       `json:"time"`
	DataVersion json.RawMessage `json:"dataversion,omitempty"`
	Data        json.RawMessage `json:"data"`
}

// Decode reads a delivery its signature vouched for, as Exciter's receiver does (inbound.go at 6706bd8): version 1
// from any of the three, CloudEvents from a product that sends them; header is the X-<P>-Webhook-Version value.
func Decode(body []byte, header, product string) (Event, error) {
	var shape struct {
		SpecVersion *string `json:"specversion"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrNotEnvelope, err)
	}
	if shape.SpecVersion != nil {
		return decodeCloudEvent(body, header, product)
	}
	var v1 envelopeV1
	if err := json.Unmarshal(body, &v1); err != nil || v1.ID == "" || v1.Topic == "" {
		return Event{}, fmt.Errorf("%w: no id or topic", ErrNotEnvelope)
	}
	if v1.Version != 1 || (header != "" && header != versionV1) {
		return Event{}, fmt.Errorf("%w: %d in the body, %q in the header", ErrUnknownVersion, v1.Version, header)
	}
	return Event{Tool: product, Topic: v1.Topic, ID: v1.ID, OccurredAt: v1.OccurredAt, Payload: objectOr(v1.Payload)}, nil
}

func decodeCloudEvent(body []byte, header, product string) (Event, error) {
	prefix, sends := config.HookCloudEventTypes[product]
	var ce cloudEvent
	if err := json.Unmarshal(body, &ce); err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrNotEnvelope, err)
	}
	if !sends || ce.SpecVersion != cloudEventsSpec || (header != "" && header != versionCloudEvents) || !readsDataVersion(ce.DataVersion) {
		return Event{}, fmt.Errorf("%w: specversion %q from %s, %q in the header", ErrUnknownVersion, ce.SpecVersion, product, header)
	}
	topic, ok := strings.CutPrefix(ce.Type, prefix)
	if ce.ID == "" || !ok || topic == "" {
		return Event{}, fmt.Errorf("%w: id %q, type %q", ErrNotEnvelope, ce.ID, ce.Type)
	}
	return Event{Tool: product, Topic: topic, ID: ce.ID, OccurredAt: ce.Time, Payload: objectOr(ce.Data)}, nil
}

// readsDataVersion says whether the data is the version 1 payload: as a number, as a string, or absent.
func readsDataVersion(raw json.RawMessage) bool {
	switch string(bytes.TrimSpace(raw)) {
	case "", "null", dataVersion, `"` + dataVersion + `"`:
		return true
	}
	return false
}

func objectOr(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}
