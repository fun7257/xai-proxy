package auth

import (
	"fmt"
	"strings"
)

// Error codes for auth failures.
const (
	CodeDiscoveryFailed       = "xai_discovery_failed"
	CodeDiscoveryInvalid      = "xai_discovery_invalid"
	CodeDeviceCodeFailed      = "device_code_request_failed"
	CodeDeviceCodeInvalid     = "device_code_invalid"
	CodeDeviceTimeout         = "device_code_timeout"
	CodeDeviceTokenInvalid    = "xai_device_token_invalid"
	CodeDeviceTokenFailed     = "xai_device_token_failed"
	CodeRefreshFailed         = "xai_refresh_failed"
	CodeTierDenied            = "xai_oauth_tier_denied"
	CodeMissingRefresh        = "xai_auth_missing_refresh_token"
	CodeMissingAccess         = "xai_auth_missing_access_token"
	CodeAuthMissing           = "xai_auth_missing"
)

// Error is a typed auth failure.
type Error struct {
	Code             string
	Message          string
	ReloginRequired  bool
	HTTPStatus       int
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func NewError(code, msg string, relogin bool) *Error {
	return &Error{Code: code, Message: msg, ReloginRequired: relogin}
}

func wrapHTTP(code string, status int, body string, relogin bool) *Error {
	msg := fmt.Sprintf("%s (HTTP %d)", code, status)
	if body != "" {
		msg += ": " + trimBody(body, 500)
	}
	return &Error{Code: code, Message: msg, ReloginRequired: relogin, HTTPStatus: status}
}

func trimBody(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// IsTerminalRefresh reports whether retrying the same refresh token cannot succeed.
func IsTerminalRefresh(err error) bool {
	e, ok := err.(*Error)
	if !ok {
		return false
	}
	switch e.Code {
	case CodeRefreshFailed, CodeMissingRefresh:
		return e.ReloginRequired
	default:
		return false
	}
}

// IsTierDenied reports SuperGrok/API allowlist rejection.
func IsTierDenied(err error) bool {
	e, ok := err.(*Error)
	return ok && e.Code == CodeTierDenied
}
