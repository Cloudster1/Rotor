package toolstub

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Persistence keeps what Rotor registered and was granted at the tools across a restart of the dev stack's stub, so
// a rebuild does not break every connection (#268). Only OAuth state is kept: clients, grants and their tokens.
// Pending codes and consent requests live for a minute or ten, and what callers wrote is the tests' to look at.

const stateFileMode = 0o600

type stateFile struct {
	Tenants []tenantState `json:"tenants"`
}

type tenantState struct {
	Name    string        `json:"name"`
	Options Options       `json:"options"`
	Clients []clientState `json:"clients"`
	Grants  []grantState  `json:"grants"`
	Tokens  []tokenState  `json:"tokens"`
}

type clientState struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	AuthMethod   string   `json:"authMethod"`
	SecretHash   []byte   `json:"secretHash"`
	RedirectURIs []string `json:"redirectUris"`
	GrantTypes   []string `json:"grantTypes"`
	Pending      bool     `json:"pending"`
}

type grantState struct {
	ID       string   `json:"id"`
	ClientID string   `json:"clientId"`
	Username string   `json:"username"`
	ReadOnly bool     `json:"readOnly"`
	Evidence bool     `json:"evidence"`
	Scopes   []string `json:"scopes"`
}

// tokenState is keyed by the token's digest, as in memory, so no token value is written.
type tokenState struct {
	Digest    []byte    `json:"digest"`
	GrantID   string    `json:"grantId"`
	Refresh   bool      `json:"refresh"`
	ReadOnly  bool      `json:"readOnly"`
	Expires   time.Time `json:"expires"`
	Replaced  bool      `json:"replaced"`
	Exchanged bool      `json:"exchanged"`
	Scopes    []string  `json:"scopes"`
}

// snapshot encodes the state; callers hold the lock. Tenants without a client are made again on first use.
func (s *Stub) snapshot() ([]byte, error) {
	out := stateFile{Tenants: []tenantState{}}
	for _, t := range s.tenants {
		if len(t.clients) == 0 && len(t.grants) == 0 {
			continue
		}
		ts := tenantState{Name: t.name, Options: t.opts, Clients: []clientState{}, Grants: []grantState{}, Tokens: []tokenState{}}
		for _, c := range t.clients {
			ts.Clients = append(ts.Clients, clientState{c.id, c.name, c.authMethod, c.secretHash, c.redirectURIs, c.grantTypes, c.pending})
		}
		for _, g := range t.grants {
			ts.Grants = append(ts.Grants, grantState{g.id, g.clientID, g.username, g.readOnly, g.evidence, g.scopes})
		}
		for key, tk := range t.tokens {
			ts.Tokens = append(ts.Tokens, tokenState{[]byte(key), tk.grant.id, tk.refresh, tk.readOnly, tk.expires, tk.replaced, tk.exchanged, tk.scopes})
		}
		out.Tenants = append(out.Tenants, ts)
	}
	return json.Marshal(out)
}

// restore replaces the tenants with those in raw, dropping tokens that expired while the stub was down.
func (s *Stub) restore(raw []byte) error {
	var in stateFile
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	tenants := map[string]*tenant{}
	for _, ts := range in.Tenants {
		if !tenantName.MatchString(ts.Name) || len(tenants) >= MaxTenants {
			continue
		}
		t := newTenant(ts.Name, ts.Options)
		for _, c := range ts.Clients {
			t.clients[c.ID] = &client{id: c.ID, name: c.Name, authMethod: c.AuthMethod, secretHash: c.SecretHash,
				redirectURIs: c.RedirectURIs, grantTypes: c.GrantTypes, pending: c.Pending}
		}
		for _, g := range ts.Grants {
			t.grants[g.ID] = &grant{id: g.ID, clientID: g.ClientID, username: g.Username, readOnly: g.ReadOnly, evidence: g.Evidence, scopes: g.Scopes}
		}
		for _, tk := range ts.Tokens {
			g, ok := t.grants[tk.GrantID]
			if !ok || !s.now().Before(tk.Expires) {
				continue
			}
			t.tokens[string(tk.Digest)] = &token{grant: g, refresh: tk.Refresh, readOnly: tk.ReadOnly, expires: tk.Expires,
				replaced: tk.Replaced, exchanged: tk.Exchanged, scopes: tk.Scopes}
		}
		tenants[ts.Name] = t
	}
	s.tenants = tenants
	return nil
}

// KeepStateIn loads the stub's OAuth state from path and keeps path current from then on. A missing or unreadable
// file starts the stub empty, with a log line, since a stub that will not start is worse than one that forgot.
func (s *Stub) KeepStateIn(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statePath = path
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		slog.Info("tool-stub has no saved state, starting empty", "path", path)
		return
	case err != nil:
		slog.Warn("tool-stub could not read its saved state, starting empty", "path", path, "error", err)
		return
	}
	if err := s.restore(raw); err != nil {
		slog.Warn("tool-stub's saved state is corrupt, starting empty", "path", path, "error", err)
		s.tenants = map[string]*tenant{}
		return
	}
	s.lastSaved = nil
	slog.Info("tool-stub restored its saved state", "path", path, "tenants", len(s.tenants))
}

// save writes the state if it changed, through a temporary file renamed over the old one so a crash leaves a whole file.
func (s *Stub) save() error {
	raw, err := s.snapshot()
	if err != nil || string(raw) == string(s.lastSaved) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.statePath), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(stateFileMode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.statePath); err != nil {
		return err
	}
	s.lastSaved = raw
	return nil
}

// Persisted saves the state after every request that may have changed it; without KeepStateIn it is h unchanged.
func (s *Stub) Persisted(h http.Handler) http.Handler {
	if s.statePath == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.save(); err != nil {
			slog.Error("tool-stub could not save its state", "path", s.statePath, "error", err)
		}
	})
}

// StateFileEnv names the file that keeps the state; the stack sets it, tests that build a stub in process do not.
const StateFileEnv = "TOOLSTUB_STATE_FILE"
