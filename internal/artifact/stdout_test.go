package artifact

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"strings"
	"testing"
)

func TestIsTextContent(t *testing.T) {
	text := []string{"text/plain", "text/html; charset=utf-8", "application/json", "application/xml"}
	binary := []string{"image/png", "application/octet-stream", "video/webm", "", "json"}

	for _, ct := range text {
		if !isTextContent(ct) {
			t.Errorf("isTextContent(%q) = false, want true", ct)
		}
	}
	for _, ct := range binary {
		if isTextContent(ct) {
			t.Errorf("isTextContent(%q) = true, want false", ct)
		}
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns what was
// written. ponytail: os.Pipe is the stdlib way; no capture library needed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestStdoutStoreUploadTextIsWrittenRaw(t *testing.T) {
	var key string
	out := captureStdout(t, func() {
		var err error
		key, err = (&StdoutStore{}).Upload(context.Background(), "log.txt", strings.NewReader("plain log line"), "text/plain")
		if err != nil {
			t.Errorf("Upload: %v", err)
		}
	})
	if !strings.Contains(out, "plain log line") {
		t.Errorf("text content should be written raw; got %q", out)
	}
	if key != "stdout://log.txt" {
		t.Errorf("key = %q, want stdout://log.txt", key)
	}
}

func TestStdoutStoreUploadBinaryIsBase64(t *testing.T) {
	raw := []byte{0x00, 0x01, 0x02, 0xff}
	out := captureStdout(t, func() {
		if _, err := (&StdoutStore{}).Upload(context.Background(), "blob.bin", strings.NewReader(string(raw)), "application/octet-stream"); err != nil {
			t.Errorf("Upload: %v", err)
		}
	})
	if !strings.Contains(out, base64.StdEncoding.EncodeToString(raw)) {
		t.Errorf("binary content should be base64-encoded; got %q", out)
	}
}
