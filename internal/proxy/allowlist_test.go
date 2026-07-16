package proxy

import "testing"

func TestPathAllowed_ChatAndMultimodal(t *testing.T) {
	allow := []string{
		"/chat/completions",
		"/responses",
		"/models",
		"/embeddings",
		"/completions",
		"/images/generations",
		"/images/edits",
		"/tts",
		"/stt",
		"/videos/generations",
		"/videos/edits",
		"/videos/extensions",
		"/videos/req_abc123",
	}
	for _, p := range allow {
		if !PathAllowed(p) {
			t.Errorf("expected allowed: %s", p)
		}
	}
}

func TestPathAllowed_RejectOpenAIAudioShims(t *testing.T) {
	reject := []string{
		"/audio/speech",
		"/audio/transcriptions",
		"/audio/translations",
		"/secret",
		"/",
		"/videos/",
		"/videos/foo/bar",
	}
	for _, p := range reject {
		if PathAllowed(p) {
			t.Errorf("expected denied: %s", p)
		}
	}
	if !IsOpenAIAudioShimRejected("/audio/speech") {
		t.Fatal("expected speech marked as OpenAI audio shim reject")
	}
	if IsOpenAIFullCompat("/tts") {
		t.Fatal("tts is native-only, not full OpenAI compat")
	}
	if !IsOpenAIFullCompat("/chat/completions") {
		t.Fatal("chat should be full compat")
	}
}

func TestPathAllowed_Normalize(t *testing.T) {
	if !PathAllowed("tts") {
		t.Fatal("leading slash optional")
	}
	if !PathAllowed("/tts/") {
		t.Fatal("trailing slash stripped")
	}
}
