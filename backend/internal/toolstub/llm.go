package toolstub

import (
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// A language model that answers from a script, behind OpenAI's chat API: what a workflow's agent nodes and the model
// providers' connection test call in tests, so no test reaches a real provider or depends on a model's whims.

const (
	llmPath = "/llm/v1"
	// StubModel is the one model the stub's provider lists.
	StubModel = "stub-model"
)

// LLMTurn is one scripted answer: text, or calls of the tools the request offered, after DelayMS when given.
type LLMTurn struct {
	Content   string        `json:"content,omitempty"`
	ToolCalls []LLMToolCall `json:"toolCalls,omitempty"`
	// DelayMS holds the answer back, as a slow model would, so a test can act while the run thinks.
	DelayMS int `json:"delayMs,omitempty"`
	// PromptTokens and CompletionTokens are the usage the answer reports (#38); zero reports the stub's default.
	PromptTokens     int `json:"promptTokens,omitempty"`
	CompletionTokens int `json:"completionTokens,omitempty"`
}

// LLMToolCall is a tool the scripted model calls, with its arguments.
type LLMToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// nextTurn is the script's next answer; past its end the last answer repeats, and with no script it says hello.
func (t *tenant) nextTurn() LLMTurn {
	if len(t.opts.LLMScript) == 0 {
		return LLMTurn{Content: "Hello from the stub model."}
	}
	turn := t.opts.LLMScript[min(t.llmNext, len(t.opts.LLMScript)-1)]
	t.llmNext++
	return turn
}

// thinking holds a chat answer back by its turn's delay, without the stub's lock, so other tenants go on.
func (s *Stub) thinking(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		delay := 0
		s.mu.Lock()
		if t, ok := s.tenants[chi.URLParam(r, "tenant")]; ok && len(t.opts.LLMScript) > 0 {
			delay = t.opts.LLMScript[min(t.llmNext, len(t.opts.LLMScript)-1)].DelayMS
		}
		s.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(min(time.Duration(delay)*time.Millisecond, config.ToolStubMaxLLMDelay)):
			case <-r.Context().Done():
				return
			}
		}
		next(w, r)
	}
}

// llmAuthorized checks the provider key: the tenant's own when it has one, any key otherwise.
func (t *tenant) llmAuthorized(r *http.Request) bool {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || key == "" {
		key = r.Header.Get("Api-Key")
	}
	return key != "" && (t.opts.LLMKey == "" || key == t.opts.LLMKey)
}

func (s *Stub) handleLLMModels(c *call) {
	if !c.t.llmAuthorized(c.r) {
		writeError(c.w, http.StatusUnauthorized, "invalid_api_key", "Send the provider key as a bearer.")
		return
	}
	writeJSON(c.w, http.StatusOK, map[string]any{"object": "list", "data": []map[string]any{{"id": StubModel, "object": "model", "owned_by": "rotor"}}})
}

type chatRequest struct {
	Model         string `json:"model"`
	Stream        bool   `json:"stream"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

func (s *Stub) handleLLMChat(c *call) {
	if !c.t.llmAuthorized(c.r) {
		writeError(c.w, http.StatusUnauthorized, "invalid_api_key", "Send the provider key as a bearer.")
		return
	}
	var req chatRequest
	body, _ := io.ReadAll(http.MaxBytesReader(c.w, c.r.Body, MaxMCPBytes))
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(c.w, http.StatusBadRequest, "bad_request", "Send a chat completion request as JSON.")
		return
	}
	c.t.stats.LLMCalls++
	turn := c.t.nextTurn()
	message, finish := map[string]any{"role": "assistant", "content": nil}, "stop"
	if turn.Content != "" {
		message["content"] = turn.Content
	}
	if len(turn.ToolCalls) > 0 {
		var calls []map[string]any
		for i, call := range turn.ToolCalls {
			arguments, _ := json.Marshal(call.Arguments)
			if call.Arguments == nil {
				arguments = []byte("{}")
			}
			calls = append(calls, map[string]any{"index": i, "id": fmt.Sprintf("call_%d_%d", c.t.stats.LLMCalls, i), "type": "function",
				"function": map[string]any{"name": call.Name, "arguments": string(arguments)}})
		}
		message["tool_calls"], finish = calls, "tool_calls"
	}
	prompt, completion := config.ToolStubPromptTokens, config.ToolStubCompletionTokens
	if turn.PromptTokens > 0 || turn.CompletionTokens > 0 {
		prompt, completion = turn.PromptTokens, turn.CompletionTokens
	}
	usage := map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}
	id, created := fmt.Sprintf("chatcmpl-%d", c.t.stats.LLMCalls), s.now().Unix()
	if !req.Stream {
		writeJSON(c.w, http.StatusOK, map[string]any{"id": id, "object": "chat.completion", "created": created, "model": req.Model,
			"choices": []map[string]any{{"index": 0, "message": message, "finish_reason": finish}}, "usage": usage})
		return
	}
	c.w.Header().Set("Content-Type", "text/event-stream")
	c.w.Header().Set("Cache-Control", "no-store")
	chunk := func(choices []map[string]any, extra map[string]any) {
		out := map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model, "choices": choices}
		for k, v := range extra {
			out[k] = v
		}
		encoded, _ := json.Marshal(out)
		_, _ = fmt.Fprintf(c.w, "data: %s\n\n", encoded)
	}
	chunk([]map[string]any{{"index": 0, "delta": message, "finish_reason": nil}}, nil)
	chunk([]map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}, nil)
	if req.StreamOptions.IncludeUsage {
		chunk([]map[string]any{}, map[string]any{"usage": usage})
	}
	_, _ = io.WriteString(c.w, "data: [DONE]\n\n")
}
