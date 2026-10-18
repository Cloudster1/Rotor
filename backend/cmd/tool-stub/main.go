// Command tool-stub stands in for the tools Rotor connects to, in the test suites (#8): each tenant is an OAuth
// authorization server and an MCP endpoint that behave as Exciter's do. Adapted from Exciter's cmd/armature-stub.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/toolstub"
)

// Settings, from the environment like everything in the stack.
const (
	addrEnv        = "TOOLSTUB_ADDR"
	controlAddrEnv = "TOOLSTUB_CONTROL_ADDR"
	toolsListEnv   = "TOOLSTUB_TOOLS_LIST"
	// defaultToolsList is where the backend image carries the snapshot of Exciter's tools/list.
	defaultToolsList = "/app/tools/exciter/tools-list.json"
)

func main() {
	if err := run(); err != nil {
		slog.Error("tool-stub exited", "error", err)
		os.Exit(1)
	}
}

func setting(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run() error {
	addr, controlAddr := setting(addrEnv, config.ToolStubAddr), setting(controlAddrEnv, config.ToolStubControlAddr)
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		return healthcheck(controlAddr)
	}
	list, err := os.ReadFile(setting(toolsListEnv, defaultToolsList))
	if err != nil {
		return fmt.Errorf("read the tools/list snapshot; set %s to api/tools/exciter/tools-list.json: %w", toolsListEnv, err)
	}
	stub, err := toolstub.New(list)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	servers := []*http.Server{
		{Addr: addr, Handler: stub.Public(), ReadHeaderTimeout: config.ReadHeaderTimeout},
		{Addr: controlAddr, Handler: stub.Control(), ReadHeaderTimeout: config.ReadHeaderTimeout},
	}
	served := make(chan error, len(servers))
	for _, srv := range servers {
		go func() {
			slog.Info("tool-stub listening", "addr", srv.Addr)
			served <- srv.ListenAndServe()
		}()
	}
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.ShutdownGrace)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	return nil
}

// healthcheck lets the container check itself without curl in the image.
func healthcheck(addr string) error {
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	resp, err := (&http.Client{Timeout: config.HealthcheckWait}).Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("the stub answered %s", resp.Status)
	}
	return nil
}
