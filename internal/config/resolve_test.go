package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOrigin(t *testing.T) {
	c := &Config{Origins: []Origin{
		{ID: "us-east-1", City: "Ashburn"},
		{ID: "us-west-2", City: "Portland"},
	}}

	if got := c.ResolveOrigin("us-west-2"); got == nil || got.ID != "us-west-2" {
		t.Errorf("ResolveOrigin(us-west-2) = %+v, want us-west-2", got)
	}
	// Unknown ID falls back to the first origin.
	if got := c.ResolveOrigin("nope"); got == nil || got.ID != "us-east-1" {
		t.Errorf("unknown ID = %+v, want fallback to first origin", got)
	}
	// Empty ID also falls back to the first origin.
	if got := c.ResolveOrigin(""); got == nil || got.ID != "us-east-1" {
		t.Errorf("empty ID = %+v, want fallback to first origin", got)
	}
	// No origins configured -> nil.
	if got := (&Config{}).ResolveOrigin("x"); got != nil {
		t.Errorf("no origins = %+v, want nil", got)
	}
}

func TestResolveChecksPathPrefersFile(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "technician.yml")

	// With no checks.yml, it points at the checks/ directory.
	if got, want := ResolveChecksPath(cfg), filepath.Join(dir, "checks"); got != want {
		t.Errorf("no checks.yml: got %q, want %q", got, want)
	}

	// Once checks.yml exists, that file wins.
	checksFile := filepath.Join(dir, "checks.yml")
	if err := os.WriteFile(checksFile, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveChecksPath(cfg); got != checksFile {
		t.Errorf("with checks.yml: got %q, want %q", got, checksFile)
	}
}
