// The trial of #132, reverted once docs/decisions.md records it: a tiny exec agent for the toolchain container.
// It runs one argv per request, without a shell, with a timeout and an output cap, and answers on a unix socket.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// request is one command: argv as given, never through a shell, in dir, with env added to the container's.
// The trial's limits: how long a killed command may hold its pipes, the largest request, the binary's mode.
const (
	waitDelay      = 5 * time.Second
	maxRequest     = 1 << 20
	binaryMode     = 0o555
	argsWithTarget = 3
	reapPoll       = 50 * time.Millisecond
)

type request struct {
	Argv           []string `json:"argv"`
	Dir            string   `json:"dir"`
	Env            []string `json:"env"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	MaxOutputBytes int      `json:"maxOutputBytes"`
}

type answer struct {
	ExitCode   int    `json:"exitCode"`
	TimedOut   bool   `json:"timedOut"`
	DurationMs int64  `json:"durationMs"`
	MaxRSSKB   int64  `json:"maxRssKB"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	Error      string `json:"error,omitempty"`
}

// capped keeps the first half of the cap and the last half, so both the start and the verdict of a long log remain.
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
	if len(c.tail) > half {
		c.tail = c.tail[len(c.tail)-half:]
	}
	return written, nil
}

func (c *capped) String() (string, bool) {
	if c.total <= c.max {
		return string(append(c.head, c.tail...)), false
	}
	return string(c.head) + fmt.Sprintf("\n[... %d bytes left out ...]\n", c.total-len(c.head)-len(c.tail)) + string(c.tail), true
}

func run(req request) answer {
	if len(req.Argv) == 0 {
		return answer{ExitCode: -1, Error: "argv is empty"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...)
	cmd.Dir, cmd.Env = req.Dir, append(os.Environ(), req.Env...)
	// A process group of its own, so a timeout ends what the command started too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	out := &capped{max: req.MaxOutputBytes}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = out, out, nil
	start := time.Now()
	err := cmd.Run()
	a := answer{DurationMs: time.Since(start).Milliseconds(), TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded)}
	a.Output, a.Truncated = out.String()
	if cmd.ProcessState != nil {
		a.ExitCode = cmd.ProcessState.ExitCode()
		if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
			a.MaxRSSKB = ru.Maxrss
		}
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		a.Error = err.Error()
	}
	return a
}

func serve(socket string) error {
	_ = os.Remove(socket)
	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /exec", func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(io.LimitReader(r.Body, maxRequest)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(run(req))
	})
	mux.HandleFunc("POST /reset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reset())
	})
	return http.Serve(l, mux)
}

// reset ends every process of the container but the agent, those that left their process group included, and reaps
// them, so nothing a step started is left for the next one.
func reset() map[string]any {
	var killed []string
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		comm, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if syscall.Kill(pid, syscall.SIGKILL) == nil {
			killed = append(killed, fmt.Sprintf("%d %s", pid, strings.TrimSpace(string(comm))))
		}
	}
	reaped := 0
	for deadline := time.Now().Add(waitDelay); time.Now().Before(deadline); {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil {
			break
		}
		if pid > 0 {
			reaped++
			continue
		}
		time.Sleep(reapPoll)
	}
	return map[string]any{"killed": killed, "reaped": reaped}
}

// install copies this binary to dst, so an init container can hand it to a toolchain image it knows nothing of.
func install(dst string) error {
	self, err := os.ReadFile("/proc/self/exe")
	if err != nil {
		return err
	}
	return os.WriteFile(dst, bytes.Clone(self), binaryMode)
}

func main() {
	var err error
	switch {
	case len(os.Args) == argsWithTarget && os.Args[1] == "install":
		err = install(os.Args[2])
	case len(os.Args) == argsWithTarget && os.Args[1] == "serve":
		err = serve(os.Args[2])
	default:
		err = errors.New("usage: agent install <path> | agent serve <socket>")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
