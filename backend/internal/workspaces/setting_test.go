package workspaces

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const digest = "@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c"

func valid() Setting {
	return Setting{Images: []Image{{Name: "golang-1.26", Reference: "golang:1.26.8-alpine" + digest}},
		PackageMirror: "https://mirror.example.com/go", MaxStepTime: "45m", MaxVolume: "2Gi"}
}

func TestAWorkspaceSettingNamesPinnedImagesAndOneMirror(t *testing.T) {
	s := valid()
	if err := s.Check(); err != nil {
		t.Fatalf("a valid setting = %v", err)
	}
	if s.StepTime() != 45*time.Minute || s.VolumeBytes() != 2<<30 {
		t.Errorf("45m and 2Gi read as %s and %d", s.StepTime(), s.VolumeBytes())
	}
	if ref, ok := s.Reference("golang-1.26"); !ok || !strings.HasSuffix(ref, digest) {
		t.Errorf("golang-1.26 stands for %q, %v", ref, ok)
	}
	if _, ok := s.Reference("node-24"); ok {
		t.Error("an image no administrator allows has a reference")
	}
	env := s.MirrorEnv()
	if env["GOPROXY"] != "https://mirror.example.com/go" || env["GOSUMDB"] != "sum.golang.org https://mirror.example.com/go/sumdb/sum.golang.org" ||
		env["npm_config_registry"] != "https://mirror.example.com/go/" {
		t.Errorf("the mirror's environment = %v", env)
	}
	if none := (Setting{}).MirrorEnv(); none["GOPROXY"] != "off" || len(none) != 1 {
		t.Errorf("without a mirror = %v", none)
	}
}

func TestAWorkspaceSettingThatWouldMisleadOrFailEveryStepIsRefused(t *testing.T) {
	for field, change := range map[string]func(*Setting){
		"images/0/reference": func(s *Setting) { s.Images[0].Reference = "golang:1.26" },
		"images/1/name": func(s *Setting) {
			s.Images = append(s.Images, Image{Name: "golang-1.26", Reference: "golang:1.25" + digest})
		},
		"images/0/name": func(s *Setting) { s.Images[0].Name = "Go 1.26" },
		"packageMirror": func(s *Setting) { s.PackageMirror = "https://user:secret@mirror.example.com" },
		"maxStepTime":   func(s *Setting) { s.MaxStepTime = "30s" },
		"maxVolume":     func(s *Setting) { s.MaxVolume = "100Gi" },
	} {
		s := valid()
		change(&s)
		var invalid *Invalid
		if err := s.Check(); !errors.As(err, &invalid) || invalid.Fields[field] == "" {
			t.Errorf("changing %s = %v", field, err)
		}
	}
	for _, mirror := range []string{"ftp://mirror.example.com", "https://mirror.example.com/?token=x", "mirror.example.com"} {
		s := valid()
		s.PackageMirror = mirror
		if s.Check() == nil {
			t.Errorf("the mirror %s is allowed", mirror)
		}
	}
}
