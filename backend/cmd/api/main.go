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

	"github.com/cloudster1/rotor/backend/internal/auth"
	"github.com/cloudster1/rotor/backend/internal/cache"
	"github.com/cloudster1/rotor/backend/internal/catalog"
	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/connections"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/httpapi"
	"github.com/cloudster1/rotor/backend/internal/observability"
	"github.com/cloudster1/rotor/backend/internal/oidc"
	"github.com/cloudster1/rotor/backend/internal/sandbox"
	"github.com/cloudster1/rotor/backend/internal/sandbox/docker"
	"github.com/cloudster1/rotor/backend/internal/tools"
	"github.com/cloudster1/rotor/backend/internal/vault"
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
	}
	secrets, err := openSecrets(ctx, cfg, log)
	if err != nil {
		return err
	}
	store, err := cache.Open(cfg.Valkey.URL, log)
	if err != nil {
		return err
	}
	defer store.Close()
	var catalogs workflows.CatalogSource
	if secrets != nil {
		server.Vault = secrets
		registry := tools.NewService(cluster, secrets, toolClient(cfg), cfg.ConnectionCallbackURL(), cfg.Env == config.EnvDevelopment)
		people := connections.NewService(cluster, secrets, registry, toolClient(cfg), cfg.ConnectionCallbackURL(), log)
		registry.Connections = people
		sessions.OnSwitchOff = people.EndPersonConnections
		server.Tools, server.Connections = registry, people
		reader := catalog.NewService(registry, people, toolClient(cfg), store, log)
		server.Catalogs, catalogs = reader, reader
	}
	sweepSandboxes(ctx, cfg, log)
	server.Workflows = workflows.NewService(cluster, catalogs)
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

// openSecrets builds the OpenBao client and keeps it signed in until ctx ends; nil without OpenBao configured.
func openSecrets(ctx context.Context, cfg config.Config, log *slog.Logger) (*vault.Client, error) {
	if !cfg.SecretsConfigured() {
		log.Warn("ROTOR_OPENBAO_ADDR is not set, so nobody can connect a tool; set it to OpenBao's address")
		return nil, nil
	}
	client, err := vault.New(vault.Config{
		OpenBao: cfg.OpenBao, Issuer: cfg.OIDC.Issuer, Log: log,
		HTTP: oidc.HTTPClient(oidc.ParseRewrites(cfg.OIDC.Backchannel)),
	})
	if err != nil {
		return nil, err
	}
	go client.Run(ctx)
	return client, nil
}

// sweepSandboxes destroys this installation's sandboxes left from before a restart; M3 keeps those of live runs.
func sweepSandboxes(ctx context.Context, cfg config.Config, log *slog.Logger) {
	if cfg.Sandbox.Backend != config.SandboxDocker {
		return
	}
	backend := docker.New(cfg.Sandbox.Socket, cfg.Sandbox.Network)
	destroyed, err := sandbox.Sweep(ctx, backend, map[string]string{sandbox.LabelStack: cfg.Sandbox.Stack}, nil)
	if err != nil {
		log.Warn("cannot sweep the sandboxes left from before", "error", err)
		return
	}
	log.Info("swept the sandboxes left from before", "destroyed", destroyed)
}

// toolClient reaches the tools an administrator adds, at the addresses Rotor can reach them.
func toolClient(cfg config.Config) *http.Client {
	return &http.Client{Timeout: config.ToolTimeout, Transport: oidc.HTTPClient(oidc.ParseRewrites(cfg.Tools.Backchannel)).Transport}
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
