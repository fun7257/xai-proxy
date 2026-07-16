package proxy

import (
	"strings"
)

// MaxBodyBytes is the default request body cap for media (STT, images, data-URIs).
// Chat-only payloads are much smaller; media uploads need headroom.
const MaxBodyBytes int64 = 100 * 1024 * 1024 // 100 MiB

// ExactAllowedPaths are fixed xAI-native paths under /v1 that the proxy forwards.
// All of these are xAI API routes. Some chat/text routes *also* match OpenAI
// path+body (see OpenAIFullCompatPaths); that is additive compatibility only.
// Non-identical OpenAI routes (e.g. /audio/speech) are intentionally absent.
var ExactAllowedPaths = map[string]struct{}{
	// Chat / text (xAI native; also OpenAI full-compat)
	"/responses":        {},
	"/chat/completions": {},
	"/completions":      {},
	"/embeddings":       {},
	"/models":           {},

	// Image (xAI Imagine)
	"/images/generations": {},
	"/images/edits":       {},

	// Voice (xAI native — NOT OpenAI /audio/speech or /audio/transcriptions)
	"/tts": {},
	"/stt": {},

	// Video (xAI native async family)
	"/videos/generations": {},
	"/videos/edits":       {},
	"/videos/extensions":  {},
}

// OpenAIFullCompatPaths is a subset of ExactAllowedPaths that match OpenAI
// path + body shape well enough for stock OpenAI SDKs. Chat is still xAI-native;
// this only marks extra client compatibility — never a separate "mode".
var OpenAIFullCompatPaths = map[string]struct{}{
	"/responses":        {},
	"/chat/completions": {},
	"/completions":      {},
	"/embeddings":       {},
	"/models":           {},
}

// Explicitly rejected OpenAI-only audio routes (not mapped to xAI /tts or /stt).
var OpenAIAudioShimRejected = map[string]struct{}{
	"/audio/speech":          {},
	"/audio/transcriptions":  {},
	"/audio/translations":    {},
}

// normalizePath canonicalizes a path relative to /v1 (leading slash, no trailing slash).
func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimRight(p, "/")
	}
	return p
}

// PathAllowed reports whether a path relative to /v1 may be forwarded to api.x.ai.
//
// Policy:
//   - Exact allow for known chat/image/tts/stt/video endpoints
//   - Prefix allow for GET/HEAD video status: /videos/{id} (not reserved action names)
//   - Never invent OpenAI aliases (/audio/*) for non-identical xAI APIs
//
// relPath should be the path after /v1 (e.g. "/chat/completions", "/tts").
func PathAllowed(relPath string) bool {
	p := normalizePath(relPath)
	if p == "/" {
		return false
	}
	if _, reject := OpenAIAudioShimRejected[p]; reject {
		return false
	}
	if _, ok := ExactAllowedPaths[p]; ok {
		return true
	}
	// Video status / job poll: /videos/<request_id>
	if strings.HasPrefix(p, "/videos/") {
		rest := strings.TrimPrefix(p, "/videos/")
		if rest == "" || strings.Contains(rest, "/") {
			return false
		}
		// reserved action segments already in ExactAllowedPaths; any other single segment is an id
		switch rest {
		case "generations", "edits", "extensions":
			return true // also exact-matched; keep consistent
		default:
			return true
		}
	}
	// Image future-proof: only exact generations/edits today
	return false
}

// IsOpenAIFullCompat reports whether path is intended for unmodified OpenAI clients.
func IsOpenAIFullCompat(relPath string) bool {
	_, ok := OpenAIFullCompatPaths[normalizePath(relPath)]
	return ok
}

// IsOpenAIAudioShimRejected reports OpenAI audio routes we refuse to fake-map.
func IsOpenAIAudioShimRejected(relPath string) bool {
	_, ok := OpenAIAudioShimRejected[normalizePath(relPath)]
	return ok
}

// AllowedPathSummary is a short human-readable list for error messages.
func AllowedPathSummary() string {
	return "chat(/chat/completions,/responses,/models,/embeddings,/completions), " +
		"images(/images/generations,/images/edits), " +
		"voice(/tts,/stt — not OpenAI /audio/*), " +
		"video(/videos/generations|/edits|/extensions|/videos/{id})"
}

// pathAllowed keeps backward-compatible name used by older call sites/tests.
// When allow map is non-nil, it is treated as an override exact set (tests).
func pathAllowed(p string, allow map[string]struct{}) bool {
	if allow != nil {
		_, ok := allow[normalizePath(p)]
		return ok
	}
	return PathAllowed(p)
}

// DefaultAllowedPaths is retained for callers that want the exact-set snapshot.
// Prefer PathAllowed for full policy including /videos/{id}.
var DefaultAllowedPaths = ExactAllowedPaths
