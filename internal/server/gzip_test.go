package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	})
}

func TestGzipCompressesWhenClientSupportsIt(t *testing.T) {
	const body = "hello gzip world, this should be compressed transparently"
	srv := Gzip(okHandler(body))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Error("Content-Length must be cleared when compressing")
	}

	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("response is not valid gzip: %v", err)
	}
	got, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if string(got) != body {
		t.Errorf("decompressed = %q, want %q", got, body)
	}
}

func TestGzipPassesThroughWithoutAcceptEncoding(t *testing.T) {
	const body = "plain body"
	rec := httptest.NewRecorder()
	Gzip(okHandler(body)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty (no gzip requested)", got)
	}
	if rec.Body.String() != body {
		t.Errorf("body = %q, want %q", rec.Body.String(), body)
	}
}

func TestGzipSkipsExemptPaths(t *testing.T) {
	for path := range gzipSkipPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		Gzip(okHandler("metrics-like body")).ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("path %s: Content-Encoding = %q, want empty (should skip)", path, got)
		}
	}
}
