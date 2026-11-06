package traces

import (
	"slices"
	"testing"
	"time"
)

var epoch = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

// keptAt is a span of id under parent, starting at the given second.
func keptAt(id, parent string, second int) Span {
	start := epoch.Add(time.Duration(second) * time.Second)
	return Span{SpanID: id, ParentSpanID: parent, Name: "s" + id, StartedAt: start, EndedAt: start.Add(time.Second)}
}

// shape writes a tree as ids nested in brackets, so a test compares one string.
func shape(nodes []*Node) string {
	out := ""
	for i, n := range nodes {
		if i > 0 {
			out += " "
		}
		out += n.SpanID
		if len(n.Children) > 0 {
			out += "[" + shape(n.Children) + "]"
		}
	}
	return out
}

func TestATreeNestsSpansUnderTheirParentsInStartOrder(t *testing.T) {
	spans := []Span{keptAt("c", "a", 3), keptAt("a", "", 1), keptAt("b", "a", 2), keptAt("d", "b", 4), keptAt("e", "b", 4)}
	if got := shape(Tree(spans)); got != "a[b[d e] c]" {
		t.Errorf("the tree is %s", got)
	}
}

func TestASpanWhoseParentTheTraceDoesNotHoldIsARoot(t *testing.T) {
	// The runner's run span has the api's span for a parent, which Rotor does not keep; a sender's spans may join too.
	spans := []Span{keptAt("a", "f00000000000000f", 2), keptAt("b", "a", 3), keptAt("x", "e00000000000000e", 1)}
	if got := shape(Tree(spans)); got != "x a[b]" {
		t.Errorf("the tree is %s", got)
	}
	if got := Tree(nil); len(got) != 0 {
		t.Errorf("no spans make %d roots", len(got))
	}
}

func TestALoopOfParentsStillShowsEverySpanOnce(t *testing.T) {
	spans := []Span{keptAt("a", "c", 1), keptAt("b", "a", 2), keptAt("c", "b", 3), keptAt("s", "s", 4), keptAt("r", "", 0)}
	roots := Tree(spans)
	if got := shape(roots); got != "r a[b[c]] s" {
		t.Errorf("the tree is %s", got)
	}
	var seen []string
	var walk func([]*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			seen = append(seen, n.SpanID)
			walk(n.Children)
		}
	}
	walk(roots)
	slices.Sort(seen)
	if !slices.Equal(seen, []string{"a", "b", "c", "r", "s"}) {
		t.Errorf("the tree shows %v", seen)
	}
}

func TestAModelAndItsTokensReadOpenInferenceFirstThenGenAI(t *testing.T) {
	both := map[string]string{"llm.model_name": "claude", "gen_ai.request.model": "ChatAnthropic",
		"llm.token_count.prompt": "10", "llm.token_count.completion": "5", "gen_ai.usage.input_tokens": "99"}
	if m := Model(both); m != "claude" {
		t.Errorf("the model is %q", m)
	}
	if p, c, total, ok := Tokens(both); !ok || p != 10 || c != 5 || total != 15 {
		t.Errorf("tokens %d %d %d %v", p, c, total, ok)
	}
	genAI := map[string]string{"gen_ai.request.model": "FakeChatModel", "gen_ai.usage.input_tokens": "7",
		"gen_ai.usage.output_tokens": "3", "llm.token_count.total": "11"}
	if m := Model(genAI); m != "FakeChatModel" {
		t.Errorf("the model is %q", m)
	}
	if p, c, total, ok := Tokens(genAI); !ok || p != 7 || c != 3 || total != 11 {
		t.Errorf("tokens %d %d %d %v", p, c, total, ok)
	}
	if _, _, _, ok := Tokens(map[string]string{"tool.name": "whoami"}); ok {
		t.Error("a tool call counts tokens")
	}
}
