package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func respondTo(t *testing.T, ctx context.Context, err error) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	r := httptest.NewRequest(http.MethodGet, "/api/v1/workflows", nil).WithContext(context.WithValue(ctx, ctxLogger, log))
	w := httptest.NewRecorder()
	respondError(w, r, err)
	return w, logs.String()
}

// A client that hung up is not a server error: nothing is logged at error level and no 500 is counted.
func TestACancelledRequestIsNotLoggedAsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w, logs := respondTo(t, ctx, fmt.Errorf("listing workflows: %w", context.Canceled))
	if logs != "" {
		t.Errorf("a cancelled request logged %q, want nothing above debug", logs)
	}
	if w.Code != statusClientClosed {
		t.Errorf("a cancelled request answered %d, want %d", w.Code, statusClientClosed)
	}
}

// A cancellation inside the server while the client still waits is a real failure and stays one.
func TestACancellationWhileTheClientWaitsIsStillAnError(t *testing.T) {
	w, logs := respondTo(t, context.Background(), context.Canceled)
	if w.Code != http.StatusInternalServerError || !strings.Contains(logs, "level=ERROR") {
		t.Errorf("answered %d, logged %q; want 500 logged as an error", w.Code, logs)
	}
}
