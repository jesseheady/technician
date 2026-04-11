package status

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/probe"
)

func makeResult(name string, typ config.ProbeType, success bool) *probe.Result {
	return &probe.Result{
		Name:      name,
		Type:      typ,
		Success:   success,
		Duration:  100 * time.Millisecond,
		Timestamp: time.Now(),
	}
}

func TestPushAndSnapshot(t *testing.T) {
	s := NewStore("test-service", nil, "")

	s.Push(makeResult("a", config.ProbeTypeHTTP, true))
	s.Push(makeResult("b", config.ProbeTypeHTTP, false))

	snap := s.Snapshot()
	if snap.Service != "test-service" {
		t.Fatalf("expected service 'test-service', got %q", snap.Service)
	}
	if len(snap.Probes) != 2 {
		t.Fatalf("expected 2 probes, got %d", len(snap.Probes))
	}
	if snap.Summary.Up != 1 {
		t.Fatalf("expected 1 up, got %d", snap.Summary.Up)
	}
	if snap.Summary.Down != 1 {
		t.Fatalf("expected 1 down, got %d", snap.Summary.Down)
	}
	if snap.Overall != "degraded" {
		t.Fatalf("expected 'degraded', got %q", snap.Overall)
	}
}

func TestOverallStatus(t *testing.T) {
	tests := []struct {
		name     string
		results  []struct{ success bool }
		expected string
	}{
		{"all up", []struct{ success bool }{{true}, {true}}, "operational"},
		{"all down", []struct{ success bool }{{false}, {false}}, "down"},
		{"mixed", []struct{ success bool }{{true}, {false}}, "degraded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore("test", nil, "")
			for i, r := range tt.results {
				s.Push(&probe.Result{
					Name: string(rune('a' + i)), Type: config.ProbeTypeHTTP,
					Success: r.success, Timestamp: time.Now(),
				})
			}
			snap := s.Snapshot()
			if snap.Overall != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, snap.Overall)
			}
		})
	}
}

func TestPendingStatusBeforeResults(t *testing.T) {
	s := NewStore("test", nil, "")
	snap := s.Snapshot()
	if snap.Overall != "pending" {
		t.Fatalf("expected 'pending', got %q", snap.Overall)
	}
}

func TestRingBufferCap(t *testing.T) {
	s := NewStore("test", nil, "")
	for i := 0; i < maxHistory+20; i++ {
		s.Push(makeResult("a", config.ProbeTypeHTTP, true))
	}
	snap := s.Snapshot()
	if len(snap.Probes[0].History) != maxHistory {
		t.Fatalf("expected %d entries, got %d", maxHistory, len(snap.Probes[0].History))
	}
}

func TestInfraErrorStatus(t *testing.T) {
	s := NewStore("test", nil, "")
	r := makeResult("a", config.ProbeTypeHTTP, false)
	r.InfraError = true
	s.Push(r)

	snap := s.Snapshot()
	if snap.Probes[0].Status != "error" {
		t.Fatalf("expected 'error', got %q", snap.Probes[0].Status)
	}
	if snap.Summary.Error != 1 {
		t.Fatalf("expected 1 error, got %d", snap.Summary.Error)
	}
}

