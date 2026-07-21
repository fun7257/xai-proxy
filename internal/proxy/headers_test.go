package proxy

import (
	"net/http"
	"testing"
)

func TestCopyResponseHeaders_PassThroughEncodingAndLength(t *testing.T) {
	src := make(http.Header)
	src.Set("Content-Type", "application/json")
	src.Set("Content-Encoding", "gzip")
	src.Set("Content-Length", "42")
	src.Set("Transfer-Encoding", "chunked") // hop-by-hop — must drop
	src.Set("Connection", "keep-alive")    // hop-by-hop
	src.Set("X-Request-Id", "abc")

	dst := make(http.Header)
	copyResponseHeaders(dst, src)

	if got := dst.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding=%q want gzip", got)
	}
	if got := dst.Get("Content-Length"); got != "42" {
		t.Fatalf("Content-Length=%q want 42", got)
	}
	if got := dst.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type=%q", got)
	}
	if got := dst.Get("X-Request-Id"); got != "abc" {
		t.Fatalf("X-Request-Id=%q", got)
	}
	if dst.Get("Transfer-Encoding") != "" {
		t.Fatal("Transfer-Encoding must not be forwarded (hop-by-hop)")
	}
	if dst.Get("Connection") != "" {
		t.Fatal("Connection must not be forwarded (hop-by-hop)")
	}
}

func TestCopyRequestHeaders_DropsContentLength(t *testing.T) {
	src := make(http.Header)
	src.Set("Content-Type", "application/json")
	src.Set("Content-Length", "99")
	src.Set("Accept-Encoding", "gzip")
	src.Set("Authorization", "Bearer client")

	dst := make(http.Header)
	copyRequestHeaders(dst, src)

	if dst.Get("Accept-Encoding") != "gzip" {
		t.Fatal("Accept-Encoding should be forwarded for compression pass-through")
	}
	if dst.Get("Content-Type") != "application/json" {
		t.Fatal("Content-Type should be forwarded")
	}
	if dst.Get("Content-Length") != "" {
		t.Fatal("Content-Length on request is hop-by-hop; proxy sets req.ContentLength")
	}
	if dst.Get("Authorization") != "" {
		t.Fatal("Authorization must be stripped (replaced with OAuth)")
	}
}
