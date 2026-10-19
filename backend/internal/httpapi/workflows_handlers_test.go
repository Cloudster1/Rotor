package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/catalog"
	"github.com/cloudster1/rotor/backend/internal/workflow/spec"
	"github.com/cloudster1/rotor/backend/internal/workflows"
)

type fakeWorkflows struct {
	wf       workflows.Workflow
	saveErr  error
	saved    int
	shared   []string
	problems []spec.Problem
	warnings []spec.Problem
}

func (f *fakeWorkflows) List(context.Context, bool) ([]workflows.Workflow, error) {
	return []workflows.Workflow{f.wf}, nil
}

func (f *fakeWorkflows) Get(_ context.Context, id uuid.UUID) (workflows.Workflow, workflows.Version, error) {
	if id != f.wf.ID {
		return workflows.Workflow{}, workflows.Version{}, workflows.ErrNotFound
	}
	return f.wf, workflows.Version{Number: f.wf.CurrentVersion, SpecYAML: "apiVersion: rotor.dev/v1\n", CreatedAt: time.Now()}, nil
}

func (f *fakeWorkflows) Versions(context.Context, uuid.UUID) ([]workflows.Version, error) {
	return []workflows.Version{{Number: 2}, {Number: 1}}, nil
}

func (f *fakeWorkflows) Version(_ context.Context, _ uuid.UUID, n int) (workflows.Version, error) {
	if n > f.wf.CurrentVersion {
		return workflows.Version{}, workflows.ErrNotFound
	}
	return workflows.Version{Number: n, SpecYAML: "x"}, nil
}

func (f *fakeWorkflows) Diff(_ context.Context, _ uuid.UUID, from, to int) (workflows.Diff, error) {
	return workflows.Diff{From: from, To: to, Hunks: []workflows.Hunk{{FromLine: 1, ToLine: 1,
		Lines: []workflows.Line{{Op: workflows.OpDelete, Text: "a"}, {Op: workflows.OpInsert, Text: "b"}}}}}, nil
}

func (f *fakeWorkflows) Check(context.Context, string) ([]spec.Problem, []spec.Problem) {
	return f.problems, f.warnings
}

func (f *fakeWorkflows) Create(context.Context, string, string) (uuid.UUID, error) {
	if len(f.problems) > 0 {
		return uuid.Nil, &workflows.Invalid{Problems: f.problems}
	}
	return f.wf.ID, nil
}

func (f *fakeWorkflows) Save(_ context.Context, _ uuid.UUID, _, _ string, base int) error {
	f.saved = base
	return f.saveErr
}

func (f *fakeWorkflows) Share(_ context.Context, _ uuid.UUID, sharing string, groups []string) error {
	f.shared = append([]string{sharing}, groups...)
	return f.saveErr
}

func (f *fakeWorkflows) Archive(context.Context, uuid.UUID) error { return f.saveErr }

type fakeCatalogs struct{ err error }

func (f fakeCatalogs) Get(_ context.Context, id uuid.UUID) (catalog.Catalog, error) {
	return catalog.Catalog{ToolID: id, ToolName: "exciter", Build: "1+abc",
		Tools: []catalog.Tool{{Name: "get_project", ReadOnly: true}, {Name: "update_requirement", Destructive: true}}}, f.err
}

func workflowServer(t *testing.T) (*Server, *fakeWorkflows) {
	t.Helper()
	s, sessions, _ := signedInServer(t)
	alice := sessions.people["alice"]
	store := &fakeWorkflows{wf: workflows.Workflow{ID: uuid.New(), Name: "triage", OwnerID: alice.UserID, OwnerName: alice.Name,
		Sharing: workflows.SharingPrivate, CurrentVersion: 2}}
	s.Workflows = store
	return s, store
}

func TestAWorkflowIsListedReadAndSaved(t *testing.T) {
	s, store := workflowServer(t)
	resp, body := serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, APIPrefix+"/workflows", nil), "alice"))
	if list, _ := body["workflows"].([]any); resp.StatusCode != http.StatusOK || len(list) != 1 || list[0].(map[string]any)["canEdit"] != true {
		t.Fatalf("GET /workflows = %d %v", resp.StatusCode, body)
	}
	path := APIPrefix + "/workflows/" + store.wf.ID.String()
	resp, body = serve(t, s.Routes(nil), withSession(httptestRequest(http.MethodPut, path, `{"specYAML":"x","baseVersion":2}`), "alice"))
	if resp.StatusCode != http.StatusOK || store.saved != 2 || body["version"] == nil {
		t.Fatalf("PUT = %d %v, saved on %d", resp.StatusCode, body, store.saved)
	}
	resp, _ = serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, path+"/versions/2", nil), "alice"))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("a version = %d", resp.StatusCode)
	}
	resp, body = serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, path+"/diff?from=1&to=2", nil), "alice"))
	if hunks, _ := body["hunks"].([]any); resp.StatusCode != http.StatusOK || len(hunks) != 1 {
		t.Errorf("a diff = %d %v", resp.StatusCode, body)
	}
	resp, _ = serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, path+"/diff?from=one", nil), "alice"))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("a diff without numbers = %d, want 422", resp.StatusCode)
	}
}

