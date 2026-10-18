package runnerctl

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"github.com/cloudster1/rotor/backend/internal/config"
	"github.com/cloudster1/rotor/backend/internal/runnerpb"
	"github.com/cloudster1/rotor/backend/internal/vault"
)

// Issuer issues the server certificate; *vault.Client is the real one.
type Issuer interface {
	IssueCertificate(ctx context.Context, host string, ttl time.Duration) (vault.Certificate, error)
}

// certificates keeps the server certificate current, renewing it before it ends.
type certificates struct {
	issuer Issuer
	host   string
	log    *slog.Logger
	mu     sync.RWMutex
	cert   *tls.Certificate
	ca     string
}

func (c *certificates) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cert == nil {
		return nil, errors.New("the runner protocol has no certificate yet")
	}
	return c.cert, nil
}

func (c *certificates) renew(ctx context.Context) time.Time {
	issued, err := c.issuer.IssueCertificate(ctx, c.host, config.RunnerCertTTL)
	if err == nil {
		var pair tls.Certificate
		if pair, err = tls.X509KeyPair([]byte(issued.Certificate), []byte(issued.PrivateKey)); err == nil {
			c.mu.Lock()
			c.cert, c.ca = &pair, issued.IssuingCA
			c.mu.Unlock()
			c.log.Info("issued the runner protocol's certificate", "until", issued.NotAfter)
			return issued.NotAfter.Add(-config.RunnerCertRenewBefore)
		}
	}
	c.log.Warn("cannot issue the runner protocol's certificate, trying again", "error", err)
	return time.Now().Add(config.VaultLoginRetry)
}

// keep issues the certificate and renews it until ctx ends.
func (c *certificates) keep(ctx context.Context) {
	for ctx.Err() == nil {
		next := c.renew(ctx)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Serve listens on addr with TLS from OpenBao's PKI until ctx ends; runners dial it from their sandboxes.
func (s *Server) Serve(ctx context.Context, addr string, issuer Issuer) error {
	host := s.Host
	if host == "" {
		host = config.RunnerHost
	}
	certs := &certificates{issuer: issuer, log: s.Log, host: host}
	go certs.keep(ctx)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	server := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(&tls.Config{GetCertificate: certs.get, MinVersion: tls.VersionTLS13})),
		grpc.MaxRecvMsgSize(config.MaxRunnerMessageBytes),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: config.RunnerKeepaliveMin, PermitWithoutStream: true}),
	)
	runnerpb.RegisterRunnerControlServiceServer(server, s)
	go func() {
		<-ctx.Done()
		server.GracefulStop()
	}()
	s.Log.Info("runner protocol listening", "addr", addr)
	return server.Serve(listener)
}
