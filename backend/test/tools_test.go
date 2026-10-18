//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// toolOf reads the tool out of an api answer.
func toolOf(t *testing.T, answer map[string]any) map[string]any {
	t.Helper()
	tool, ok := answer["tool"].(map[string]any)
	if !ok {
		t.Fatalf("no tool in %v", answer)
	}
	return tool
}

func TestAnAdministratorAddsAToolAndRotorRegistersThere(t *testing.T) {
	h := newHarness(t)
	stub := newToolStub(t, nil)
	alice := newBrowser(t)
	alice.signIn("alice")

	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL + "/"})
	if status != http.StatusCreated {
		t.Fatalf("adding the tool = %d %v", status, answer)
	}
	tool := toolOf(t, answer)
	id, _ := tool["id"].(string)
	t.Cleanup(func() { alice.api(http.MethodDelete, "/tools/"+id, nil) })
	if tool["name"] != "exciter" || tool["baseURL"] != stub.BaseURL || tool["resource"] != stub.BaseURL+"/api/v1/mcp" ||
		tool["issuer"] != stub.BaseURL || tool["status"] != "ready" || tool["registration"] != "dynamic" {
		t.Errorf("the tool = %v", tool)
	}
	if got := stub.Stats()["registrations"]; got != float64(1) {
		t.Errorf("the tool saw %v registrations, want 1", got)
	}

	// The client secret is in OpenBao, and nowhere in the database.
	secret, err := rootBao(t).Logical().Read("rotor/data/tools/" + id + "/client")
	if err != nil || secret == nil {
		t.Fatalf("OpenBao holds no client for the tool: %v", err)
	}
	data, _ := secret.Data["data"].(map[string]any)
	clientSecret, _ := data["client_secret"].(string)
	if clientSecret == "" || data["auth_method"] != "client_secret_basic" {
		t.Fatalf("the client in OpenBao = %v", data)
	}
	if where := h.find(t, clientSecret); where != "" {
		t.Errorf("the client secret is in the database, in %s", where)
	}

	bob := newBrowser(t)
	bob.signIn("bob")
	if status, answer := bob.api(http.MethodGet, "/tools", nil); status != http.StatusOK || !strings.Contains(stringOf(answer), stub.BaseURL) {
		t.Errorf("a member listing the tools = %d %v", status, answer)
	}
	if status, _ := bob.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL}); status != http.StatusForbidden {
		t.Errorf("a member adding a tool = %d, want 403", status)
	}
	if status, _ := bob.api(http.MethodDelete, "/tools/"+id, nil); status != http.StatusForbidden {
		t.Errorf("a member removing a tool = %d, want 403", status)
	}
}

func TestAToolWithoutOAuthIsRefusedInASentence(t *testing.T) {
	stub := newToolStub(t, map[string]any{"oauth": false})
	alice := newBrowser(t)
	alice.signIn("alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	e, _ := answer["error"].(map[string]any)
	message, _ := e["message"].(string)
	if status != http.StatusUnprocessableEntity || e["code"] != "tool_refused" || !strings.Contains(message, "OAuth") {
		t.Fatalf("adding a tool without OAuth = %d %v", status, answer)
	}

	without := newToolStub(t, map[string]any{"capabilities": []string{}})
	if status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": without.BaseURL}); status != http.StatusUnprocessableEntity ||
		!strings.Contains(stringOf(answer), "mcp@1") {
		t.Errorf("adding a tool without MCP = %d %v", status, answer)
	}
}

func TestAToolThatForgotRotorIsRegisteredAgainAndRemovedForGood(t *testing.T) {
	stub := newToolStub(t, nil)
	alice := newBrowser(t)
	alice.signIn("alice")
	status, answer := alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	if status != http.StatusCreated {
		t.Fatalf("adding the tool = %d %v", status, answer)
	}
	id, _ := toolOf(t, answer)["id"].(string)

	stub.ForgetClients()
	status, answer = alice.api(http.MethodPost, "/tools", map[string]any{"baseURL": stub.BaseURL})
	if status != http.StatusOK || toolOf(t, answer)["id"] != id || stub.Stats()["registrations"] != float64(2) {
		t.Fatalf("registering again = %d %v, %v registrations", status, answer, stub.Stats()["registrations"])
	}

	if status, _ := alice.api(http.MethodDelete, "/tools/"+id, nil); status != http.StatusNoContent {
		t.Fatalf("removing the tool = %d", status)
	}
	if status, _ := alice.api(http.MethodGet, "/tools/"+id, nil); status != http.StatusNotFound {
		t.Errorf("a removed tool answers %d, want 404", status)
	}
	if secret, err := rootBao(t).Logical().Read("rotor/data/tools/" + id + "/client"); err != nil || secret != nil {
		t.Errorf("the client secret outlived the tool: %v %v", secret, err)
	}
}

// find names the table and column holding text anywhere in Rotor's schema, as a dump would reveal it.
func (h *harness) find(t *testing.T, text string) string {
	t.Helper()
	ctx := context.Background()
	rows, err := h.super.Query(ctx, `SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND data_type IN ('text', 'jsonb', 'json', 'ARRAY', 'character varying', 'citext', 'bytea')`)
	if err != nil {
		t.Fatal(err)
	}
	var columns [][2]string
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, [2]string{table, column})
	}
	rows.Close()
	for _, c := range columns {
		var found bool
		query := `SELECT EXISTS (SELECT 1 FROM "` + c[0] + `" WHERE position($1 in "` + c[1] + `"::text) > 0)`
		if err := h.super.QueryRow(ctx, query, text).Scan(&found); err != nil {
			t.Fatalf("search %s.%s: %v", c[0], c[1], err)
		}
		if found {
			return c[0] + "." + c[1]
		}
	}
	return ""
}

func stringOf(v any) string {
	var b strings.Builder
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			b.WriteString(k + "=" + stringOf(inner) + " ")
		}
	case []any:
		for _, inner := range v {
			b.WriteString(stringOf(inner) + " ")
		}
	case string:
		b.WriteString(v)
	}
	return b.String()
}