func TestARefusedSaveSaysWhyInTheWordsItNeeds(t *testing.T) {
	s, store := workflowServer(t)
	path := APIPrefix + "/workflows/" + store.wf.ID.String()
	for err, want := range map[error]struct {
		status int
		code   string
	}{
		workflows.ErrStale:    {http.StatusConflict, "workflow_changed"},
		workflows.ErrArchived: {http.StatusConflict, "workflow_archived"},
		workflows.ErrNotOwner: {http.StatusForbidden, "forbidden"},
		workflows.ErrNotFound: {http.StatusNotFound, "not_found"},
		&workflows.Invalid{Problems: []spec.Problem{{Path: "/nodes/0/call", Code: spec.CodeCallNotAllow, Message: "x is not allowed."}}}: {
			http.StatusUnprocessableEntity, "workflow_invalid"},
	} {
		store.saveErr = err
		resp, body := serve(t, s.Routes(nil), withSession(httptestRequest(http.MethodPut, path, `{"specYAML":"x","baseVersion":2}`), "alice"))
		if e := errorOf(t, body); resp.StatusCode != want.status || e["code"] != want.code {
			t.Errorf("%v = %d %v, want %d %s", err, resp.StatusCode, body, want.status, want.code)
		}
	}
}

func TestValidatingAndCreatingNameEachProblem(t *testing.T) {
	s, store := workflowServer(t)
	store.problems = []spec.Problem{{Path: "/limits", Line: 3, Column: 1, Code: spec.CodeSchema, Message: "At /limits: missing."}}
	resp, body := serve(t, s.Routes(nil), withSession(httptestRequest(http.MethodPost, APIPrefix+"/workflows/validate", `{"specYAML":"x"}`), "alice"))
	problems, _ := body["problems"].([]any)
	if resp.StatusCode != http.StatusOK || body["valid"] != false || len(problems) != 1 || problems[0].(map[string]any)["line"] != float64(3) {
		t.Fatalf("validate = %d %v", resp.StatusCode, body)
	}
	resp, body = serve(t, s.Routes(nil), withSession(httptestRequest(http.MethodPost, APIPrefix+"/workflows", `{"specYAML":"x"}`), "alice"))
	if fields, _ := errorOf(t, body)["fields"].(map[string]any); resp.StatusCode != http.StatusUnprocessableEntity || fields["/limits"] == nil {
		t.Errorf("create with problems = %d %v", resp.StatusCode, body)
	}
	store.problems = nil
	if resp, _ := serve(t, s.Routes(nil), withSession(httptestRequest(http.MethodPost, APIPrefix+"/workflows", `{"specYAML":"x"}`), "alice")); resp.StatusCode != http.StatusCreated {
		t.Errorf("create = %d", resp.StatusCode)
	}
}

func TestAWarningLeavesAWorkflowValidAndSavable(t *testing.T) {
	s, store := workflowServer(t)
	store.warnings = []spec.Problem{{Path: "/nodes/1", Line: 9, Column: 3, Code: spec.CodeUntrustedWrite, Message: "Put an approval node between them."}}
	resp, body := serve(t, s.Routes(nil), withSession(httptestRequest(http.MethodPost, APIPrefix+"/workflows/validate", `{"specYAML":"x"}`), "alice"))
	warnings, _ := body["warnings"].([]any)
	problems, _ := body["problems"].([]any)
	if resp.StatusCode != http.StatusOK || body["valid"] != true || len(problems) != 0 || len(warnings) != 1 ||
		warnings[0].(map[string]any)["code"] != spec.CodeUntrustedWrite {
		t.Fatalf("validate with a warning = %d %v", resp.StatusCode, body)
	}
	if resp, _ := serve(t, s.Routes(nil), withSession(httptestRequest(http.MethodPost, APIPrefix+"/workflows", `{"specYAML":"x"}`), "alice")); resp.StatusCode != http.StatusCreated {
		t.Errorf("create with a warning = %d", resp.StatusCode)
	}
}

func TestSharingAndTheSchemaAndTheCatalog(t *testing.T) {
	s, store := workflowServer(t)
	path := APIPrefix + "/workflows/" + store.wf.ID.String() + "/sharing"
	if resp, _ := serve(t, s.Routes(nil), withSession(httptestRequest(http.MethodPut, path, `{"sharing":"groups","groups":["developers"]}`), "alice")); resp.StatusCode != http.StatusOK ||
		fmt.Sprint(store.shared) != "[groups developers]" {
		t.Errorf("sharing = %d, shared %v", resp.StatusCode, store.shared)
	}
	resp, body := serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, APIPrefix+"/schemas/workflow.v1.json", nil), "alice"))
	if resp.StatusCode != http.StatusOK || body["$id"] != spec.SchemaURL {
		t.Errorf("the schema = %d %v", resp.StatusCode, body["$id"])
	}
	catalogPath := APIPrefix + "/tools/" + uuid.NewString() + "/catalog"
	if resp, _ := serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, catalogPath, nil), "alice")); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("the catalog without tools = %d", resp.StatusCode)
	}
	s.Catalogs = fakeCatalogs{}
	resp, body = serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, catalogPath, nil), "alice"))
	if listed, _ := body["tools"].([]any); resp.StatusCode != http.StatusOK || len(listed) != 2 || listed[1].(map[string]any)["destructive"] != true {
		t.Errorf("the catalog = %d %v", resp.StatusCode, body)
	}
	s.Catalogs = fakeCatalogs{err: fmt.Errorf("%w: no", catalog.ErrNotConnected)}
	resp, body = serve(t, s.Routes(nil), withSession(httptest.NewRequest(http.MethodGet, catalogPath, nil), "alice"))
	if resp.StatusCode != http.StatusConflict || errorOf(t, body)["code"] != "not_connected" {
		t.Errorf("the catalog unconnected = %d %v", resp.StatusCode, body)
	}
}
