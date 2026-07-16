package credential

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"xai-proxy/internal/auth"
	"xai-proxy/internal/outbound"
	"xai-proxy/internal/store"
)

// Status is a non-sensitive snapshot for CLI /ready.
type Status struct {
	State     string `json:"state"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Message   string `json:"message,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
}

// Manager resolves live OAuth bearers with refresh + disk lock.
type Manager struct {
	mu     sync.Mutex
	mem    *store.Tokens
	client *http.Client
	sf     singleflight
}

// NewManager creates a credential manager.
// When client is nil, uses the process outbound proxy policy (CLI --proxy / env).
func NewManager(client *http.Client) *Manager {
	if client == nil {
		c, err := outbound.NewClient(outbound.Options{
			Timeout: time.Duration(auth.DefaultRefreshTimeoutSeconds) * time.Second,
		})
		if err != nil {
			c = &http.Client{Timeout: time.Duration(auth.DefaultRefreshTimeoutSeconds) * time.Second}
		}
		client = c
	}
	return &Manager{client: client}
}

// IsAuthenticated is a cheap local check (no network).
func (m *Manager) IsAuthenticated() bool {
	t, err := store.Load()
	if err != nil {
		return false
	}
	return store.HasUsableTokens(t)
}

// Status returns a non-sensitive credential state.
func (m *Manager) Status() Status {
	t, err := store.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return Status{State: store.StatusUnauthenticated, Message: "not logged in; run `xai-proxy login`"}
		}
		return Status{State: store.StatusUnauthenticated, Message: err.Error()}
	}
	if t.Status == store.StatusReauthRequired {
		msg := "re-authentication required"
		if t.LastError != nil {
			msg = *t.LastError
		}
		return Status{State: store.StatusReauthRequired, UpdatedAt: t.UpdatedAt, Message: msg}
	}
	if t.Status == store.StatusTierDenied {
		msg := "tier denied"
		if t.LastError != nil {
			msg = *t.LastError
		}
		return Status{State: store.StatusTierDenied, UpdatedAt: t.UpdatedAt, Message: msg, BaseURL: t.BaseURL}
	}
	if !store.HasUsableTokens(t) {
		return Status{State: store.StatusUnauthenticated, UpdatedAt: t.UpdatedAt, Message: "tokens incomplete"}
	}
	base := t.BaseURL
	if base == "" {
		base = auth.DefaultAPIBase
	}
	return Status{State: store.StatusReady, UpdatedAt: t.UpdatedAt, BaseURL: base}
}

// GetBearer returns a usable access token, refreshing if near expiry.
func (m *Manager) GetBearer(ctx context.Context) (bearer, baseURL string, err error) {
	return m.resolve(ctx, false)
}

// ForceRefresh always refreshes (e.g. after upstream 401).
func (m *Manager) ForceRefresh(ctx context.Context) (bearer, baseURL string, err error) {
	return m.resolve(ctx, true)
}

func (m *Manager) resolve(ctx context.Context, force bool) (string, string, error) {
	// Fast path: in-memory token still good.
	m.mu.Lock()
	if !force && m.mem != nil && store.HasUsableTokens(m.mem) {
		skew := auth.ProactiveRefreshSkewSeconds(m.mem.AccessToken)
		if !auth.AccessTokenIsExpiring(m.mem.AccessToken, skew) {
			b, u := m.mem.AccessToken, baseOf(m.mem)
			m.mu.Unlock()
			return b, u, nil
		}
	}
	m.mu.Unlock()

	v, err, _ := m.sf.Do("bearer", func() (any, error) {
		return m.refreshLocked(ctx, force)
	})
	if err != nil {
		return "", "", err
	}
	pair := v.([2]string)
	return pair[0], pair[1], nil
}

