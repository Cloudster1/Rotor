package tools

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The contract from Rotor's side: each tool's vendored document still offers what discovery, connecting and acting
// read there. Nightly, suite.yml vendors the siblings' main first, so a change that would break Rotor shows before it
// is tagged (#43).

const toolsDir = "../../../api/tools/"

// reads are the operations Rotor relies on per tool, as "METHOD /path" under the document's /api/v1: Armature's are
// its OAuth (#45) and the ones its tools the story workflow calls stand for (#129); Stator's OAuth joins with #105.
var reads = map[string][]string{
	"exciter": {"GET /meta", "POST /mcp", "POST /oauth/register", "POST /oauth/token", "POST /oauth/revoke",
		"GET /.well-known/oauth-authorization-server", "GET /.well-known/oauth-protected-resource/api/v1/mcp"},
	"armature": {"GET /meta", "POST /mcp", "POST /oauth/register", "POST /oauth/token", "POST /oauth/revoke",
		"GET /.well-known/oauth-authorization-server", "GET /.well-known/oauth-protected-resource",
		"GET /auth/me", "GET /issues/{issueKey}", "GET /issues/{issueKey}/comments", "POST /issues/{issueKey}/comments",
		"GET /issues/{issueKey}/remote-links", "POST /issues/{issueKey}/remote-links"},
	"stator": {"GET /meta", "POST /mcp"},
}

type document struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]schema `json:"schemas"`
	} `json:"components"`
}

type schema struct {
	Ref        string                     `json:"$ref"`
	Properties map[string]json.RawMessage `json:"properties"`
}

func vendored(t *testing.T, tool string) document {
	t.Helper()
	raw, err := os.ReadFile(toolsDir + tool + "/openapi.json")
	if err != nil {
		t.Fatalf("read %s's document; vendor it with make tool-contracts TOOL=%s REF=<sha>: %v", tool, tool, err)
	}
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s's document does not read as OpenAPI: %v", tool, err)
	}
	return doc
}

// metaFields are the properties of the tool's /meta answer, through one $ref.
func (d document) metaFields() map[string]json.RawMessage {
	var op struct {
		Responses map[string]struct {
			Content map[string]struct {
				Schema schema `json:"schema"`
			} `json:"content"`
		} `json:"responses"`
	}
	_ = json.Unmarshal(d.Paths["/meta"]["get"], &op)
	s := op.Responses["200"].Content["application/json"].Schema
	if name, ok := strings.CutPrefix(s.Ref, "#/components/schemas/"); ok {
		s = d.Components.Schemas[name]
	}
	return s.Properties
}

func TestVendoredToolsOfferWhatRotorReads(t *testing.T) {
	for tool, ops := range reads {
		doc := vendored(t, tool)
		for _, op := range ops {
			method, path, _ := strings.Cut(op, " ")
			if _, ok := doc.Paths[path][strings.ToLower(method)]; !ok {
				t.Errorf("%s's document has no %s, which Rotor relies on", tool, op)
			}
		}
		fields := doc.metaFields()
		for _, field := range []string{"product", "capabilities"} {
			if _, ok := fields[field]; !ok {
				t.Errorf("%s's /meta answer has no %s, which discovery reads", tool, field)
			}
		}
	}
}
