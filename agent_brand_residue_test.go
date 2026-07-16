package xai_proxy_doc_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenAgentBrandResidue ensures product docs/code do not embed a
// specific external agent project name the maintainers deliberately uncoupled
// from this tree. The forbidden token is assembled at runtime so this file
// itself does not contain it as a contiguous literal.
func TestNoForbiddenAgentBrandResidue(t *testing.T) {
	// Assemble: her + mes  (case-insensitive search uses lower form)
	token := string([]byte{'h', 'e', 'r', 'm', 'e', 's'})
	if token != "her"+"mes" {
		t.Fatal("token assembly mismatch")
	}
	lowerNeedle := []byte(token)

	root := moduleRoot(t)
	var hits []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		// Skip built binary at module root if present.
		if d.Name() == "xai-proxy" && filepath.Dir(path) == root {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		check := data
		if len(check) > 512 {
			check = check[:512]
		}
		if bytes.IndexByte(check, 0) >= 0 {
			return nil
		}
		lower := bytes.ToLower(data)
		if !bytes.Contains(lower, lowerNeedle) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(strings.ToLower(line), token) {
				hits = append(hits, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(hits) > 0 {
		t.Fatalf("forbidden agent brand residue (%d line(s)):\n%s", len(hits), strings.Join(hits, "\n"))
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
		return wd
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from %s", wd)
		}
		dir = parent
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
