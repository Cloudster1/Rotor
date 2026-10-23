// Package workspaces holds what an administrator lets workspaces use (#136): the toolchain images by the names
// workflows give, the package mirror they reach, and how long a step and how large a work tree may be.
package workspaces

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Image is a toolchain image: the name a workflow gives, and the reference, pinned by digest, it stands for.
type Image struct {
	Name      string `json:"name"`
	Reference string `json:"reference"`
}

// Setting is what workspaces may use.
type Setting struct {
	Images        []Image
	PackageMirror string
	MaxStepTime   string
	MaxVolume     string
	UpdatedAt     time.Time
}

// Invalid is a setting refused, with a sentence for each field that is wrong.
type Invalid struct{ Fields map[string]string }

func (e *Invalid) Error() string { return "the workspace setting is not valid" }

var (
	imageName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)
	// A reference with a registry, a repository and a digest; the digest keeps a name meaning one image.
	imageReference = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:-]*@sha256:[0-9a-f]{64}$`)
	stepTime       = regexp.MustCompile(`^([1-9][0-9]{0,5})([smh])$`)
	volumeSize     = regexp.MustCompile(`^([1-9][0-9]{0,5})(Mi|Gi)$`)
	durationUnits  = map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour}
	sizeUnits      = map[string]int64{"Mi": 1 << 20, "Gi": 1 << 30}
)

// StepTime is how long a workspace step may take.
func (s Setting) StepTime() time.Duration {
	m := stepTime.FindStringSubmatch(s.MaxStepTime)
	if m == nil {
		return config.DefaultWorkspaceStepTime
	}
	n, _ := strconv.Atoi(m[1])
	return time.Duration(n) * durationUnits[m[2]]
}

// VolumeBytes is how large a workspace's work tree may grow.
func (s Setting) VolumeBytes() int64 {
	m := volumeSize.FindStringSubmatch(s.MaxVolume)
	if m == nil {
		return config.DefaultWorkspaceVolume
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return n * sizeUnits[m[2]]
}

// Reference is the image a workflow's name stands for, and whether an administrator allows it.
func (s Setting) Reference(name string) (string, bool) {
	for _, i := range s.Images {
		if i.Name == name {
			return i.Reference, true
		}
	}
	return "", false
}

// Names are the images a workflow may name.
func (s Setting) Names() []string {
	out := make([]string, 0, len(s.Images))
	for _, i := range s.Images {
		out = append(out, i.Name)
	}
	return out
}

// MirrorEnv points a toolchain's package managers at the mirror alone; without one they fetch nothing, which the
// sandbox's egress would refuse anyway, and say so at once.
func (s Setting) MirrorEnv() map[string]string {
	if s.PackageMirror == "" {
		return map[string]string{"GOPROXY": "off"}
	}
	mirror := strings.TrimSuffix(s.PackageMirror, "/")
	return map[string]string{"GOPROXY": mirror, "GOSUMDB": "sum.golang.org " + mirror + "/sumdb/sum.golang.org",
		"npm_config_registry": mirror + "/"}
}

// Check refuses a setting that would let a workflow name an image ambiguously, reach more than one host, or make
// every step fail.
func (s Setting) Check() error {
	fields := map[string]string{}
	if len(s.Images) > config.MaxWorkspaceImages {
		fields["images"] = fmt.Sprintf("Allow at most %d toolchain images.", config.MaxWorkspaceImages)
	}
	seen := map[string]bool{}
	for i, image := range s.Images {
		at := fmt.Sprintf("images/%d", i)
		switch {
		case !imageName.MatchString(image.Name):
			fields[at+"/name"] = "Name the image with lowercase letters, digits, dots, dashes and underscores, as workflows will, such as golang-1.26."
		case seen[image.Name]:
			fields[at+"/name"] = fmt.Sprintf("%s is named twice; give each image a name of its own.", image.Name)
		}
		seen[image.Name] = true
		if len(image.Reference) > config.MaxImageReference || !imageReference.MatchString(image.Reference) {
			fields[at+"/reference"] = "Give the image's reference with its digest, such as golang:1.26-alpine@sha256:..., so the name always means the same image."
		}
	}
	if problem := checkMirror(s.PackageMirror); problem != "" {
		fields["packageMirror"] = problem
	}
	if m := stepTime.FindStringSubmatch(s.MaxStepTime); m == nil || s.StepTime() < config.MinWorkspaceStepTime || s.StepTime() > config.MaxWorkspaceStepTime {
		fields["maxStepTime"] = fmt.Sprintf("Give the longest a step may take as a duration from %s to %s, such as 1h or 45m.", config.MinWorkspaceStepTime, config.MaxWorkspaceStepTime)
	}
	if m := volumeSize.FindStringSubmatch(s.MaxVolume); m == nil || s.VolumeBytes() < config.MinWorkspaceVolume || s.VolumeBytes() > config.MaxWorkspaceVolume {
		fields["maxVolume"] = fmt.Sprintf("Give the work tree's size from %dMi to %dGi, such as 1Gi.", config.MinWorkspaceVolume>>20, config.MaxWorkspaceVolume>>30)
	}
	if len(fields) > 0 {
		return &Invalid{Fields: fields}
	}
	return nil
}

// checkMirror lets one plain address through: a host and maybe a path, no credentials, no query.
func checkMirror(mirror string) string {
	if mirror == "" {
		return ""
	}
	u, err := url.Parse(mirror)
	if err != nil || len(mirror) > config.MaxPackageMirror || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "Give the package mirror as one address, such as https://mirror.example.com/go, without credentials or a query; leave it empty for none."
	}
	return ""
}
