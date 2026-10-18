package runnerctl

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/db"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
)

// Server is the control plane's side of the runner protocol.
type Server struct {
	runnerpb.UnimplementedRunnerControlServiceServer

	DB    *db.Cluster
	Tools ToolFinder
	Mint  Minter
	Log   *slog.Logger
	// Checkpoints answers the runner's checkpoint requests (#19); nil refuses them.
	Checkpoints Checkpoints
	// Reach turns a tool's public address into one a sandbox reaches, as the api's own calls do.
	Reach func(string) string
	// Models resolves a run's model and its key (#20); nil starts runs without one.
	Models Models
	// Signals wakes the stream of a run when another replica queued a control message for it; nil polls alone.
	Signals Signals

	store store
	mu    sync.Mutex
	live  map[uuid.UUID]*liveStream
}

// liveStream is the stream a run is served on, so a newer one can end it.
type liveStream struct{ cancel context.CancelFunc }

// Signals is pub/sub across api replicas; *cache.Cache is the real one.
type Signals interface {
	Subscribe(ctx context.Context, channel string) <-chan []byte
}

// Checkpoints keeps a run's checkpoints for its runner (#19).
type Checkpoints interface {
	Handle(ctx context.Context, runID uuid.UUID, req *runnerpb.ConnectRequest) *runnerpb.ConnectResponse
}

// NewServer returns the protocol's server; Serve puts it on the network.
func NewServer(cluster *db.Cluster, finder ToolFinder, minter Minter, log *slog.Logger) *Server {
	return &Server{DB: cluster, Tools: finder, Mint: minter, Log: log, store: store{db: cluster}, live: map[uuid.UUID]*liveStream{}}
}

// claim makes this stream the run's one, ending any earlier stream of the run, which a reconnect leaves behind.
func (s *Server) claim(runID uuid.UUID, cancel context.CancelFunc) *liveStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if earlier, ok := s.live[runID]; ok {
		earlier.cancel()
	}
	live := &liveStream{cancel: cancel}
	s.live[runID] = live
	return live
}

func (s *Server) release(runID uuid.UUID, live *liveStream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live[runID] == live {
		delete(s.live, runID)
	}
}

// Connect serves one runner: Hello, then StartRun for a sandbox just started or whatever a reconnecting one has not
// acknowledged, then its messages until it ends the stream, another stream of the run replaces it, or it goes quiet.
func (s *Server) Connect(stream runnerpb.RunnerControlService_ConnectServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "a runner starts with Hello")
	}
	runID, err := s.redeemHello(ctx, hello)
	if err != nil {
		return status.Error(codes.Unauthenticated, "the runner's credential is unknown, used or expired")
	}
	r, err := s.loadRun(ctx, runID)
	if err != nil {
		return s.fail(runID, "load the run", err)
	}
	// A sandbox of a run that ended or waits is left over; refusing it ends its runner for good.
	if r.Status != StatusStarting && r.Status != StatusRunning {
		return status.Error(codes.Unauthenticated, "the run is "+r.Status+", so this sandbox has nothing to do")
	}
	live := s.claim(runID, cancel)
	defer s.release(runID, live)

	session, err := issue(ctx, s.DB, runID, kindSession, r.sessionExpiry())
	if err != nil {
		return s.fail(runID, "issue a session", err)
	}
	last, err := s.store.lastReceived(ctx, runID)
	if err != nil {
		return s.fail(runID, "read the last message", err)
	}
	if err := stream.Send(&runnerpb.ConnectResponse{Ack: last, Message: &runnerpb.ConnectResponse_HelloAck{HelloAck: &runnerpb.HelloAck{
		RunId: runID.String(), SessionToken: session, SessionExpiresAt: timestamppb.New(r.sessionExpiry())}}}); err != nil {
		return err
	}
	bindings, resolved, err := s.resolve(ctx, r)
	if err != nil {
		return s.fail(runID, "resolve the bindings", err)
	}
	s.resolveModel(ctx, r)
	var pending []*runnerpb.ConnectResponse
	if r.Status == StatusStarting {
		deadline := time.Now().Add(r.spec.Limits.TimeoutDuration() + config.RunDeadlineGrace)
		start, err := s.store.supersede(ctx, runID, startRun(r, bindings, s.modelConfig(r)), deadline)
		if err != nil {
			return s.fail(runID, "start the run", err)
		}
		pending = append(pending, start)
	} else {
		if err := s.store.seen(ctx, runID); err != nil {
			return s.fail(runID, "record the runner's return", err)
		}
		if pending, err = s.store.unsent(ctx, runID, 0); err != nil {
			return s.fail(runID, "read what the runner has not acknowledged", err)
		}
	}
	var sent uint64
	for _, msg := range pending {
		msg.Ack, sent = last, max(sent, msg.Seq)
		if err := stream.Send(msg); err != nil {
			return err
		}
	}
	if r.model != nil {
		grant, err := s.secret(ctx, r)
		if err != nil {
			return s.fail(runID, "read the provider key", err)
		}
		if err := stream.Send(grant); err != nil {
			return err
		}
	}
	err = s.serve(ctx, stream, r, resolved, sent)
	if status.Code(err) != codes.Aborted {
		if markErr := s.store.dropped(context.WithoutCancel(ctx), runID); markErr != nil {
			s.Log.Error("cannot record a dropped stream", "run", runID, "error", markErr)
		}
	}
	return err
}

