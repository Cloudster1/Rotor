package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// waitDelay is how long a killed command may hold its output pipes open through a child before the agent stops
// reading them.
const waitDelay = 5 * time.Second

// request is one command: argv as given, never through a shell, in dir, with env added to the container's own.
type request struct {
	Argv           []string `json:"argv"`
	Dir            string   `json:"dir"`
	Env            []string `json:"env"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	MaxOutputBytes int      `json:"maxOutputBytes"`
}

// answer is how a command ended: its exit code, whether its timeout ended it, and what it wrote, cut in the middle.
type answer struct {
	ExitCode   int    `json:"exitCode"`
	TimedOut   bool   `json:"timedOut"`
	DurationMs int64  `json:"durationMs"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	Error      string `json:"error,omitempty"`
}

// check refuses a command the agent will not start, saying why in a sentence.
func (r *request) check() string {
	switch {
	case len(r.Argv) == 0 || r.Argv[0] == "":
		return "Name the program to run as the first element of argv."
	case !filepath.IsAbs(r.Dir) || !inside(filepath.Clean(r.Dir), config.WorkspaceTreePath):
		return fmt.Sprintf("Run commands in a directory under %s.", config.WorkspaceTreePath)
	}
	for _, kv := range r.Env {
		if k, _, ok := strings.Cut(kv, "="); !ok || k == "" || strings.HasPrefix(k, "ROTOR_") {
			return "Give each variable of env as NAME=value, and none starting with ROTOR_."
		}
	}
	maxSeconds := int(config.MaxWorkspaceStepTime / time.Second)
	if r.TimeoutSeconds <= 0 || r.TimeoutSeconds > maxSeconds {
		r.TimeoutSeconds = min(int(config.DefaultCommandTimeout/time.Second), maxSeconds)
	}
	if r.MaxOutputBytes <= 0 || r.MaxOutputBytes > config.MaxCommandOutput {
		r.MaxOutputBytes = config.MaxCommandOutput
	}
	return ""
}

func inside(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

// capped keeps the first half of its cap and the last half, so both a log's start and its verdict remain.
type capped struct {
	max        int
	head, tail []byte
	total      int
}

func (c *capped) Write(p []byte) (int, error) {
	written := len(p)
	c.total += written
	half := c.max / 2
	if room := half - len(c.head); room > 0 {
		n := min(room, len(p))
		c.head = append(c.head, p[:n]...)
		p = p[n:]
	}
	c.tail = append(c.tail, p...)
	if len(c.tail) > c.max-half {
		c.tail = append(c.tail[:0], c.tail[len(c.tail)-(c.max-half):]...)
	}
	return written, nil
}

// String is what was kept, with a line saying how much was left out between the halves.
func (c *capped) String() (string, bool) {
	if c.total <= c.max {
		return string(c.head) + string(c.tail), false
	}
	left := c.total - len(c.head) - len(c.tail)
	return string(c.head) + fmt.Sprintf("\n[... %d bytes left out ...]\n", left) + string(c.tail), true
}

// run starts argv in a process group of its own, so its timeout ends what it started too, and waits for it.
func run(ctx context.Context, req request) answer {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...)
	cmd.Dir, cmd.Env = req.Dir, append(os.Environ(), req.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	out := &capped{max: req.MaxOutputBytes}
	cmd.Stdout, cmd.Stderr = out, out
	start := time.Now()
	err := cmd.Run()
	a := answer{DurationMs: time.Since(start).Milliseconds(), TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded)}
	a.Output, a.Truncated = out.String()
	a.ExitCode = -1
	if cmd.ProcessState != nil {
		a.ExitCode = cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) && !a.TimedOut {
		a.Error = fmt.Sprintf("%s could not be started: %v.", req.Argv[0], err)
	}
	return a
}