func TestUptimePercent(t *testing.T) {
	tests := []struct {
		name    string
		entries []Entry
		want    string
	}{
		{"empty", nil, "—"},
		{"all up", []Entry{{Success: true}, {Success: true}}, "100%"},
		{"one down", []Entry{{Success: true}, {Success: false}}, "50.0%"},
		{"all infra error", []Entry{{InfraError: true}, {InfraError: true}}, "—"},
		{"infra error excluded", []Entry{{Success: true}, {InfraError: true}}, "100%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uptimePercent(tt.entries)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestDownSinceTracking(t *testing.T) {
	s := NewStore("test", nil, "")

	// First failure sets downSince
	s.Push(makeResult("a", config.ProbeTypeHTTP, false))
	snap := s.Snapshot()
	if snap.Probes[0].DownSince == "" {
		t.Fatal("expected DownSince to be set")
	}

	// Recovery clears downSince
	s.Push(makeResult("a", config.ProbeTypeHTTP, true))
	snap = s.Snapshot()
	if snap.Probes[0].DownSince != "" {
		t.Fatal("expected DownSince to be cleared after recovery")
	}
}

func TestBudgetChecks(t *testing.T) {
	s := NewStore("test", nil, "")
	s.Push(makeResult("a", config.ProbeTypeHTTP, true))

	s.RecordBudgetCheck("a", "duration", false)
	s.RecordBudgetCheck("a", "ttfb", true)

	snap := s.Snapshot()
	if snap.Summary.BudgetTotal != 2 {
		t.Fatalf("expected 2 budget checks, got %d", snap.Summary.BudgetTotal)
	}
	if snap.Summary.BudgetViolations != 1 {
		t.Fatalf("expected 1 violation, got %d", snap.Summary.BudgetViolations)
	}

	// Budget checks should be attached to the probe
	if len(snap.Probes[0].BudgetChecks) != 2 {
		t.Fatalf("expected 2 budget checks on probe, got %d", len(snap.Probes[0].BudgetChecks))
	}
}

func TestBudgetEscalation(t *testing.T) {
	s := NewStore("test", nil, "")
	s.Push(makeResult("a", config.ProbeTypeHTTP, true))

	// First violation → warn
	crossed := s.RecordBudgetCheck("a", "dns", true)
	if crossed {
		t.Error("should not cross threshold on first violation")
	}
	snap := s.Snapshot()
	bc := findBudget(snap.Probes[0].BudgetChecks, "dns")
	if bc.Severity != "warn" {
		t.Errorf("expected warn, got %s", bc.Severity)
	}

	// Second violation → still warn
	s.RecordBudgetCheck("a", "dns", true)
	snap = s.Snapshot()
	bc = findBudget(snap.Probes[0].BudgetChecks, "dns")
	if bc.Severity != "warn" {
		t.Errorf("expected warn, got %s", bc.Severity)
	}

	// Third violation → fail, crosses threshold
	crossed = s.RecordBudgetCheck("a", "dns", true)
	if !crossed {
		t.Error("should cross threshold on third consecutive violation")
	}
	snap = s.Snapshot()
	bc = findBudget(snap.Probes[0].BudgetChecks, "dns")
	if bc.Severity != "fail" {
		t.Errorf("expected fail, got %s", bc.Severity)
	}

	// Fourth violation → still fail, but threshold not re-signaled
	crossed = s.RecordBudgetCheck("a", "dns", true)
	if crossed {
		t.Error("should only signal threshold crossing once")
	}

	// Single pass resets to pass
	s.RecordBudgetCheck("a", "dns", false)
	snap = s.Snapshot()
	bc = findBudget(snap.Probes[0].BudgetChecks, "dns")
	if bc.Severity != "pass" {
		t.Errorf("expected pass after reset, got %s", bc.Severity)
	}

	// Next violation starts fresh at warn
	s.RecordBudgetCheck("a", "dns", true)
	snap = s.Snapshot()
	bc = findBudget(snap.Probes[0].BudgetChecks, "dns")
	if bc.Severity != "warn" {
		t.Errorf("expected warn after reset+violation, got %s", bc.Severity)
	}
}

func findBudget(checks []BudgetCheck, metric string) BudgetCheck {
	for _, c := range checks {
		if c.Metric == metric {
			return c
		}
	}
	return BudgetCheck{}
}

func TestGrouping(t *testing.T) {
	s := NewStore("test", nil, "")
	r1 := makeResult("a", config.ProbeTypeHTTP, true)
	r1.Group = "Web"
	s.Push(r1)

	r2 := makeResult("b", config.ProbeTypeHTTP, true)
	r2.Group = "Infra"
	s.Push(r2)

	snap := s.Snapshot()
	if len(snap.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(snap.Groups))
	}
	if snap.Groups[0].Name != "Web" {
		t.Fatalf("expected first group 'Web', got %q", snap.Groups[0].Name)
	}
	if snap.Groups[1].Name != "Infra" {
		t.Fatalf("expected second group 'Infra', got %q", snap.Groups[1].Name)
	}
}

func TestSiteInfo(t *testing.T) {
	site := &config.Site{Code: "us-east-1", City: "Virginia", Country: "US"}
	s := NewStore("test", site, "")
	snap := s.Snapshot()
	if snap.Site == nil {
		t.Fatal("expected site info")
	}
	if snap.Site.Code != "us-east-1" {
		t.Fatalf("expected code 'us-east-1', got %q", snap.Site.Code)
	}
}

// --- Persistence tests ---

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	// Create store, push data, save
	s1 := NewStore("test", nil, path)
	s1.Push(makeResult("a", config.ProbeTypeHTTP, true))
	s1.Push(makeResult("b", config.ProbeTypeSMTP, false))
	s1.RecordBudgetCheck("a", "duration", false)
	s1.RecordBudgetCheck("a", "ttfb", true)
	s1.Save()

	// Create new store from same path — should load
	s2 := NewStore("test", nil, path)
	snap := s2.Snapshot()

	if len(snap.Probes) != 2 {
		t.Fatalf("expected 2 probes after load, got %d", len(snap.Probes))
	}
	if snap.Summary.BudgetTotal != 2 {
		t.Fatalf("expected 2 budget checks after load, got %d", snap.Summary.BudgetTotal)
	}
	if snap.Summary.BudgetViolations != 1 {
		t.Fatalf("expected 1 violation after load, got %d", snap.Summary.BudgetViolations)
	}
}

func TestPersistencePreservesOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	s1 := NewStore("test", nil, path)
	s1.Push(makeResult("first", config.ProbeTypeHTTP, true))
	s1.Push(makeResult("second", config.ProbeTypeHTTP, true))
	s1.Push(makeResult("third", config.ProbeTypeHTTP, true))
	s1.Save()

	s2 := NewStore("test", nil, path)
	snap := s2.Snapshot()

	expected := []string{"first", "second", "third"}
	for i, p := range snap.Probes {
		if p.Name != expected[i] {
			t.Fatalf("probe %d: expected %q, got %q", i, expected[i], p.Name)
		}
	}
}

func TestPersistenceMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	// Should not error — just start empty
	s := NewStore("test", nil, path)
	snap := s.Snapshot()
	if len(snap.Probes) != 0 {
		t.Fatalf("expected 0 probes, got %d", len(snap.Probes))
	}
}

func TestPersistenceCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	os.WriteFile(path, []byte("not json"), 0o644)

	// Should not panic — just start empty
	s := NewStore("test", nil, path)
	snap := s.Snapshot()
	if len(snap.Probes) != 0 {
		t.Fatalf("expected 0 probes, got %d", len(snap.Probes))
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "status.json")

	s := NewStore("test", nil, "")
	s.path = path // set path manually to skip load
	s.Push(makeResult("a", config.ProbeTypeHTTP, true))
	s.Save()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected file to be created")
	}
}

func TestPersistenceDownSince(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	s1 := NewStore("test", nil, path)
	s1.Push(makeResult("a", config.ProbeTypeHTTP, false))
	s1.Save()

	s2 := NewStore("test", nil, path)
	snap := s2.Snapshot()
	if snap.Probes[0].DownSince == "" {
		t.Fatal("expected DownSince to survive persistence")
	}
}

// --- Handler tests ---

func TestAPIStatusEndpoint(t *testing.T) {
	s := NewStore("test-svc", nil, "")
	s.Push(makeResult("probe-a", config.ProbeTypeHTTP, true))

	handler := Handler(s)
	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}

	var snap Snapshot
	if err := json.NewDecoder(w.Body).Decode(&snap); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if snap.Service != "test-svc" {
		t.Fatalf("expected service 'test-svc', got %q", snap.Service)
	}
	if len(snap.Probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(snap.Probes))
	}
}

func TestStatusPageEndpoint(t *testing.T) {
	s := NewStore("test-svc", nil, "")
	s.Push(makeResult("probe-a", config.ProbeTypeHTTP, true))

	handler := Handler(s)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("expected text/html, got %q", ct)
	}
	body := w.Body.String()
	if len(body) < 100 {
		t.Fatal("expected non-trivial HTML response")
	}
}

