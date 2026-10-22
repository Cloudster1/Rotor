package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// session is somebody signed in to one product, by its session cookie. Adapted from Exciter's cmd/suite-seed.
type session struct {
	base   string
	client *http.Client
}

func newSession(base string) (*session, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &session{base: base, client: &http.Client{Jar: jar, Timeout: config.SuiteSeedCallTimeout}}, nil
}

func signIn(ctx context.Context, base, email, password string) (*session, error) {
	s, err := newSession(base)
	if err != nil {
		return nil, err
	}
	return s, s.send(ctx, http.MethodPost, "/auth/login", map[string]any{"email": email, "password": password}, nil)
}

// refusal is an answer of 400 or more, with the product's own sentence.
type refusal struct {
	status int
	body   string
}

func (r *refusal) Error() string { return fmt.Sprintf("answered %d: %s", r.status, r.body) }

func isConflict(err error) bool {
	var r *refusal
	return errors.As(err, &r) && r.status == http.StatusConflict
}

func (s *session) send(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return &refusal{status: resp.StatusCode, body: strings.TrimSpace(string(raw))}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}
