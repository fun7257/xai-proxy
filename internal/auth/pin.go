package auth

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateXAIURL ensures scheme is https and host is x.ai or a *.x.ai subdomain.
// Prevents credential leak via poisoned discovery cache or env overrides.
func ValidateXAIURL(raw, field string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%s is empty", field)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", field, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%s must use https (got %q)", field, u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("%s is missing a hostname", field)
	}
	if host != "x.ai" && !strings.HasSuffix(host, ".x.ai") {
		return fmt.Errorf("%s host %q is not on the x.ai origin", field, host)
	}
	return nil
}

// ValidateInferenceBaseURL returns a safe base URL or falls back on rejection.
func ValidateInferenceBaseURL(candidate, fallback string) string {
	candidate = strings.TrimSpace(strings.TrimRight(candidate, "/"))
	if candidate == "" {
		return strings.TrimRight(fallback, "/")
	}
	if err := ValidateXAIURL(candidate, "base_url"); err != nil {
		return strings.TrimRight(fallback, "/")
	}
	return candidate
}
