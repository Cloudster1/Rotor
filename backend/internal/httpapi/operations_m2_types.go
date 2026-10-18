package httpapi

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/cloudster1/rotor/backend/internal/openapi"
)

// workflowSharing says who besides the owner sees a workflow.
type workflowSharing string

// runStatus is where a run is in docs/api-contract-m2.md's state machine.
type runStatus string

// providerKind is how Rotor talks to a language model provider.
type providerKind string

// diffOp is what a diff line does.
type diffOp string

// m2Enums gives the builder the values each M2 string type takes.
var m2Enums = map[reflect.Type][]string{
	reflect.TypeOf(workflowSharing("")): {"private", "groups", "everyone"},
	reflect.TypeOf(runStatus("")):       {"queued", "starting", "running", "waiting", "succeeded", "failed", "cancelled", "expired"},
	reflect.TypeOf(providerKind("")):    {"anthropic", "openai", "azure_openai", "openai_compatible"},
	reflect.TypeOf(diffOp("")):          {"equal", "insert", "delete"},
}

// registerM2Schemas tells the builder about the M2 enums.
func registerM2Schemas(b *openapi.Builder) {
	for t, values := range m2Enums {
		b.Enums[t] = values
	}
}

// personRef names a person as others see them.
type personRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// workflowSummary is a workflow in a list.
type workflowSummary struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Owner          personRef       `json:"owner"`
	Sharing        workflowSharing `json:"sharing"`
	SharedGroups   []string        `json:"sharedGroups"`
	CurrentVersion int             `json:"currentVersion"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	Archived       bool            `json:"archived"`
	// CanEdit says whether the caller may save versions and share it: the owner alone.
	CanEdit bool `json:"canEdit"`
}

// workflowResponse is a workflow with its current version.
type workflowResponse struct {
	Workflow workflowSummary `json:"workflow"`
	Version  versionResponse `json:"version"`
}

// saveWorkflowRequest is a workflow's YAML; baseVersion is the version it was edited from, 0 for a new one.
type saveWorkflowRequest struct {
	SpecYAML    string `json:"specYAML"`
	Comment     string `json:"comment,omitempty"`
	BaseVersion int    `json:"baseVersion,omitempty"`
}

// versionSummary is a version in a list.
type versionSummary struct {
	Number    int       `json:"number"`
	Comment   string    `json:"comment"`
	Hash      string    `json:"hash"`
	CreatedBy personRef `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// versionResponse is a version with its YAML.
type versionResponse struct {
	versionSummary
	SpecYAML string `json:"specYAML"`
}

// diffResponse is what changed between two versions, as hunks of lines.
type diffResponse struct {
	From  int        `json:"from"`
	To    int        `json:"to"`
	Hunks []diffHunk `json:"hunks"`
}

// diffHunk is a run of changed lines with their context; lines count from 1.
type diffHunk struct {
	FromLine int        `json:"fromLine"`
	ToLine   int        `json:"toLine"`
	Lines    []diffLine `json:"lines"`
}

type diffLine struct {
	Op   diffOp `json:"op"`
	Text string `json:"text"`
}

// sharingRequest says who sees a workflow; groups are realm groups, used when sharing is groups.
type sharingRequest struct {
	Sharing workflowSharing `json:"sharing"`
	Groups  []string        `json:"groups,omitempty"`
}

// validateRequest is YAML to check without saving.
type validateRequest struct {
	SpecYAML string `json:"specYAML"`
}

// validateResponse lists what is wrong; valid is true when nothing is.
type validateResponse struct {
	Valid    bool      `json:"valid"`
	Problems []problem `json:"problems"`
}

// problem is one thing wrong with a workflow, where it is: a JSON pointer, and a line and column from 1.
type problem struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// catalogResponse is a tool's MCP tools as the caller's connection sees them.
type catalogResponse struct {
	Tool  connectionTool `json:"tool"`
	Build string         `json:"build"`
	Tools []catalogTool  `json:"tools"`
}

// catalogTool is one MCP tool, with the hints the tool gives.
type catalogTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	ReadOnly    bool            `json:"readOnly"`
	Destructive bool            `json:"destructive"`
}

// providerResponse is a provider as administrators see it; hasKey says whether OpenBao holds its key.
type providerResponse struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Kind         providerKind `json:"kind"`
	BaseURL      string       `json:"baseURL"`
	APIVersion   string       `json:"apiVersion"`
	Models       []string     `json:"models"`
	DefaultModel string       `json:"defaultModel"`
	// IsDefault says this provider holds the installation's default model.
	IsDefault bool `json:"isDefault"`
	Enabled   bool `json:"enabled"`
	HasKey    bool `json:"hasKey"`
}

// providerRequest adds or changes a provider; apiKey goes straight to OpenBao.
type providerRequest struct {
	Name         string       `json:"name"`
	Kind         providerKind `json:"kind"`
	BaseURL      string       `json:"baseURL,omitempty"`
	APIVersion   string       `json:"apiVersion,omitempty"`
	Models       []string     `json:"models"`
	DefaultModel string       `json:"defaultModel,omitempty"`
	IsDefault    bool         `json:"isDefault,omitempty"`
	Enabled      bool         `json:"enabled"`
	APIKey       string       `json:"apiKey,omitempty"`
}

// providerTestResponse says whether the provider answered with the key, and which models it offers.
type providerTestResponse struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Models  []string `json:"models"`
}

// modelResponse is a model a workflow may name.
type modelResponse struct {
	Provider personRef `json:"provider"`
	Name     string    `json:"name"`
	Default  bool      `json:"default"`
}

// startRunRequest starts a run; version pins one, the current version otherwise.
type startRunRequest struct {
	Inputs  json.RawMessage `json:"inputs"`
	Version int             `json:"version,omitempty"`
}

// runResponse is a run as its viewers see it.
type runResponse struct {
	ID         string     `json:"id"`
	Workflow   personRef  `json:"workflow"`
	Version    int        `json:"version"`
	Status     runStatus  `json:"status"`
	StartedBy  personRef  `json:"startedBy"`
	Reason     string     `json:"reason"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}
