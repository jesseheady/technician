package artifact

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStore(t *testing.T) {
	dir := t.TempDir()
	store := NewLocalStore(dir)

	data := []byte("test artifact content")
	path, err := store.Upload(context.Background(), "test/file.txt", bytes.NewReader(data), "text/plain")
	if err != nil {
		t.Fatal(err)
	}

	expected := filepath.Join(dir, "test/file.txt")
	if path != expected {
		t.Errorf("expected path %s, got %s", expected, path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "test artifact content" {
		t.Errorf("expected 'test artifact content', got %s", string(content))
	}
}

func TestNoopStore(t *testing.T) {
	store := &NoopStore{}
	path, err := store.Upload(context.Background(), "key", bytes.NewReader([]byte("data")), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Errorf("expected empty path, got %s", path)
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		driver string
		ok     bool
	}{
		{"none", true},
		{"", true},
		{"local", true},
		{"stdout", true},
		{"unknown", false},
	}

	for _, tt := range tests {
		store, err := New(tt.driver, map[string]string{})
		if tt.ok {
			if err != nil {
				t.Errorf("driver=%q: unexpected error: %v", tt.driver, err)
			}
			if store == nil {
				t.Errorf("driver=%q: expected non-nil store", tt.driver)
			}
		} else {
			if err == nil {
				t.Errorf("driver=%q: expected error", tt.driver)
			}
		}
	}
}
