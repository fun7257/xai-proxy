package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DeviceCodeResponse is the device authorization response.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// TokenResponse is a successful token endpoint payload.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// RequestDeviceCode starts the device-code flow.
func RequestDeviceCode(ctx context.Context, client *http.Client) (*DeviceCodeResponse, error) {
	if client == nil {
		client = outboundClient(30 * time.Second)
	}
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("scope", Scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, DeviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, NewError(CodeDeviceCodeFailed, err.Error(), false)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, NewError(CodeDeviceCodeFailed, fmt.Sprintf("device-code request failed: %v", err), false)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, wrapHTTP(CodeDeviceCodeFailed, resp.StatusCode, string(body), false)
	}

	var dc DeviceCodeResponse
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, NewError(CodeDeviceCodeInvalid, fmt.Sprintf("invalid device-code JSON: %v", err), false)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" ||
		dc.ExpiresIn <= 0 || dc.Interval <= 0 {
		return nil, NewError(CodeDeviceCodeInvalid, "device-code response missing required fields", false)
	}
	return &dc, nil
}

// PollDeviceToken polls until the user approves or the code expires.
func PollDeviceToken(ctx context.Context, client *http.Client, tokenEndpoint, deviceCode string, expiresIn, interval int) (*TokenResponse, error) {
	if client == nil {
		client = outboundClient(30 * time.Second)
	}
	if err := ValidateXAIURL(tokenEndpoint, "token_endpoint"); err != nil {
		return nil, NewError(CodeDiscoveryInvalid, err.Error(), true)
	}
	if interval < 1 {
		interval = 1
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	currentInterval := interval

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("client_id", ClientID)
		form.Set("device_code", deviceCode)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, NewError(CodeDeviceTokenFailed, err.Error(), false)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, NewError(CodeDeviceTokenFailed, fmt.Sprintf("token poll failed: %v", err), false)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var tr TokenResponse
			if err := json.Unmarshal(body, &tr); err != nil {
				return nil, NewError(CodeDeviceTokenInvalid, fmt.Sprintf("invalid token JSON: %v", err), false)
			}
			if strings.TrimSpace(tr.AccessToken) == "" {
				return nil, NewError(CodeDeviceTokenInvalid, "token response missing access_token", true)
			}
			if strings.TrimSpace(tr.RefreshToken) == "" {
				return nil, NewError(CodeDeviceTokenInvalid, "token response missing refresh_token", true)
			}
			if tr.TokenType == "" {
				tr.TokenType = "Bearer"
			}
			return &tr, nil
		}

		var errPayload struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &errPayload)
		switch errPayload.Error {
		case "authorization_pending":
			if err := sleepCtx(ctx, time.Duration(currentInterval)*time.Second); err != nil {
				return nil, err
			}
			continue
		case "slow_down":
			currentInterval++
			if currentInterval > 30 {
				currentInterval = 30
			}
			if err := sleepCtx(ctx, time.Duration(currentInterval)*time.Second); err != nil {
				return nil, err
			}
			continue
		default:
			desc := errPayload.ErrorDescription
			if desc == "" {
				desc = errPayload.Error
			}
			if desc == "" {
				desc = string(body)
			}
			return nil, NewError(CodeDeviceTokenFailed, "device-code token polling failed: "+desc, true)
		}
	}
	return nil, NewError(CodeDeviceTimeout, "timed out waiting for device authorization", true)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// LoginResult is the outcome of a full device-code login.
type LoginResult struct {
	Tokens    TokenResponse
	Discovery Discovery
	BaseURL   string
}

// DeviceLogin runs discovery → device code → poll and returns tokens.
// openBrowser attempts to open the verification URL when true.
func DeviceLogin(ctx context.Context, client *http.Client, openBrowser bool, printFn func(string)) (*LoginResult, error) {
	if printFn == nil {
		printFn = func(s string) { fmt.Fprintln(os.Stderr, s) }
	}
	disc, err := Discover(ctx, client)
	if err != nil {
		return nil, err
	}
	dc, err := RequestDeviceCode(ctx, client)
	if err != nil {
		return nil, err
	}

	verify := dc.VerificationURIComplete
	if verify == "" {
		verify = dc.VerificationURI
	}
	printFn("")
	printFn("To continue:")
	printFn("  1. Open: " + verify)
	printFn("  2. If prompted, enter code: " + dc.UserCode)
	if openBrowser {
		if opened := tryOpenBrowser(verify); opened {
			printFn("  (Opened browser for verification)")
		} else {
			printFn("  Could not open browser automatically — use the URL above.")
		}
	}
	printFn(fmt.Sprintf("Waiting for approval (polling every %ds)...", max(1, dc.Interval)))

	tr, err := PollDeviceToken(ctx, client, disc.TokenEndpoint, dc.DeviceCode, dc.ExpiresIn, dc.Interval)
	if err != nil {
		return nil, err
	}

	base := ValidateInferenceBaseURL(os.Getenv("XAI_BASE_URL"), DefaultAPIBase)
	if v := os.Getenv("HERMES_XAI_BASE_URL"); v != "" {
		base = ValidateInferenceBaseURL(v, DefaultAPIBase)
	}
	return &LoginResult{
		Tokens:    *tr,
		Discovery: *disc,
		BaseURL:   base,
	}, nil
}

func tryOpenBrowser(u string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		return false
	}
	return cmd.Start() == nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
