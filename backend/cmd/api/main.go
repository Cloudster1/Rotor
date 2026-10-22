// Command api serves Rotor's HTTP API. Run with -healthcheck it probes the
// local readiness endpoint instead, for the container's health check.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cloudster1/rotor/backend/internal/approvals"
	"github.com/cloudster1/rotor/backend/internal/audit"
	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/boot"
	"github.com/cloudster1/rotor/backend/internal/cache"
	"github.com/cloudster1/rotor/backend/internal/catalog"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/envelope"
	"github.com/cloudster1/rotor/backend/internal/hooks"
	"github.com/cloudster1/rotor/backend/internal/httpapi"
	"github.com/cloudster1/rotor/backend/internal/langsmith"
	"github.com/cloudster1/rotor/backend/internal/metrics"
	"github.com/cloudster1/rotor/backend/internal/netguard"
	"github.com/cloudster1/rotor/backend/internal/oauth"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/oidc"
	"github.com/cloudster1/rotor/backend/internal/providers"
	"github.com/cloudster1/rotor/backend/internal/retention"
	"github.com/cloudster1/rotor/backend/internal/runnerctl"
	"github.com/cloudster1/rotor/backend/internal/runs"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/triggers"
	"github.com/cloudster1/rotor/backend/internal/usage"
	"github.com/cloudster1/rotor/backend/internal/version"
	"github.com/cloudster1/rotor/backend/internal/workflows"
)

// devOrigins are where the web client's development server runs.
var devOrigins = []string{"http://localhost:5173", "http://127.0.0.1:5173"}

