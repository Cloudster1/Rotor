package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// reapFor is how long a reset waits for the processes it killed to be reaped; reapPoll how often it looks.
const (
	reapFor  = 5 * time.Second
	reapPoll = 50 * time.Millisecond
	dirMode  = 0o700
)

// resetResult is what a reset ended and removed.
type resetResult struct {
	Killed  int `json:"killed"`
	Reaped  int `json:"reaped"`
	Cleared int `json:"cleared"`
}

// errNotInit refuses a reset outside a container of its own, where killing every other process would reach the host's.
var errNotInit = errors.New("The agent ends other processes only as the first process of its own container; start it as the container's entrypoint.")

// reset ends every process of the container but the agent, those that left their process group included, reaps
// them, and empties the caches, so nothing a step started or built is left for the next one.
func reset(cache string) (resetResult, error) {
	if os.Getpid() != 1 {
		return resetResult{}, errNotInit
	}
	var out resetResult
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == 1 {
			continue
		}
		if syscall.Kill(pid, syscall.SIGKILL) == nil {
			out.Killed++
		}
	}
	for deadline := time.Now().Add(reapFor); time.Now().Before(deadline); {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil {
			break
		}
		if pid > 0 {
			out.Reaped++
			continue
		}
		time.Sleep(reapPoll)
	}
	cleared, err := emptyDir(cache)
	out.Cleared = cleared
	return out, err
}

// emptyDir empties dir, the module cache's read-only directories included, and answers how many entries it removed.
func emptyDir(dir string) (int, error) {
	if dir == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, dirMode)
			}
			return nil
		})
		errs = append(errs, os.RemoveAll(path))
	}
	return len(entries), errors.Join(errs...)
}