func (m *Manager) refreshLocked(ctx context.Context, force bool) ([2]string, error) {
	lock, err := store.AcquireLock()
	if err != nil {
		return [2]string{}, fmt.Errorf("acquire lock: %w", err)
	}
	defer lock.Unlock()

	t, err := store.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return [2]string{}, auth.NewError(auth.CodeAuthMissing, "no credentials; run `xai-proxy login`", true)
		}
		return [2]string{}, err
	}
	if t.Status == store.StatusReauthRequired {
		msg := "re-authentication required; run `xai-proxy login`"
		if t.LastError != nil {
			msg = *t.LastError
		}
		return [2]string{}, auth.NewError(auth.CodeAuthMissing, msg, true)
	}
	if !store.HasUsableTokens(t) {
		return [2]string{}, auth.NewError(auth.CodeAuthMissing, "incomplete tokens; run `xai-proxy login`", true)
	}

	need := force
	if !need {
		skew := auth.ProactiveRefreshSkewSeconds(t.AccessToken)
		need = auth.AccessTokenIsExpiring(t.AccessToken, skew)
	}

	if need {
		tr, rerr := auth.Refresh(ctx, m.client, t.TokenEndpoint, t.RefreshToken)
		if rerr != nil {
			if auth.IsTierDenied(rerr) {
				msg := rerr.Error()
				t.Status = store.StatusTierDenied
				t.LastError = &msg
				t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				_ = store.Save(t)
				return [2]string{}, rerr
			}
			if auth.IsTerminalRefresh(rerr) {
				msg := rerr.Error()
				t.AccessToken = ""
				t.RefreshToken = ""
				t.Status = store.StatusReauthRequired
				t.LastError = &msg
				t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				_ = store.Save(t)
				m.mu.Lock()
				m.mem = nil
				m.mu.Unlock()
				return [2]string{}, rerr
			}
			return [2]string{}, rerr
		}
		t.AccessToken = tr.AccessToken
		t.RefreshToken = tr.RefreshToken
		if tr.ExpiresIn > 0 {
			t.ExpiresIn = tr.ExpiresIn
		}
		if tr.TokenType != "" {
			t.TokenType = tr.TokenType
		}
		t.Status = store.StatusReady
		t.LastError = nil
		t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := store.Save(t); err != nil {
			return [2]string{}, err
		}
	}

	m.mu.Lock()
	m.mem = t
	m.mu.Unlock()
	return [2]string{t.AccessToken, baseOf(t)}, nil
}

func baseOf(t *store.Tokens) string {
	if t != nil && t.BaseURL != "" {
		return auth.ValidateInferenceBaseURL(t.BaseURL, auth.DefaultAPIBase)
	}
	if v := os.Getenv("XAI_BASE_URL"); v != "" {
		return auth.ValidateInferenceBaseURL(v, auth.DefaultAPIBase)
	}
	return auth.DefaultAPIBase
}

// SaveLogin persists a successful device-code login.
func SaveLogin(result *auth.LoginResult) error {
	if result == nil {
		return errors.New("nil login result")
	}
	t := &store.Tokens{
		Version:               1,
		AccessToken:           result.Tokens.AccessToken,
		RefreshToken:          result.Tokens.RefreshToken,
		TokenType:             result.Tokens.TokenType,
		ExpiresIn:             result.Tokens.ExpiresIn,
		TokenEndpoint:         result.Discovery.TokenEndpoint,
		AuthorizationEndpoint: result.Discovery.AuthorizationEndpoint,
		BaseURL:               result.BaseURL,
		UpdatedAt:             time.Now().UTC().Format(time.RFC3339),
		Status:                store.StatusReady,
		LastError:             nil,
	}
	lock, err := store.AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Unlock()
	return store.Save(t)
}

// Logout clears stored credentials.
func Logout() error {
	lock, err := store.AcquireLock()
	if err != nil {
		// still try delete
		return store.Delete()
	}
	defer lock.Unlock()
	return store.Delete()
}

// --- minimal singleflight (stdlib only) ---

type singleflight struct {
	mu sync.Mutex
	m  map[string]*call
}

type call struct {
	wg  sync.WaitGroup
	val any
	err error
}

func (g *singleflight) Do(key string, fn func() (any, error)) (any, error, bool) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err, true
	}
	c := &call{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	// Always finish the flight: a panic inside fn() must not leave waiters blocked forever
	// (that would freeze every subsequent GetBearer until process restart).
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				c.err = fmt.Errorf("credential refresh panic: %v", rec)
			}
			c.wg.Done()
			g.mu.Lock()
			delete(g.m, key)
			g.mu.Unlock()
		}()
		c.val, c.err = fn()
	}()
	return c.val, c.err, false
}