func main() {
	if err := run(); err != nil {
		slog.Error("api exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// The container health check runs the binary itself, so the image needs no curl.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		return healthcheck()
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel, cfg.IsProduction())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Traces go to the collector when one is set (#37); the api's metrics are served beside its public listener.
	tracing, err := observability.SetupTracing(ctx, "rotor-api", cfg.Env, cfg.Telemetry, log, nil)
	if err != nil {
		return err
	}
	defer flushTraces(ctx, tracing)
	counted := &metrics.Registry{}
	mintFailures := counted.Counter("rotor_token_mint_failures_total", "Tokens a run asked for and did not get, by why.", "reason")
	go metrics.Serve(ctx, cfg.Telemetry.APIMetricsAddr, counted, log)

	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		return err
	}
	defer cluster.Close()
	// Outside development a role that ignores row level security would make every policy decoration.
	if cfg.Env != config.EnvDevelopment {
		if err := cluster.RefuseSuperuser(ctx); err != nil {
			return err
		}
	}

	if !cfg.SignInConfigured() {
		log.Warn("ROTOR_OIDC_ISSUER is not set, so nobody can sign in; set it to the identity provider's issuer")
	}
	sessions := auth.NewService(cluster, cfg.Auth.SessionTTL)
	server := &httpapi.Server{
		DB:             cluster,
		Log:            log,
		Sessions:       sessions,
		SignIn:         oidc.NewService(cluster, sessions, cfg.OIDC),
		SessionTTL:     cfg.Auth.SessionTTL,
		CookieName:     cfg.Auth.SessionCookie,
		Secure:         cfg.Auth.SecureCookies,
		AppBaseURL:     cfg.AppBaseURL,
		CheckOrigin:    cfg.IsProduction(),
		RequestTimeout: cfg.RequestTimeout,
		TrustedProxies: cfg.TrustedProxies,
		// Assistants connect through Rotor's own authorization server (#47); a client's metadata document is fetched
		// through a client that refuses Rotor's own network, beyond what the operator allows (#114).
		OAuth: oauth.NewService(cluster, cfg.AppBaseURL, httpapi.APIPrefix,
			oauth.HTTPFetcher(netguard.Client(config.OAuthMetadataFetchTimeout, netguard.ParseAllow(cfg.OAuthFetchAllow)))),
	}
	secrets, err := boot.Secrets(ctx, cfg, log)
	if err != nil {
		return err
	}
	store, err := cache.Open(cfg.Valkey.URL, log)
	if err != nil {
		return err
	}
	defer store.Close()
	// Without OpenBao there are no tools, so workflows are checked without them and nobody starts a run.
	workflowService := workflows.NewService(cluster, nil)
	if secrets != nil {
		server.Vault = secrets
		registry := tools.NewService(cluster, secrets, boot.ToolClient(cfg), cfg.ConnectionCallbackURL(), cfg.Env == config.EnvDevelopment)
		people := connections.NewService(cluster, secrets, registry, boot.ToolClient(cfg), cfg.ConnectionCallbackURL(), log)
		people.Runs = secrets
		registry.Connections = people
		sessions.OnSwitchOff = people.EndPersonConnections
		server.Tools, server.Connections = registry, people
		hookService := hooks.NewService(cluster, secrets)
		server.Hooks = hookService
		reader := catalog.NewService(registry, people, boot.ToolClient(cfg), store, log)
		server.Catalogs = reader
		workflowService = workflows.NewService(cluster, reader)
		// Every run's data is sealed with a key of its own, which OpenBao wraps (#41).
		keys := envelope.NewKeys(secrets)
		runService := runs.NewService(cluster, keys, workflowService, registry, people, store)
		server.Runs = runService
		triggerService := triggers.NewService(cluster, workflowService, reader, runService)
		triggerService.CheckEvent = hookService.CheckEvent(people)
		server.Triggers = triggerService
		server.Approvals = approvals.NewService(cluster, keys, store)
		runners := runnerctl.NewServer(cluster, keys, registry, people, log)
		reach := oidc.ParseRewrites(cfg.Tools.Backchannel)
		runners.Reach = func(address string) string { return oidc.Rewrite(reach, address) }
		runners.Checkpoints = runs.NewCheckpoints(cluster, keys)
		runners.Signals, runners.Wrap, runners.Events, runners.Host = store, secrets, store, cfg.Runner.Host
		runners.ApprovalTTL = cfg.Approvals.TTL
		smith := langsmith.NewService(cluster, secrets, cfg.Telemetry.LangSmithEndpoint)
		server.LangSmith, server.LangSmithEndpoint = smith, cfg.Telemetry.LangSmithEndpoint
		runners.Telemetry = &langsmith.Forwarder{Tracing: tracing, LangSmith: smith, Log: log}
		runners.MintFailed = func(reason string) { mintFailures.Add(reason, 1) }
		models := providers.NewService(cluster, secrets, boot.ToolClient(cfg))
		server.Providers, runners.Models, workflowService.Models = models, models, models
		// Usage (#38): a run's person's quota and its model's prices are checked as it starts.
		usageService := usage.NewService(cluster, models)
		server.Usage, runService.Usage = usageService, usageService
		runService.Catalogs = reader
		go func() {
			if err := runners.Serve(ctx, cfg.Runner.Addr, secrets); err != nil {
				log.Error("the runner protocol stopped", "error", err)
			}
		}()
	}
	server.Workflows = workflowService
	server.Audit = audit.NewService(cluster)
	server.Retention = retention.NewService(cluster, log)
	origins := cfg.CORSOrigins
	if len(origins) == 0 && !cfg.IsProduction() {
		origins = devOrigins
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(origins),
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// Exactly one value is ever sent on this channel; closing it would read as a clean exit.
	serveErr := make(chan error, 1)
	go func() {
		build := version.Current()
		log.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "version", build.Version, "commit", build.Commit)
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return errors.New("http server stopped serving without being asked to")
	case <-ctx.Done():
		log.Info("shutdown requested, draining connections")
	}

	// Give in-flight requests a chance to finish before the pools close.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.ShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("api stopped cleanly")
	return nil
}

// healthcheck probes the local readiness endpoint and fails unless it is ready.
func healthcheck() error {
	addr := os.Getenv("ROTOR_HTTP_ADDR")
	if addr == "" {
		addr = config.DefaultHTTPAddr
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := &http.Client{Timeout: config.HealthcheckWait}
	resp, err := client.Get("http://" + addr + "/readyz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned %s", resp.Status)
	}
	return nil
}

// flushTraces sends what the tracer still holds as the process ends.
func flushTraces(ctx context.Context, tracing *observability.Tracing) {
	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.ShutdownGrace)
	defer cancel()
	_ = tracing.Shutdown(flush)
}