func TestStatusPage404ForNonRoot(t *testing.T) {
	s := NewStore("test", nil, "")
	handler := Handler(s)
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// --- Latency percentiles ---

func TestComputeLatency(t *testing.T) {
	entries := make([]Entry, 20)
	for i := range entries {
		entries[i] = Entry{Success: true, DurationMs: float64((i + 1) * 10)} // 10, 20, ..., 200
	}
	lat := computeLatency(entries)
	if lat == nil {
		t.Fatal("expected latency, got nil")
	}
	// P50 of 10..200 (20 values): index 9.5 → interpolation between 100 and 110
	if lat.P50 < 100 || lat.P50 > 110 {
		t.Errorf("expected P50 ~105, got %.1f", lat.P50)
	}
	if lat.P95 < 185 || lat.P95 > 200 {
		t.Errorf("expected P95 ~191, got %.1f", lat.P95)
	}
}

func TestComputeLatencyTooFewEntries(t *testing.T) {
	entries := []Entry{{Success: true, DurationMs: 100}}
	if lat := computeLatency(entries); lat != nil {
		t.Fatalf("expected nil for single entry, got %+v", lat)
	}
}

func TestComputeLatencyExcludesFailures(t *testing.T) {
	entries := []Entry{
		{Success: true, DurationMs: 100},
		{Success: false, DurationMs: 9999},
		{Success: true, DurationMs: 200},
		{Success: true, InfraError: true, DurationMs: 8888},
		{Success: true, DurationMs: 300},
	}
	lat := computeLatency(entries)
	if lat == nil {
		t.Fatal("expected latency")
	}
	// Only 100, 200, 300 should be included; P50 = 200
	if lat.P50 != 200 {
		t.Errorf("expected P50=200, got %.1f", lat.P50)
	}
}

func TestSnapshotIncludesLatency(t *testing.T) {
	s := NewStore("test", nil, "")
	for i := 0; i < 10; i++ {
		r := &probe.Result{
			Name: "a", Type: config.ProbeTypeHTTP, Success: true,
			Duration:  time.Duration((i+1)*100) * time.Millisecond,
			Timestamp: time.Now(),
		}
		s.Push(r)
	}
	snap := s.Snapshot()
	if snap.Probes[0].Latency == nil {
		t.Fatal("expected latency in snapshot")
	}
	if snap.Probes[0].Latency.P50 <= 0 {
		t.Errorf("expected positive P50, got %.1f", snap.Probes[0].Latency.P50)
	}
}

// --- Timing breakdown ---

func TestComputeTiming(t *testing.T) {
	entries := []Entry{
		{Success: true, DurationMs: 500, DNSMs: 20, TLSMs: 30, TTFBMs: 400},
		{Success: true, DurationMs: 600, DNSMs: 10, TLSMs: 40, TTFBMs: 500},
	}
	tb := computeTiming(entries)
	if tb == nil {
		t.Fatal("expected timing, got nil")
	}
	if tb.DNSMs != 15 {
		t.Errorf("expected DNS avg 15, got %.1f", tb.DNSMs)
	}
	if tb.TLSMs != 35 {
		t.Errorf("expected TLS avg 35, got %.1f", tb.TLSMs)
	}
	if tb.TTFBMs != 450 {
		t.Errorf("expected TTFB avg 450, got %.1f", tb.TTFBMs)
	}
	if tb.TransferMs != 100 {
		t.Errorf("expected Transfer avg 100, got %.1f", tb.TransferMs)
	}
}

func TestComputeTimingSkipsNonHTTP(t *testing.T) {
	entries := []Entry{
		{Success: true, DurationMs: 500, TTFBMs: 0}, // no HTTP timing
	}
	if tb := computeTiming(entries); tb != nil {
		t.Fatalf("expected nil for entries without timing, got %+v", tb)
	}
}

// --- Helpers ---

func TestBackupAndFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	// Create store with data and save
	s := NewStore("test", nil, path)
	s.Push(makeResult("web", config.ProbeTypeHTTP, true))
	s.Save()

	// Create backup
	s.SaveBackup()

	// Verify backup file exists
	backupPath := path + "." + time.Now().Format("20060102")
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatal("backup file should exist")
	}

	// Calling SaveBackup again is idempotent (same day)
	s.SaveBackup()

	// Corrupt the main file
	os.WriteFile(path, []byte("corrupted"), 0o644)

	// Load should fall back to backup
	s2 := NewStore("test", nil, path)
	snap := s2.Snapshot()
	if len(snap.Probes) != 1 {
		t.Fatalf("expected 1 probe from backup, got %d", len(snap.Probes))
	}
	if snap.Probes[0].Name != "web" {
		t.Errorf("expected probe 'web', got %s", snap.Probes[0].Name)
	}
}

func TestBackupFallbackMissingMain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")

	// Create a "backup" file directly (simulating prior day's backup)
	s := NewStore("test", nil, path)
	s.Push(makeResult("api", config.ProbeTypeHTTP, true))
	s.Save()

	// Move main file to a dated backup, remove main
	backup := path + "." + time.Now().Format("20060102")
	os.Rename(path, backup)

	// Load should find the backup
	s2 := NewStore("test", nil, path)
	snap := s2.Snapshot()
	if len(snap.Probes) != 1 || snap.Probes[0].Name != "api" {
		t.Fatalf("expected probe 'api' from backup, got %v", snap.Probes)
	}
}

func TestFmtDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "for 30s"},
		{5 * time.Minute, "for 5m"},
		{2 * time.Hour, "for 2h"},
		{2*time.Hour + 15*time.Minute, "for 2h 15m"},
	}
	for _, tt := range tests {
		got := fmtDuration(tt.d)
		if got != tt.want {
			t.Errorf("fmtDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
