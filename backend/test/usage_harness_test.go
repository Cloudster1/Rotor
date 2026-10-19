//go:build integration

package test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

// fakeRunner speaks the runner protocol from the test, so a test can send exactly what a runner would, twice.
type fakeRunner struct {
	t         *testing.T
	stream    runnerpb.RunnerControlService_ConnectClient
	responses chan *runnerpb.ConnectResponse
}

// connectRunner opens the run's stream with a fresh bootstrap token and waits for its StartRun.
func connectRunner(t *testing.T, h *harness, runID uuid.UUID) *fakeRunner {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(runnerCA(t)))
	conn, err := grpc.NewClient(config.RunnerHost+config.DefaultRunnerAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: pool, ServerName: config.RunnerHost, MinVersion: tls.VersionTLS13})))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = conn.Close()
	})
	stream, err := runnerpb.NewRunnerControlServiceClient(conn).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{t: t, stream: stream, responses: make(chan *runnerpb.ConnectResponse, 64)}
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				close(f.responses)
				return
			}
			f.responses <- msg
		}
	}()
	if err := stream.Send(&runnerpb.ConnectRequest{Message: &runnerpb.ConnectRequest_Hello{Hello: &runnerpb.Hello{
		Credential: &runnerpb.Hello_BootstrapToken{BootstrapToken: h.bootstrap(t, runID)}}}}); err != nil {
		t.Fatal(err)
	}
	f.await(func(m *runnerpb.ConnectResponse) bool { return m.GetStartRun() != nil }, "StartRun")
	return f
}

// usage sends an llm.usage event numbered seq, as a runner reports one model call.
func (f *fakeRunner) usage(seq uint64, in, out int) {
	f.t.Helper()
	payload, _ := json.Marshal(map[string]any{"model": "stub-model", "inputTokens": in, "outputTokens": out})
	if err := f.stream.Send(&runnerpb.ConnectRequest{Seq: seq, Message: &runnerpb.ConnectRequest_Event{Event: &runnerpb.Event{
		Type: "llm.usage", NodeId: "ask", PayloadJson: string(payload)}}}); err != nil {
		f.t.Fatal(err)
	}
	f.await(func(m *runnerpb.ConnectResponse) bool { return m.Ack >= seq && m.Message == nil }, "the acknowledgement")
}

// await reads the stream until a message matches, failing after sandboxWait.
func (f *fakeRunner) await(match func(*runnerpb.ConnectResponse) bool, what string) *runnerpb.ConnectResponse {
	f.t.Helper()
	deadline := time.After(sandboxWait)
	for {
		select {
		case msg, open := <-f.responses:
			if !open {
				f.t.Fatalf("the stream ended before %s", what)
			}
			if match(msg) {
				return msg
			}
		case <-deadline:
			f.t.Fatalf("no %s within %s", what, sandboxWait)
		}
	}
}

// stubProvider adds a provider at the tenant's stub model, prices it when prices are given, and answers its name.
func stubProvider(t *testing.T, alice *browser, stub *toolStub, key string, inputPerMillion float64) string {
	t.Helper()
	provider := addProvider(t, alice, stub, key)
	if inputPerMillion > 0 {
		id := provider["id"].(string)
		if status, answer := alice.api(http.MethodPut, "/providers/"+id+"/prices", map[string]any{"prices": []map[string]any{
			{"model": "stub-model", "inputPerMillion": inputPerMillion, "outputPerMillion": 0}}}); status != http.StatusOK {
			t.Fatalf("pricing the provider = %d %v", status, answer)
		}
	}
	return provider["name"].(string)
}

// setLimits sets the limits on one person as alice, and clears them after the test.
func setLimits(t *testing.T, alice *browser, limits map[string]any) {
	t.Helper()
	if status, answer := alice.api(http.MethodPut, "/settings/limits", limits); status != http.StatusOK {
		t.Fatalf("setting the limits to %v = %d %v", limits, status, answer)
	}
	t.Cleanup(func() {
		alice.api(http.MethodPut, "/settings/limits", map[string]any{"dailyTokens": nil, "dailyCost": nil, "concurrentRuns": nil})
	})
}

// thinker is one agent node at the provider's stub model, after any nodes before, within limits.
func thinker(name, provider, limits, before string) string {
	entry := "think"
	if before != "" {
		entry = "first"
	}
	return `apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: ` + name + `}
model: {name: stub-model, provider: ` + provider + `}
limits: {maxSteps: 10, timeout: 5m` + limits + `}
entry: ` + entry + `
nodes:
` + before + `  - {id: think, type: agent, prompt: 'Think.'}
`
}
