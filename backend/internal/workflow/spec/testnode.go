package spec

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// CodeTestNode is a test node that cannot work as written (#138): one that writes its workspace, names no report or
// one outside the work tree, or asks for longer than its run.
const CodeTestNode = "test_node"

// testSense refuses what a test step could never do: push, since it only reads the branch, read a report the work
// tree does not hold, or run past the run's own timeout.
func testSense(s *Spec) []Problem {
	var out []Problem
	add := func(path, format string, args ...any) {
		out = append(out, Problem{Path: path, Code: CodeTestNode, Message: fmt.Sprintf(format, args...)})
	}
	for i, n := range s.Nodes {
		if n.Type != TypeTest {
			continue
		}
		base := fmt.Sprintf("/nodes/%d", i)
		if n.Workspace != nil && n.Workspace.Writes() {
			add(base+"/workspace/access", "%s only runs the tests, so its workspace reads the branch; give it access: read.", n.ID)
		}
		if len(n.Reports) == 0 {
			add(base+"/reports", "%s needs the JUnit files its command writes in reports, such as reports/junit.xml.", n.ID)
		}
		for j, report := range n.Reports {
			if why := reportPathProblem(report); why != "" {
				add(fmt.Sprintf("%s/reports/%d", base, j), "The report %s %s; give a path inside the work tree, such as reports/junit.xml.", report, why)
			}
		}
		if d, err := time.ParseDuration(n.Timeout); err == nil && d > s.Limits.TimeoutDuration() {
			add(base+"/timeout", "%s may run for the run's %s at most, not %s.", n.ID, s.Limits.Timeout, n.Timeout)
		}
	}
	return out
}

// reportPathProblem says why a report's path leaves the work tree or names no file, or is empty when it does neither.
func reportPathProblem(path string) string {
	parts := strings.Split(path, "/")
	switch {
	case strings.HasPrefix(path, "/"):
		return "is absolute"
	case slices.Contains(parts, ".."):
		return "climbs out with .."
	case strings.ContainsRune(path, 0):
		return "holds a NUL byte"
	case !slices.ContainsFunc(parts, func(p string) bool { return p != "" && p != "." }):
		return "names no file"
	}
	return ""
}
