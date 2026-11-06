package httpapi

import "net/http"

// The rows of #267: renaming a tool and the workflows that bind it by name; docs/api-contract-m1.md says what they mean.
func init() {
	operations = append(operations, toolNameOperations...)
}

var toolNameOperations = []operation{
	{method: "GET", path: "/tools/{toolID}/workflows", handler: "handleToolWorkflows", tag: "tools", admin: true,
		summary:     "The workflows whose current version binds the tool by its name, which stop finding it once it is renamed.",
		description: "Every workflow not archived, whoever owns it, so an administrator renaming the tool knows whom to tell.",
		responses:   map[int]any{http.StatusOK: toolWorkflowsResponse{}, http.StatusNotFound: errorEnvelope{}}},
	{method: "PUT", path: "/tools/{toolID}/name", handler: "handleRenameTool", tag: "tools", admin: true,
		summary: "Rename a tool: the name workflows bind it by.",
		description: "Its product, client and connections stay. Workflows that bound the old name no longer find the tool, and " +
			"the answer lists them for their owners to change. A name another tool has, case aside, is refused with 409 " +
			"tool_name_taken. The audit log records tool.renamed.",
		request: renameToolRequest{},
		responses: map[int]any{http.StatusOK: renameToolResponse{}, http.StatusNotFound: errorEnvelope{},
			http.StatusConflict: errorEnvelope{}, http.StatusUnprocessableEntity: errorEnvelope{}}},
}

// renameToolRequest is the tool's new name.
type renameToolRequest struct {
	// Name is 1 to 63 lower case letters, digits, dots, dashes or underscores, starting with a letter or digit.
	Name string `json:"name"`
}

// toolBinder is a workflow whose current version binds a tool by name.
type toolBinder struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnerName string `json:"ownerName"`
}

// toolWorkflowsResponse are the workflows binding the tool's name now.
type toolWorkflowsResponse struct {
	Workflows []toolBinder `json:"workflows"`
}

// renameToolResponse is the renamed tool and the workflows that still bind its old name.
type renameToolResponse struct {
	Tool      toolResponse `json:"tool"`
	Workflows []toolBinder `json:"workflows"`
}
