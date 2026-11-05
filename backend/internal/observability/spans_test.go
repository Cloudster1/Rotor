package observability

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudster1/rotor/backend/internal/config"
)

func TestTheEmbeddedTraceContentIsTheOneInAPI(t *testing.T) {
	published, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "trace-content.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(published, traceContentFile) {
		t.Fatal("backend/internal/observability/trace-content.json differs from api/; run make trace-content")
	}
}

func TestKeysThatCarryTextAreToldFromThoseThatDoNot(t *testing.T) {
	for _, key := range []string{"input.value", "llm.input_messages.0.message.content", "tool.parameters", "llm.prompts", "custom"} {
		if !IsText(key) {
			t.Errorf("%s carries text", key)
		}
	}
	for _, key := range []string{"openinference.span.kind", "llm.model_name", "tool.name", "tool_call.id", "metadata", "exception.message"} {
		if IsText(key) {
			t.Errorf("%s carries no text", key)
		}
	}
	if !never("llm.input_messages.0.message.contents.0.message_content.image.image.url") || !never("embedding.embeddings.0.embedding.vector") {
		t.Error("an image or a vector would be kept")
	}
}

// The control plane holds every span to the contract's limits whatever the runner did, Rotor's keys first.
func TestASpanIsHeldToTheLimits(t *testing.T) {
	attrs := map[string]string{"rotor.node": "a", "input.value": "text"}
	for i := range config.MaxSpanAttributes {
		attrs[fmt.Sprintf("llm.extra.%03d", i)] = "v"
	}
	events := make([]SpanEvent, config.MaxSpanEvents+3)
	for i := range events {
		events[i] = SpanEvent{Name: fmt.Sprintf("retry %d", i)}
	}
	got := Hold(RunnerSpan{Name: strings.Repeat("n", 2*config.MaxSpanNameChars), Attributes: attrs, Events: events})
	if len(got.Attributes) != config.MaxSpanAttributes || got.Attributes["rotor.node"] != "a" || got.Attributes["input.value"] != "" {
		t.Errorf("kept %d attributes, rotor.node %q and input.value %q", len(got.Attributes), got.Attributes["rotor.node"], got.Attributes["input.value"])
	}
	if len(got.Name) != config.MaxSpanNameChars || len(got.Events) != config.MaxSpanEvents || got.Events[0].Name != "retry 3" {
		t.Errorf("the name is %d long and the events are %v; the latest events are kept", len(got.Name), got.Events)
	}
}

func TestASpanTooLargeDropsItsLongestTextFirstAndSaysSo(t *testing.T) {
	attrs := map[string]string{"tool.name": "t", "rotor.node": "a"}
	for i := range 6 {
		attrs[fmt.Sprintf("llm.output_messages.%d.message.content", i)] = strings.Repeat(fmt.Sprint(i), config.MaxSpanContentChars)
	}
	got := Hold(RunnerSpan{Name: "model", Attributes: attrs})
	if size := spanBytes(&got); size > config.MaxSpanBytes || got.Attributes[ContentCut] != "true" {
		t.Errorf("the span holds %d bytes, cut %q", size, got.Attributes[ContentCut])
	}
	if got.Attributes["tool.name"] != "t" || got.Attributes["rotor.node"] != "a" || len(got.Attributes) < 4 {
		t.Errorf("the span kept %d attributes; only text goes while that fits", len(got.Attributes))
	}

	without := WithoutText([]RunnerSpan{got, {Events: []SpanEvent{{Attributes: map[string]string{"exception.message": "m", "input.value": "v"}}}}})
	if len(without[0].Attributes) != 3 || len(without[1].Events[0].Attributes) != 1 {
		t.Errorf("without text the spans keep %v and %v", without[0].Attributes, without[1].Events[0].Attributes)
	}
}
