package toolstub

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
)

// The MCP endpoint as Exciter's backend/internal/httpapi/mcp.go, one JSON-RPC request per POST: tools/list answers
// the tenant's snapshot, and the tools the suite's workflows call answer from its options and what was written there.

// MCPProtocolVersions are the revisions spoken, newest first, as Exciter speaks them.
var MCPProtocolVersions = []string{"2025-11-25", "2025-06-18"}

// WhoamiTool is the tool that names the person a token acts for, which is how a test proves a token is theirs.
const WhoamiTool = "whoami"

const (
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// caller is who an MCP request acts for.
type caller struct {
	username string
	readOnly bool
	scopes   []string
	// app and clientID name the connected app an Armature OAuth token was granted to; empty for a personal token.
	app, clientID string
}

// callerOf reads the bearer: an access token of the tenant when it is an authorization server, a personal token
// stub_pat_<username> otherwise.
func (s *Stub) callerOf(c *call) (*caller, bool) {
	auth := c.r.Header.Get("Authorization")
	bearer, isBearer := strings.CutPrefix(auth, "Bearer ")
	if !isBearer || bearer == "" {
		return nil, false
	}
	return s.callerByToken(c, bearer)
}

// callerByToken is who a token acts for; sent is false for no token, and the caller nil for one the tenant refuses.
func (s *Stub) callerByToken(c *call, bearer string) (*caller, bool) {
	if !c.t.oauth() {
		name, ok := strings.CutPrefix(bearer, PATPrefix)
		return &caller{username: name, scopes: []string{}}, ok && name != ""
	}
	tk, ok := c.t.tokens[string(digest(bearer))]
	if !ok || tk.refresh || !s.now().Before(tk.expires) || tk.grant.evidence {
		return nil, true
	}
	if c.t.armature() {
		app := "Rotor"
		if cl, ok := c.t.clients[tk.grant.clientID]; ok && cl.name != "" {
			app = cl.name
		}
		return &caller{username: tk.grant.username, readOnly: tk.readOnly, scopes: tk.scopes, app: app, clientID: tk.grant.clientID}, true
	}
	return &caller{username: tk.grant.username, readOnly: tk.readOnly, scopes: []string{scopeName(tk.readOnly, false)}}, true
}

func (s *Stub) handleMCP(c *call) {
	who, sent := s.callerOf(c)
	if who == nil {
		if c.t.oauth() {
			challenge := `Bearer resource_metadata="` + c.issuer + "/.well-known/oauth-protected-resource" + mcpPath + `", scope="mcp"`
			if c.t.armature() {
				challenge = `Bearer resource_metadata="` + c.issuer + `/.well-known/oauth-protected-resource"`
			}
			if sent {
				challenge += `, error="invalid_token"`
			}
			c.w.Header().Set("WWW-Authenticate", challenge)
		}
		writeError(c.w, http.StatusUnauthorized, "unauthorized", "Sign in, or send a token as a bearer.")
		return
	}
	if v := c.r.Header.Get("MCP-Protocol-Version"); v != "" && !slices.Contains(MCPProtocolVersions, v) && v < MCPProtocolVersions[0] {
		writeError(c.w, http.StatusBadRequest, "unsupported_protocol_version",
			"The stub speaks MCP "+strings.Join(MCPProtocolVersions, " and ")+". Update the client, or set it to one of them.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.w, c.r.Body, MaxMCPBytes))
	if err != nil {
		writeError(c.w, http.StatusBadRequest, "bad_request", "The request body is too large. Send less at a time.")
		return
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		writeRPC(c, nil, nil, &rpcError{Code: rpcInvalidRequest, Message: "Send one request at a time; batches are not accepted."})
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(c.w, http.StatusBadRequest, "bad_request", "The request body is not valid JSON. Send one JSON-RPC 2.0 request.")
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		writeRPC(c, req.ID, nil, &rpcError{Code: rpcInvalidRequest, Message: "A request names a method and says jsonrpc 2.0."})
		return
	}
	c.t.stats.MCPCalls++
	if strings.HasPrefix(req.Method, "notifications/") {
		c.w.WriteHeader(http.StatusAccepted)
		return
	}
	result, rpcErr := s.dispatch(c, who, req)
	writeRPC(c, req.ID, result, rpcErr)
}

func (s *Stub) dispatch(c *call, who *caller, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := MCPProtocolVersions[0]
		if slices.Contains(MCPProtocolVersions, params.ProtocolVersion) {
			version = params.ProtocolVersion
		}
		return map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo": map[string]any{"name": c.t.product(), "version": "stub"}}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.toolsFor(c.t, who)}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "A tool call names a tool and gives its arguments as an object."}
		}
		return s.callTool(c, who, params.Name, params.Arguments)
	}
	return nil, &rpcError{Code: rpcMethodNotFound, Message: "There is no method " + req.Method + "."}
}

// readOnlyTool reads a tool's readOnlyHint, which Exciter sets on every tool it lists.
func readOnlyTool(tool map[string]any) bool {
	hints, _ := tool["annotations"].(map[string]any)
	ro, _ := hints["readOnlyHint"].(bool)
	return ro
}

// toolsFor leaves the writing tools out for a token made to read, as Exciter does, with the tenant's writes marked;
// Armature lists every tool to every caller and refuses the call instead.
func (s *Stub) toolsFor(t *tenant, who *caller) []map[string]any {
	out := []map[string]any{}
	for _, tool := range s.toolsOf(t) {
		if name, _ := tool["name"].(string); slices.Contains(t.opts.Writes, name) {
			tool = asWrite(tool)
		}
		if !who.readOnly || readOnlyTool(tool) || t.snapshot() == SnapshotArmature {
			out = append(out, tool)
		}
	}
	return out
}

// asWrite is a copy of a listed tool whose annotations say it writes.
func asWrite(tool map[string]any) map[string]any {
	out := maps.Clone(tool)
	hints, _ := tool["annotations"].(map[string]any)
	hints = maps.Clone(hints)
	if hints == nil {
		hints = map[string]any{}
	}
	hints["readOnlyHint"] = false
	out["annotations"] = hints
	return out
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func refusal(message string) map[string]any {
	return map[string]any{"content": []toolContent{{Type: "text", Text: message}}, "isError": true}
}

func (s *Stub) callTool(c *call, who *caller, name string, arguments json.RawMessage) (any, *rpcError) {
	var tool map[string]any
	for _, t := range s.toolsOf(c.t) {
		if t["name"] == name {
			tool = t
		}
	}
	switch {
	case tool == nil:
		return nil, &rpcError{Code: rpcInvalidParams, Message: "There is no tool " + name + ". Ask tools/list for the names."}
	case c.t.snapshot() == SnapshotArmature:
		return s.callArmature(c, who, tool, arguments)
	case who.readOnly && !readOnlyTool(tool):
		return refusal("This connection may only read, so it cannot " + name + ". Connect again with writing allowed."), nil
	}
	if answer, ok := s.callExciter(c, who, name, arguments); ok {
		return answer, nil
	}
	return refusal("The test double does not answer " + name + "; it is listed so clients see Exciter's tools."), nil
}

// whoami answers in the shape of Exciter's whoami output schema, which clients check the answer against; the
// person's name is their username, and ids are derived from it so a test can expect them.
func whoami(t *tenant, who *caller) map[string]any {
	kind := "oauth"
	if !t.oauth() {
		kind = "personal"
	}
	scopes := who.scopes
	if scopes == nil {
		scopes = []string{}
	}
	return map[string]any{
		"user": map[string]any{"id": uuid.NewSHA1(uuid.NameSpaceOID, []byte("user:"+who.username)).String(),
			"email": who.username + "@suite.test", "name": who.username, "locale": ""},
		"installation": map[string]any{"name": t.name, "oauthConnections": t.oauth()},
		"role":         "member",
		"token": map[string]any{"id": uuid.NewSHA1(uuid.NameSpaceOID, []byte("token:"+t.name+":"+who.username)).String(),
			"kind": kind, "name": "Rotor", "scopes": scopes, "projects": []string{}, "allProjects": true, "expiresAt": nil},
		"signsWith": "token",
	}
}

func writeRPC(c *call, id json.RawMessage, result any, rpcErr *rpcError) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	if session := c.r.Header.Get("Mcp-Session-Id"); session != "" {
		c.w.Header().Set("Mcp-Session-Id", session)
	}
	writeJSON(c.w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr})
}
