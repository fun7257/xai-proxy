package auth

// OAuth constants aligned with Hermes hermes_cli/auth.py (xAI Grok OAuth).
const (
	Issuer          = "https://auth.x.ai"
	DiscoveryURL    = Issuer + "/.well-known/openid-configuration"
	DeviceCodeURL   = Issuer + "/oauth2/device/code"
	ClientID        = "b1a00492-073a-47ea-816f-4c329264a828"
	Scope           = "openid profile email offline_access grok-cli:access api:access"
	DefaultAPIBase  = "https://api.x.ai/v1"
	DefaultTokenURL = Issuer + "/oauth2/token"

	// Max proactive refresh skew for long-lived SuperGrok JWTs.
	MaxRefreshSkewSeconds = 3600
	// Short-JWT skew cap when remaining lifetime <= 45 minutes.
	ShortJWTSkewSeconds = 120
	// Remaining lifetime threshold that switches to short skew.
	ShortJWTRemainingSeconds = 45 * 60

	DefaultRefreshTimeoutSeconds = 20
)
