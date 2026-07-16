package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Discovery holds OIDC endpoints from auth.x.ai.
type Discovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// Discover fetches and validates the OIDC configuration.
func Discover(ctx context.Context, client *http.Client) (*Discovery, error) {
	if client == nil {
		client = outboundClient(15 * time.Second)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DiscoveryURL, nil)
	if err != nil {
		return nil, NewError(CodeDiscoveryFailed, err.Error(), false)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, NewError(CodeDiscoveryFailed, fmt.Sprintf("OIDC discovery failed: %v", err), false)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, wrapHTTP(CodeDiscoveryFailed, resp.StatusCode, string(body), false)
	}

	var d Discovery
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, NewError(CodeDiscoveryInvalid, fmt.Sprintf("invalid discovery JSON: %v", err), false)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" {
		return nil, NewError(CodeDiscoveryInvalid, "discovery missing authorization_endpoint or token_endpoint", false)
	}
	if err := ValidateXAIURL(d.AuthorizationEndpoint, "authorization_endpoint"); err != nil {
		return nil, NewError(CodeDiscoveryInvalid, err.Error(), false)
	}
	if err := ValidateXAIURL(d.TokenEndpoint, "token_endpoint"); err != nil {
		return nil, NewError(CodeDiscoveryInvalid, err.Error(), false)
	}
	return &d, nil
}
