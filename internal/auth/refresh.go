package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Refresh exchanges a refresh_token for a new access (and possibly rotated refresh) token.
func Refresh(ctx context.Context, client *http.Client, tokenEndpoint, refreshToken string) (*TokenResponse, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, NewError(CodeMissingRefresh, "missing refresh_token; re-authenticate with `xai-proxy login`", true)
	}
	endpoint := strings.TrimSpace(tokenEndpoint)
	if endpoint == "" {
		d, err := Discover(ctx, client)
		if err != nil {
			return nil, err
		}
		endpoint = d.TokenEndpoint
	}
	if err := ValidateXAIURL(endpoint, "token_endpoint"); err != nil {
		return nil, NewError(CodeDiscoveryInvalid, err.Error(), true)
	}
	if client == nil {
		client = &http.Client{Timeout: time.Duration(DefaultRefreshTimeoutSeconds) * time.Second}
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", ClientID)
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, NewError(CodeRefreshFailed, err.Error(), false)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, NewError(CodeRefreshFailed, fmt.Sprintf("token refresh failed: %v", err), false)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusForbidden {
		msg := "xAI token refresh failed with HTTP 403. This OAuth account may not be authorized for API access (tier/entitlement). Re-login will not fix it; use an API key path or upgrade at https://x.ai/grok."
		if b := strings.TrimSpace(string(body)); b != "" {
			msg += " Response: " + trimBody(b, 300)
		}
		return nil, &Error{Code: CodeTierDenied, Message: msg, ReloginRequired: false, HTTPStatus: 403}
	}
	if resp.StatusCode != http.StatusOK {
		relogin := resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized
		return nil, wrapHTTP(CodeRefreshFailed, resp.StatusCode, string(body), relogin)
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, NewError(CodeRefreshFailed, fmt.Sprintf("invalid refresh JSON: %v", err), false)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return nil, NewError(CodeMissingAccess, "refresh response missing access_token", true)
	}
	// Rotate: keep old refresh if server did not return a new one.
	if strings.TrimSpace(tr.RefreshToken) == "" {
		tr.RefreshToken = refreshToken
	}
	if tr.TokenType == "" {
		tr.TokenType = "Bearer"
	}
	return &tr, nil
}
