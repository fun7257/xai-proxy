package auth

import "testing"

func TestValidateXAIURL(t *testing.T) {
	ok := []string{
		"https://auth.x.ai/oauth2/token",
		"https://api.x.ai/v1",
		"https://accounts.x.ai/oauth2/device",
	}
	for _, u := range ok {
		if err := ValidateXAIURL(u, "test"); err != nil {
			t.Errorf("expected ok for %s: %v", u, err)
		}
	}
	bad := []string{
		"http://api.x.ai/v1",
		"https://evil.example/token",
		"https://x.ai.evil.com/token",
		"",
	}
	for _, u := range bad {
		if err := ValidateXAIURL(u, "test"); err == nil {
			t.Errorf("expected error for %s", u)
		}
	}
}

func TestValidateInferenceBaseURLFallback(t *testing.T) {
	got := ValidateInferenceBaseURL("https://attacker.example/v1", DefaultAPIBase)
	if got != DefaultAPIBase {
		t.Fatalf("got %q want %q", got, DefaultAPIBase)
	}
	got = ValidateInferenceBaseURL("https://api.x.ai/v1/", DefaultAPIBase)
	if got != "https://api.x.ai/v1" {
		t.Fatalf("got %q", got)
	}
}
