package httpapi

// The rows of M13: what an administrator allows workspaces (#136); docs/api-contract-m13.md says what they mean.
func init() {
	operations = append(operations, m13Operations...)
}

var m13Operations = []operation{
	{method: "GET", path: "/settings/workspaces", handler: "handleGetWorkspaces", tag: "workflows", admin: true, pending: true,
		summary: "The toolchain images, package mirror and limits a workflow's workspaces may use.", responses: ok(workspaceSetting{})},
	{method: "PUT", path: "/settings/workspaces", handler: "handleSetWorkspaces", tag: "workflows", admin: true, pending: true,
		summary:     "Set the images workspaces may use, the package mirror they reach and their limits.",
		description: "A workflow naming an image no longer allowed is refused when it is saved or a run starts.",
		request:     workspaceSetting{}, responses: ok(workspaceSetting{})},
}

// workspaceSetting is what workspaces may use: images by the name workflows give, a package mirror, and limits.
type workspaceSetting struct {
	Images        []workspaceImage `json:"images"`
	PackageMirror string           `json:"packageMirror"`
	MaxStepTime   string           `json:"maxStepTime"`
	MaxVolume     string           `json:"maxVolume"`
}

// workspaceImage is a toolchain image: the name a workflow gives, and the pinned reference it stands for.
type workspaceImage struct {
	Name      string `json:"name"`
	Reference string `json:"reference"`
}
