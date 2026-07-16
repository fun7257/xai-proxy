package example_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xai-proxy/internal/proxy"
)

// TestExampleScriptsCoverEveryAllowedPath walks example/** and asserts every
// ExactAllowedPaths entry (and the /videos/{id} pattern) appears as a URL
// path segment in at least one sample file. Drives the real allowlist export.
func TestExampleScriptsCoverEveryAllowedPath(t *testing.T) {
	root := findExampleRoot(t)
	corpus := readAllTextUnder(t, root)

	for path := range proxy.ExactAllowedPaths {
		// samples use ${BASE_URL}/chat/completions → look for /chat/completions or full /v1/...
		needleA := path
		needleB := "/v1" + path
		if !strings.Contains(corpus, needleA) && !strings.Contains(corpus, needleB) {
			t.Errorf("allowed path %q not found in any example under %s", path, root)
		}
	}

	// Video status pattern /videos/{id}
	if !strings.Contains(corpus, "/videos/${REQUEST_ID}") &&
		!strings.Contains(corpus, "/videos/$REQUEST_ID") &&
		!strings.Contains(corpus, "/videos/{id}") {
		// status_poll uses ${BASE_URL}/videos/${REQUEST_ID}
		if !strings.Contains(corpus, "/videos/") {
			t.Error("missing /videos/{id} poll example")
		}
	}
	// Prefer explicit poll script content
	statusPath := filepath.Join(root, "video", "status_poll.sh")
	b, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("status poll example missing: %v", err)
	}
	if !strings.Contains(string(b), "/videos/") {
		t.Error("status_poll.sh must call /videos/{id}")
	}

	// Policy: no supported /audio/* examples
	if strings.Contains(corpus, "/audio/speech") || strings.Contains(corpus, "/audio/transcriptions") {
		// Allow only negative mentions (NOT OpenAI...)
		// Fail if a curl targets those paths as the URL
		for _, line := range strings.Split(corpus, "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "#") {
				continue
			}
			if strings.Contains(line, "${BASE_URL}/audio/") || strings.Contains(line, "/v1/audio/") {
				t.Errorf("OpenAI audio path must not appear as a supported request: %s", trim)
			}
		}
	}
}

func findExampleRoot(t *testing.T) string {
	t.Helper()
	// test file lives in example/; root is this directory
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// When go test runs package example_test from example/, cwd is example/
	if _, err := os.Stat(filepath.Join(wd, "README.md")); err == nil {
		if _, err := os.Stat(filepath.Join(wd, "chat")); err == nil {
			return wd
		}
	}
	// fallback: relative to module
	cand := filepath.Join(wd, "example")
	if _, err := os.Stat(cand); err == nil {
		return cand
	}
	t.Fatalf("cannot locate example/ from cwd %s", wd)
	return ""
}

func readAllTextUnder(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if !strings.HasSuffix(name, ".sh") && !strings.HasSuffix(name, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
