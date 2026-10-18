// Command worker carries runs from queued to their end: it starts each run's sandbox, replaces a lost one, ends runs
// past their time and cleans up after them. Any number may run; each run is leased to one at a time.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudster1/rotor/backend/internal/boot"
	"github.com/cloudster1/rotor/backend/internal/cache"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/metrics"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/runs"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
	"github.com/cloudster1/rotor/backend/internal/sandbox/docker"
	"github.com/cloudster1/rotor/backend/internal/sandbox/kube"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/version"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// The container health check runs the binary itself, so the image needs no shell tools.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		return healthcheck()
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)
	backend, err := sandboxes(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer cluster.Close()
	if cfg.Env != config.EnvDevelopment {
		if err := cluster.RefuseSuperuser(ctx); err != nil {
			return err
		}
	}
	secrets, err := boot.Secrets(ctx, cfg, log)
	if err != nil {
		return err
	}
	if secrets == nil {
		return errors.New("ROTOR_OPENBAO_ADDR is not set, so the worker cannot hand sandboxes the runner CA; set it to OpenBao's address")
	}

	// Losing Valkey costs only latency: whoever follows a run also reads its events on their own.
	store, err := cache.Open(cfg.Valkey.URL, log)
	if err != nil {
		return err
	}
	defer store.Close()

	registry := tools.NewService(cluster, secrets, boot.ToolClient(cfg), cfg.ConnectionCallbackURL(), cfg.Env == config.EnvDevelopment)
	people := connections.NewService(cluster, secrets, registry, boot.ToolClient(cfg), cfg.ConnectionCallbackURL(), log)
	people.Runs = secrets
	worker := runs.NewWorker(cluster, backend, runs.Launch{
		Image: cfg.Sandbox.Image, Dial: cfg.Runner.Dial, Stack: cfg.Sandbox.Stack, CA: secrets.RunnerCA, OpenBao: cfg.OpenBao.Addr,
		Limits: sandbox.Limits{MemoryBytes: config.DefaultSandboxMemory, MilliCPUs: config.DefaultSandboxMilliCPUs, PIDs: config.DefaultSandboxPIDs},
	}, log)
	// A run's tokens end with it, revoked at their tools, and when it starts waiting (#28).
	worker.Teardown, worker.Waiting = people.RevokeRun, people.RevokeRun
	worker.Events = store
	counted := &metrics.Registry{}
	worker.Metrics = runs.NewWorkerMetrics(counted, cluster)
	go metrics.Serve(ctx, cfg.Worker.MetricsAddr, counted, log)
	worker.Passed = func() {
		now := time.Now()
		if err := os.Chtimes(config.WorkerAliveFile, now, now); errors.Is(err, os.ErrNotExist) {
			_ = os.WriteFile(config.WorkerAliveFile, nil, aliveFileMode)
		}
	}
	build := version.Current()
	log.Info("worker starting", "env", cfg.Env, "version", build.Version, "commit", build.Commit, "stack", cfg.Sandbox.Stack)
	worker.Run(ctx)
	return nil
}

// sandboxes is the backend the settings name: pods in a cluster, containers on the Docker daemon in development.
func sandboxes(cfg config.Config) (sandbox.Backend, error) {
	switch cfg.Sandbox.Backend {
	case config.SandboxKubernetes:
		return kube.New(kube.Config{Namespace: cfg.Sandbox.Namespace, RuntimeClass: cfg.Sandbox.RuntimeClass,
			ServiceAccount: cfg.Sandbox.ServiceAccount, Kubeconfig: cfg.Sandbox.Kubeconfig})
	case config.SandboxDocker:
		return docker.New(cfg.Sandbox.Socket, cfg.Sandbox.Network), nil
	}
	return nil, errors.New("ROTOR_SANDBOX_BACKEND is not set, so the worker has nowhere to start sandboxes; set it to kubernetes, or docker in development")
}

// aliveFileMode is the file the worker touches, readable by the health check alone.
const aliveFileMode = 0o600

// healthcheck fails unless the worker finished a pass within config.WorkerAliveWithin.
func healthcheck() error {
	info, err := os.Stat(config.WorkerAliveFile)
	if err != nil {
		return errors.New("the worker has not finished a pass yet")
	}
	if age := time.Since(info.ModTime()); age > config.WorkerAliveWithin {
		return fmt.Errorf("the worker's last pass was %s ago", age.Round(time.Second))
	}
	return nil
}
