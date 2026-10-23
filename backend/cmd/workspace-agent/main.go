// Command workspace-agent runs a workspace step's commands in a toolchain container (#136): an init container copies
// it in, the container starts it, and only the runner, holding the sandbox's secret, may ask it to run anything.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// What the agent reads from its environment and removes, so no command it runs inherits them.
const (
	envListen = "ROTOR_AGENT_LISTEN"
	envSecret = "ROTOR_WORKSPACE_SECRET"
	envCache  = "ROTOR_AGENT_CACHE"
)

// Sizes and times of the agent's own: the largest request, how long a header may take, the binary's mode.
const (
	maxRequestBytes   = 1 << 20
	readHeaderTimeout = 10 * time.Second
	binaryMode        = 0o555
	argsWithTarget    = 3
	argsServe         = 2
)

func main() {
	var err error
	switch {
	case len(os.Args) == argsWithTarget && os.Args[1] == "install":
		err = install(os.Args[2])
	case len(os.Args) == argsServe && os.Args[1] == "serve":
		err = serveFromEnv()
	default:
		err = errors.New("usage: workspace-agent install <path> | workspace-agent serve")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// install copies this binary to dst, so an init container hands it to a toolchain image it knows nothing of.
func install(dst string) error {
	self, err := os.ReadFile("/proc/self/exe")
	if err != nil {
		return fmt.Errorf("read the agent's own binary: %w", err)
	}
	return os.WriteFile(dst, self, binaryMode)
}

// agent serves one toolchain container: its secret, the cache directory a reset empties, and a lock that keeps a
// reset from reaping a command's own process while it runs.
type agent struct {
	secret []byte
	cache  string
	busy   chan struct{}
}

func newAgent(secret, cache string) *agent {
	return &agent{secret: []byte(secret), cache: cache, busy: make(chan struct{}, 1)}
}

func serveFromEnv() error {
	listen, secret, cache := os.Getenv(envListen), os.Getenv(envSecret), os.Getenv(envCache)
	for _, k := range []string{envListen, envSecret, envCache} {
		_ = os.Unsetenv(k)
	}
	if listen == "" || secret == "" {
		return fmt.Errorf("set %s and %s, as the sandbox does", envListen, envSecret)
	}
	if host, _, err := net.SplitHostPort(listen); err != nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("%s must be a loopback address and port, such as 127.0.0.1:%d", envListen, config.WorkspaceAgentPortBase)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	l, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: newAgent(secret, cache).routes(), ReadHeaderTimeout: readHeaderTimeout}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	slog.Info("workspace agent listening", "addr", listen)
	if err := server.Serve(l); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (a *agent) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /exec", a.authorized(a.handleExec))
	mux.HandleFunc("POST /reset", a.authorized(a.handleReset))
	return mux
}

// authorized lets only a caller holding the sandbox's secret through: code nodes share the loopback, not the secret.
func (a *agent) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), a.secret) != 1 {
			http.Error(w, "Show the sandbox's secret to run commands here.", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (a *agent) handleExec(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes)).Decode(&req); err != nil {
		http.Error(w, "The command could not be read: "+err.Error(), http.StatusBadRequest)
		return
	}
	if problem := req.check(); problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}
	a.busy <- struct{}{}
	defer func() { <-a.busy }()
	respond(w, run(r.Context(), req))
}

func (a *agent) handleReset(w http.ResponseWriter, _ *http.Request) {
	a.busy <- struct{}{}
	defer func() { <-a.busy }()
	result, err := reset(a.cache)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	respond(w, result)
}

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
