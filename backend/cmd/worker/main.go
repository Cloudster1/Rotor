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
	"github.com/cloudster1/rotor/backend/internal/catalog"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/hooks"
	"github.com/cloudster1/rotor/backend/internal/metrics"
	"github.com/cloudster1/rotor/backend/internal/oauth"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/providers"
	"github.com/cloudster1/rotor/backend/internal/retention"
	"github.com/cloudster1/rotor/backend/internal/runs"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
	"github.com/cloudster1/rotor/backend/internal/sandbox/docker"
	"github.com/cloudster1/rotor/backend/internal/sandbox/kube"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/triggers"
	"github.com/cloudster1/rotor/backend/internal/usage"
	"github.com/cloudster1/rotor/backend/internal/version"
	"github.com/cloudster1/rotor/backend/internal/workflows"
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

	// Starting a run's sandbox and cleaning up after it join the run's trace when a collector is set (#37).
	tracing, err := observability.SetupTracing(ctx, "rotor-worker", cfg.Env, cfg.Telemetry, log, nil)
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.ShutdownGrace)
		defer cancel()
		_ = tracing.Shutdown(flush)
	}()

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
	// The worker writes runs' events too, sealed with each run's own key (#41).
	keys := envelope.NewKeys(secrets)
	worker := runs.NewWorker(cluster, keys, backend, runs.Launch{
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
	// Triggers (#34): schedules queue firings, and each firing starts its run as the trigger's owner.
	reader := catalog.NewService(registry, people, boot.ToolClient(cfg), store, log)
	workflowService := workflows.NewService(cluster, reader)
	starter := runs.NewService(cluster, keys, workflowService, registry, people, store)
	// A trigger's run is held to its person's quota and its model's prices as one started by hand (#38).
	starter.Usage = usage.NewService(cluster, providers.NewService(cluster, secrets, boot.ToolClient(cfg)))
	// A trigger's firing starts its run through the same checks, the tools read afresh as its owner (#90).
	starter.Catalogs = reader
	scheduler := triggers.NewScheduler(cluster, log)
	// Event triggers (#35): checked as for schedules, and started only for an event whose subject the owner may read.
	hookService := hooks.NewService(cluster, secrets)
	triggerService := triggers.NewService(cluster, workflowService, reader, starter)
	triggerService.CheckEvent = hookService.CheckEvent(people)
	firer := triggers.NewFirer(triggerService, log)
	firer.Probe = hooks.Probe(reader)
	fired := counted.Counter("rotor_trigger_fires_total", "Firings of triggers that ended, by outcome.", "outcome")
	firer.Fired = func(outcome string) { fired.Add(outcome, 1) }
	// Retention (#41): a finished run's data is deleted once kept as long as the installation or its workflow says.
	kept := retention.NewService(cluster, log)
	// Assistants (#47): requests, codes and tokens that ran out go, and clients nobody holds.
	swept := oauth.NewSweeper(cluster)
	worker.Also = func(ctx context.Context) error {
		_, purged := hookService.Purge(ctx)
		return errors.Join(scheduler.Sweep(ctx), scheduler.Schedule(ctx), firer.Fire(ctx), purged, kept.Sweep(ctx), swept.Sweep(ctx))
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