func (s *Server) redeemHello(ctx context.Context, hello *runnerpb.Hello) (uuid.UUID, error) {
	switch credential := hello.Credential.(type) {
	case *runnerpb.Hello_BootstrapToken:
		return redeem(ctx, s.DB, credential.BootstrapToken, kindBootstrap)
	case *runnerpb.Hello_SessionToken:
		return redeem(ctx, s.DB, credential.SessionToken, kindSession)
	}
	return uuid.Nil, errBadCredential
}

// serve handles the runner's messages and sends the control messages queued for it, such as a Cancel from another
// api replica; a runner silent for its heartbeats' window is lost and the stream ends.
func (s *Server) serve(ctx context.Context, stream runnerpb.RunnerControlService_ConnectServer, r *run, bindings map[string]binding, sent uint64) error {
	messages, failures := make(chan *runnerpb.ConnectRequest), make(chan error, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				failures <- err
				return
			}
			select {
			case messages <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	var signals <-chan []byte
	if s.Signals != nil {
		signals = s.Signals.Subscribe(ctx, ControlChannel(r.ID))
	}
	// A signal can be lost, so the queue is also read on every heartbeat's beat.
	poll := time.NewTicker(config.HeartbeatInterval)
	defer poll.Stop()
	window := config.HeartbeatInterval * config.MissedHeartbeats
	watchdog := time.NewTimer(window)
	defer watchdog.Stop()
	for {
		select {
		case <-ctx.Done():
			return status.Error(codes.Aborted, "another stream of the run took over")
		case err := <-failures:
			return err
		case <-watchdog.C:
			if err := s.store.lost(context.WithoutCancel(ctx), r.ID); err != nil {
				s.Log.Error("cannot record a lost runner", "run", r.ID, "error", err)
			}
			return status.Error(codes.DeadlineExceeded, "the runner missed its heartbeats")
		case <-signals:
			if err := s.sendQueued(ctx, stream, r.ID, &sent); err != nil {
				return err
			}
		case <-poll.C:
			if err := s.sendQueued(ctx, stream, r.ID, &sent); err != nil {
				return err
			}
		case msg := <-messages:
			watchdog.Reset(window)
			if err := s.handle(ctx, stream, r, bindings, msg); err != nil {
				return err
			}
		}
	}
}

// sendQueued sends the control messages queued after the last this stream sent.
func (s *Server) sendQueued(ctx context.Context, stream runnerpb.RunnerControlService_ConnectServer, runID uuid.UUID, sent *uint64) error {
	queued, err := s.store.unsent(ctx, runID, *sent)
	if err != nil {
		return s.fail(runID, "read the queued control messages", err)
	}
	for _, msg := range queued {
		if err := stream.Send(msg); err != nil {
			return err
		}
		*sent = max(*sent, msg.Seq)
	}
	return nil
}

func (s *Server) handle(ctx context.Context, stream runnerpb.RunnerControlService_ConnectServer, r *run, bindings map[string]binding, msg *runnerpb.ConnectRequest) error {
	if err := s.store.acknowledge(ctx, r.ID, msg.Ack); err != nil {
		return s.fail(r.ID, "store an acknowledgement", err)
	}
	var apply func(ctx context.Context, tx db.DBTX) error
	switch m := msg.Message.(type) {
	case *runnerpb.ConnectRequest_Event:
		apply = func(ctx context.Context, tx db.DBTX) error {
			return appendEvent(ctx, tx, r.ID, m.Event.Type, m.Event.NodeId, []byte(m.Event.PayloadJson))
		}
	case *runnerpb.ConnectRequest_Done:
		apply = func(ctx context.Context, tx db.DBTX) error { return finish(ctx, tx, r.ID, m.Done) }
	case *runnerpb.ConnectRequest_TokenRequest:
		return stream.Send(s.grant(ctx, r, bindings, m.TokenRequest.Binding))
	case *runnerpb.ConnectRequest_CheckpointGet, *runnerpb.ConnectRequest_CheckpointList,
		*runnerpb.ConnectRequest_CheckpointPut, *runnerpb.ConnectRequest_CheckpointWrites:
		if s.Checkpoints == nil {
			return status.Error(codes.Unimplemented, "this control plane keeps no checkpoints")
		}
		return stream.Send(s.Checkpoints.Handle(ctx, r.ID, msg))
	}
	if apply == nil || msg.Seq == 0 {
		return nil
	}
	if _, err := s.store.received(ctx, r.ID, msg.Seq, apply); err != nil {
		if errors.Is(err, errGap) {
			return status.Error(codes.FailedPrecondition, err.Error())
		}
		return s.fail(r.ID, "store a runner message", err)
	}
	return stream.Send(&runnerpb.ConnectResponse{Ack: msg.Seq})
}

// finish records how the run ended in its sandbox, as a run.finished event and the run's status; a run the control
// plane already ended, such as a cancelled one, stays as it is.
func finish(ctx context.Context, tx db.DBTX, runID uuid.UUID, done *runnerpb.Done) error {
	current, err := lockStatus(ctx, tx, runID)
	if err != nil || current != StatusRunning {
		return err
	}
	state, ok := outcomes[done.Outcome]
	if !ok {
		state = StatusFailed
	}
	return endLocked(ctx, tx, runID, state, done.Reason, nil, []byte(done.OutputJson))
}

func validJSON(b []byte) []byte {
	if len(b) == 0 || !json.Valid(b) {
		return []byte("null")
	}
	return b
}

// fail logs what went wrong on the control plane's side and ends the stream; the runner reconnects and retries.
func (s *Server) fail(runID uuid.UUID, what string, err error) error {
	s.Log.Error("the runner protocol failed", "run", runID, "step", what, "error", err)
	return status.Error(codes.Unavailable, "the control plane could not "+what+"; reconnect")
}
