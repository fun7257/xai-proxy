package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// JWTExp returns the exp claim unix seconds, or 0 if not parseable.
func JWTExp(accessToken string) int64 {
	if accessToken == "" || !strings.Contains(accessToken, ".") {
		return 0
	}
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some tokens use padded base64url.
		payload, err = base64.URLEncoding.DecodeString(padB64(parts[1]))
		if err != nil {
			return 0
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0
	}
	return claims.Exp
}

func padB64(s string) string {
	switch len(s) % 4 {
	case 2:
		return s + "=="
	case 3:
		return s + "="
	default:
		return s
	}
}

// AccessTokenIsExpiring reports whether the JWT exp is within skewSeconds.
// Opaque (non-JWT) tokens return false — force refresh only on 401.
func AccessTokenIsExpiring(accessToken string, skewSeconds int) bool {
	exp := JWTExp(accessToken)
	if exp == 0 {
		return false
	}
	if skewSeconds < 0 {
		skewSeconds = 0
	}
	return float64(exp) <= float64(time.Now().Unix())+float64(skewSeconds)
}

// ProactiveRefreshSkewSeconds mirrors Hermes adaptive skew:
// long-lived tokens use up to 1h; short remaining life caps at 120s.
func ProactiveRefreshSkewSeconds(accessToken string) int {
	maxSkew := MaxRefreshSkewSeconds
	exp := JWTExp(accessToken)
	if exp == 0 {
		return maxSkew
	}
	remaining := float64(exp) - float64(time.Now().Unix())
	if remaining <= 0 {
		return maxSkew
	}
	if remaining <= ShortJWTRemainingSeconds {
		if ShortJWTSkewSeconds < maxSkew {
			return ShortJWTSkewSeconds
		}
		return maxSkew
	}
	return maxSkew
}
