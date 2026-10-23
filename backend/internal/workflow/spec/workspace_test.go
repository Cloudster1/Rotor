package spec

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAWorkspaceImageIsOneAnAdministratorAllows(t *testing.T) {
	source, _ := os.ReadFile(filepath.Join(fixtures, "valid", "workspace-steps.yaml"))
	s, problems := Parse(source)
	if s == nil || len(problems) > 0 {
		t.Fatalf("the fixture = %v", problems)
	}
	if images := s.WorkspaceImages(); !slices.Equal(images, []string{"golang-1.26"}) {
		t.Errorf("the workflow names the images %v", images)
	}
	if found := s.CheckImages(func(name string) bool { return name == "golang-1.26" }); len(found) != 0 {
		t.Errorf("an allowed image = %v", found)
	}
	found := s.CheckImages(func(string) bool { return false })
	if len(found) != 2 || found[0].Code != CodeImageNotAllowed || found[0].Path != "/nodes/1/workspace/image" || found[0].Line == 0 {
		t.Errorf("an image no administrator allows = %+v", found)
	}
}
