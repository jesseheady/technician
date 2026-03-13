package status

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
	"github.com/monkeyWzr/technician/internal/probe"
)

const backupRetention = 90 * 24 * time.Hour

const maxHistory = 90

// Entry is a compact snapshot of a single probe result.
type Entry struct {
	Success    bool      `json:"success"`
	InfraError bool      `json:"infra_error,omitempty"` // probe infrastructure failed, not the target
	DurationMs float64   `json:"duration_ms"`
	Timestamp  time.Time `json:"timestamp"`
	Error      string    `json:"error,omitempty"`

	// HTTP details (zero values omitted)
	StatusCode int     `json:"status_code,omitempty"`
	DNSMs      float64 `json:"dns_ms,omitempty"`
	TLSMs      float64 `json:"tls_ms,omitempty"`
	TTFBMs     float64 `json:"ttfb_ms,omitempty"`

	// NTP details
	NTPOffsetMs float64 `json:"ntp_offset_ms,omitempty"`

	// TLS certificate details
	CertDaysRemaining int       `json:"cert_days_remaining,omitempty"`
	CertValid         bool      `json:"cert_valid,omitempty"`
	CertExpiry        time.Time `json:"cert_expiry,omitempty"`

	// ICMP details
	ICMPPacketLoss float64 `json:"icmp_packet_loss,omitempty"`

	// gRPC details
	GRPCStatus string `json:"grpc_status,omitempty"`
}

// BudgetCheck is a single budget metric evaluation for display on the status page.
type BudgetCheck struct {
	Metric   string `json:"metric"`
	Severity string `json:"severity"` // "pass", "warn", "fail"
}

// BudgetFailThreshold is the number of consecutive violations before a budget
// check escalates from "warn" (amber) to "fail" (red / alert-worthy).
const BudgetFailThreshold = 3

// Latency holds percentile latencies computed from the ring buffer.
type Latency struct {
	P50 float64 `json:"p50"`
	P90 float64 `json:"p90"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

// TimingBreakdown holds average timing phases for successful HTTP probes.
type TimingBreakdown struct {
	DNSMs      float64 `json:"dns_ms"`
	TLSMs      float64 `json:"tls_ms"`
	TTFBMs     float64 `json:"ttfb_ms"`
	TransferMs float64 `json:"transfer_ms"`
}

// ProbeState is the current state of a single probe.
type ProbeState struct {
	Name         string           `json:"name"`
	Type         config.ProbeType `json:"type"`
	Domain       string           `json:"domain,omitempty"` // canonical hostname for domain grouping
	Status       string           `json:"status"`           // "up", "down", "pending"
	DownSince    string           `json:"down_since"` // human-readable, e.g. "for 2h 15m"
	Uptime       string           `json:"uptime"`    // e.g. "99.7%"
	Latency      *Latency         `json:"latency,omitempty"`
	Timing       *TimingBreakdown `json:"timing,omitempty"`
	Latest       *Entry           `json:"latest,omitempty"`
	History      []Entry          `json:"history"`
	BudgetChecks []BudgetCheck    `json:"budget_checks,omitempty"`
}

// ProbeGroup is a named group of probes for the status page.
type ProbeGroup struct {
	Name   string       `json:"name"`
	Probes []ProbeState `json:"probes"`
}

// Summary provides aggregate counts for the status page header.
type Summary struct {
	Total            int `json:"total"`
	Up               int `json:"up"`
	Down             int `json:"down"`
	Error            int `json:"error"`
	BudgetTotal      int `json:"budget_total"`
	BudgetViolations int `json:"budget_violations"`
}

// Snapshot is the full status payload returned by the API.
type Snapshot struct {
	Service   string       `json:"service"`
	Site      *SiteInfo    `json:"site,omitempty"`
	Overall   string       `json:"overall"` // "operational", "degraded", "down"
	Summary   Summary      `json:"summary"`
	Types     []string     `json:"types"` // distinct probe types present
	Groups    []ProbeGroup `json:"groups"`
	Probes    []ProbeState `json:"probes"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type SiteInfo struct {
	Code    string `json:"code"`
	City    string `json:"city"`
	Country string `json:"country"`
}

const snapshotTTL = 2 * time.Second

// Store holds recent probe results in memory with optional file persistence.
type Store struct {
	mu      sync.RWMutex
	probes  map[string]*probeRing
	order   []string // insertion order of probe names
	service string
	site    *SiteInfo
	path    string // file path for persistence; empty = no persistence

	budgetMu     sync.RWMutex
	budgetChecks map[string]budgetState // keyed by "probe:metric"

	snapMu    sync.Mutex
	snapCache *Snapshot
	snapTime  time.Time
}

type budgetState struct {
	violated              bool
	consecutiveViolations int
}

type probeRing struct {
	name      string
	typ       config.ProbeType
	group     string
	target    string // canonical hostname/domain
	entries   []Entry
	head      int  // next write position (circular)
	full      bool // true once the buffer has wrapped
	downSince time.Time // zero if currently up
}

// push appends an entry to the circular ring buffer.
func (r *probeRing) push(e Entry) {
	if len(r.entries) < maxHistory {
		// Still filling up — just append
		r.entries = append(r.entries, e)
		return
	}
	// Buffer is full — overwrite oldest
	r.entries[r.head] = e
	r.head = (r.head + 1) % maxHistory
	r.full = true
}

// ordered returns entries in chronological order (oldest first).
func (r *probeRing) ordered() []Entry {
	if !r.full || r.head == 0 {
		return r.entries
	}
	out := make([]Entry, len(r.entries))
	copy(out, r.entries[r.head:])
	copy(out[len(r.entries)-r.head:], r.entries[:r.head])
	return out
}

// NewStore creates a new store. If path is non-empty, the store will persist
// its state to that file and load any existing state on creation.
func NewStore(service string, site *config.Site, path string) *Store {
	s := &Store{
		probes:  make(map[string]*probeRing),
		service: service,
		path:    path,
	}
	if site != nil {
		s.site = &SiteInfo{
			Code:    site.Code,
			City:    site.City,
			Country: site.Country,
		}
	}
	if path != "" {
		s.load()
	}
	return s
}

// probeKey returns a unique key for a probe by combining type and name,
// since different probe types (e.g. http and traceroute) can share a name.
func probeKey(typ config.ProbeType, name string) string {
	return string(typ) + ":" + name
}

// Push adds a probe result to the store.
func (s *Store) Push(r *probe.Result) {
	e := Entry{
		Success:    r.Success,
		InfraError: r.InfraError,
		DurationMs: float64(r.Duration) / float64(time.Millisecond),
		Timestamp:  r.Timestamp,
		Error:      r.Error,
		StatusCode: r.StatusCode,
		DNSMs:      float64(r.DNSDuration) / float64(time.Millisecond),
		TLSMs:      float64(r.TLSDuration) / float64(time.Millisecond),
		TTFBMs:      float64(r.TTFBDuration) / float64(time.Millisecond),
		NTPOffsetMs:       r.NTPOffsetMs,
		CertDaysRemaining: r.CertDaysRemaining,
		CertValid:         r.CertValid,
		CertExpiry:        r.CertExpiry,
		ICMPPacketLoss:    r.ICMPPacketLoss,
		GRPCStatus:        r.GRPCStatus,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := probeKey(r.Type, r.Name)
	ring, ok := s.probes[key]
	if !ok {
		ring = &probeRing{name: r.Name, typ: r.Type, group: r.Group, target: r.Target}
		s.probes[key] = ring
		s.order = append(s.order, key)
	}
	if r.Group != "" {
		ring.group = r.Group
	}
	if r.Target != "" {
		ring.target = r.Target
	}

	// Track down-since (infra errors don't count as the target being down)
	if r.Success || r.InfraError {
		ring.downSince = time.Time{}
	} else if ring.downSince.IsZero() {
		ring.downSince = r.Timestamp
	}

	ring.push(e)

	s.invalidateCache()
}

// RecordBudgetCheck updates the per-check budget state for the status page summary.
// Returns true when the check has just crossed the fail threshold (for alerting).
func (s *Store) RecordBudgetCheck(probe, metric string, violated bool) bool {
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()

	if s.budgetChecks == nil {
		s.budgetChecks = make(map[string]budgetState)
	}

	key := probe + ":" + metric
	prev := s.budgetChecks[key]

	var bs budgetState
	if violated {
		bs.violated = true
		bs.consecutiveViolations = prev.consecutiveViolations + 1
	}
	// else: zero value resets both fields

	s.budgetChecks[key] = bs
	s.invalidateCache()
	// Signal that we just crossed the fail threshold
	return bs.consecutiveViolations == BudgetFailThreshold
}

// Snapshot returns the current status of all probes. Results are cached
// for snapshotTTL to avoid recomputing percentiles on rapid requests.
func (s *Store) Snapshot() *Snapshot {
	s.snapMu.Lock()
	if s.snapCache != nil && time.Since(s.snapTime) < snapshotTTL {
		cached := s.snapCache
		s.snapMu.Unlock()
		return cached
	}
	s.snapMu.Unlock()

	snap := s.computeSnapshot()

	s.snapMu.Lock()
	s.snapCache = snap
	s.snapTime = time.Now()
	s.snapMu.Unlock()

	return snap
}

func (s *Store) invalidateCache() {
	s.snapMu.Lock()
	s.snapCache = nil
	s.snapMu.Unlock()
}

func (s *Store) computeSnapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap := &Snapshot{
		Service:   s.service,
		Site:      s.site,
		UpdatedAt: time.Now(),
	}

	upCount := 0
	downCount := 0
	errCount := 0
	typesSeen := make(map[string]bool)
	groupMap := make(map[string]*ProbeGroup)
	var groupOrder []string

	for _, key := range s.order {
		ring := s.probes[key]
		typesSeen[string(ring.typ)] = true

		entries := ring.ordered()
		ps := ProbeState{
			Name:    ring.name,
			Type:    ring.typ,
			Domain:  ring.target,
			Status:  "pending",
			Uptime:  uptimePercent(entries),
			Latency: computeLatency(entries),
			Timing:  computeTiming(entries),
			History: entries,
		}

		if len(entries) > 0 {
			last := entries[len(entries)-1]
			ps.Latest = &last
			if last.Success {
				ps.Status = "up"
				upCount++
			} else if last.InfraError {
				ps.Status = "error"
				errCount++
			} else {
				ps.Status = "down"
				downCount++
				if !ring.downSince.IsZero() {
					ps.DownSince = fmtDuration(time.Since(ring.downSince))
				}
			}
		}

		snap.Probes = append(snap.Probes, ps)

		gName := ring.group
		g, exists := groupMap[gName]
		if !exists {
			g = &ProbeGroup{Name: gName}
			groupMap[gName] = g
			groupOrder = append(groupOrder, gName)
		}
		g.Probes = append(g.Probes, ps)
	}

	for _, gName := range groupOrder {
		snap.Groups = append(snap.Groups, *groupMap[gName])
	}

	// Collect distinct types in stable order
	typeOrder := []config.ProbeType{config.ProbeTypeHTTP, config.ProbeTypeTCP, config.ProbeTypeUDP, config.ProbeTypeDNS, config.ProbeTypeICMP, config.ProbeTypeGRPC, config.ProbeTypeNTP, config.ProbeTypeTLS, config.ProbeTypeSMTP, config.ProbeTypeTraceroute, config.ProbeTypePlaywright}
	for _, t := range typeOrder {
		if typesSeen[string(t)] {
			snap.Types = append(snap.Types, string(t))
		}
	}

	switch {
	case downCount == 0 && upCount > 0:
		snap.Overall = "operational"
	case upCount == 0 && downCount > 0:
		snap.Overall = "down"
	case downCount > 0:
		snap.Overall = "degraded"
	default:
		snap.Overall = "pending"
	}

	s.budgetMu.RLock()
	budgetViolations := 0
	probeBudgets := make(map[string][]BudgetCheck) // keyed by probe name
	for key, bs := range s.budgetChecks {
		if bs.violated {
			budgetViolations++
		}
		// key format: "probeName:metric"
		for i := len(key) - 1; i >= 0; i-- {
			if key[i] == ':' {
				probeName := key[:i]
				metric := key[i+1:]
				severity := "pass"
				if bs.consecutiveViolations >= BudgetFailThreshold {
					severity = "fail"
				} else if bs.violated {
					severity = "warn"
				}
				probeBudgets[probeName] = append(probeBudgets[probeName], BudgetCheck{
					Metric:   metric,
					Severity: severity,
				})
				break
			}
		}
	}
	// Sort budget checks by metric name for stable rendering
	for k := range probeBudgets {
		sort.Slice(probeBudgets[k], func(i, j int) bool {
			return probeBudgets[k][i].Metric < probeBudgets[k][j].Metric
		})
	}
	// Attach budget checks to probes
	for i := range snap.Probes {
		if checks, ok := probeBudgets[snap.Probes[i].Name]; ok {
			snap.Probes[i].BudgetChecks = checks
		}
	}
	// Also update probes inside groups
	for gi := range snap.Groups {
		for pi := range snap.Groups[gi].Probes {
			if checks, ok := probeBudgets[snap.Groups[gi].Probes[pi].Name]; ok {
				snap.Groups[gi].Probes[pi].BudgetChecks = checks
			}
		}
	}
	snap.Summary = Summary{
		Total:            len(s.order),
		Up:               upCount,
		Down:             downCount,
		Error:            errCount,
		BudgetTotal:      len(s.budgetChecks),
		BudgetViolations: budgetViolations,
	}
	s.budgetMu.RUnlock()

	return snap
}

func computeLatency(entries []Entry) *Latency {
	var durations []float64
	for _, e := range entries {
		if e.Success && !e.InfraError {
			durations = append(durations, e.DurationMs)
		}
	}
	if len(durations) < 2 {
		return nil
	}
	sort.Float64s(durations)
	return &Latency{
		P50: percentile(durations, 0.50),
		P90: percentile(durations, 0.90),
		P95: percentile(durations, 0.95),
		P99: percentile(durations, 0.99),
	}
}

func percentile(sorted []float64, p float64) float64 {
	idx := p * float64(len(sorted)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

func computeTiming(entries []Entry) *TimingBreakdown {
	var dns, tls, ttfb, total float64
	var n int
	for _, e := range entries {
		if !e.Success || e.InfraError || e.TTFBMs == 0 {
			continue
		}
		dns += e.DNSMs
		tls += e.TLSMs
		ttfb += e.TTFBMs
		total += e.DurationMs
		n++
	}
	if n == 0 {
		return nil
	}
	fn := float64(n)
	transfer := total/fn - ttfb/fn
	if transfer < 1 { // sub-millisecond transfer is noise
		transfer = 0
	}
	return &TimingBreakdown{
		DNSMs:      dns / fn,
		TLSMs:      tls / fn,
		TTFBMs:     ttfb / fn,
		TransferMs: transfer,
	}
}

func uptimePercent(entries []Entry) string {
	if len(entries) == 0 {
		return "—"
	}
	up := 0
	total := 0
	for _, e := range entries {
		if e.InfraError {
			continue // don't count infra errors in uptime calculation
		}
		total++
		if e.Success {
			up++
		}
	}
	if total == 0 {
		return "—" // all entries are infra errors
	}
	pct := float64(up) / float64(total) * 100
	if pct == 100 {
		return "100%"
	}
	return fmt.Sprintf("%.1f%%", pct)
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("for %ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("for %dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("for %dh", h)
	}
	return fmt.Sprintf("for %dh %dm", h, m)
}

// --- Persistence ---

// persistedRing is the JSON-serializable form of probeRing.
type persistedRing struct {
	Name      string           `json:"name"`
	Type      config.ProbeType `json:"type"`
	Group     string           `json:"group,omitempty"`
	Target    string           `json:"target,omitempty"`
	Entries   []Entry          `json:"entries"`
	DownSince time.Time        `json:"down_since,omitempty"`
}

type persistedBudget struct {
	Violated              bool `json:"violated"`
	ConsecutiveViolations int  `json:"consecutive_violations,omitempty"`
}

type persistedStore struct {
	Order        []string                    `json:"order"`
	Rings        map[string]persistedRing    `json:"rings"`
	BudgetChecks map[string]persistedBudget  `json:"budget_checks,omitempty"` // "probe:metric" -> state
}

// persistedStoreRaw is used for backwards-compatible loading: budget_checks
// used to be map[string]bool, so we decode it as RawMessage first.
type persistedStoreRaw struct {
	Order        []string                 `json:"order"`
	Rings        map[string]persistedRing `json:"rings"`
	BudgetChecks json.RawMessage          `json:"budget_checks,omitempty"`
}

// Save writes the current store state to disk. Safe to call concurrently.
func (s *Store) Save() {
	if s.path == "" {
		return
	}

	s.mu.RLock()
	ps := persistedStore{
		Order: s.order,
		Rings: make(map[string]persistedRing, len(s.probes)),
	}
	for key, ring := range s.probes {
		ps.Rings[key] = persistedRing{
			Name:      ring.name,
			Type:      ring.typ,
			Group:     ring.group,
			Target:    ring.target,
			Entries:   ring.ordered(),
			DownSince: ring.downSince,
		}
	}
	s.mu.RUnlock()

	s.budgetMu.RLock()
	if len(s.budgetChecks) > 0 {
		ps.BudgetChecks = make(map[string]persistedBudget, len(s.budgetChecks))
		for key, bs := range s.budgetChecks {
			ps.BudgetChecks[key] = persistedBudget{
				Violated:              bs.violated,
				ConsecutiveViolations: bs.consecutiveViolations,
			}
		}
	}
	s.budgetMu.RUnlock()

	data, err := json.Marshal(ps)
	if err != nil {
		slog.Warn("Failed to marshal status store", "error", err)
		return
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		slog.Warn("Failed to create status store directory", "error", err)
		return
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Warn("Failed to write status store", "error", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		slog.Warn("Failed to rename status store", "error", err)
	}
}

// SaveBackup creates a daily timestamped backup of the current status file
// and prunes backups older than the retention period (90 days).
func (s *Store) SaveBackup() {
	if s.path == "" {
		return
	}
	src, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	backup := s.path + "." + time.Now().Format("20060102")
	if _, err := os.Stat(backup); err == nil {
		return // today's backup already exists
	}
	if err := os.WriteFile(backup, src, 0o644); err != nil {
		slog.Warn("Failed to write status backup", "error", err)
		return
	}
	slog.Info("Created status backup", "file", filepath.Base(backup))
	s.pruneBackups()
}

func (s *Store) pruneBackups() {
	dir := filepath.Dir(s.path)
	base := filepath.Base(s.path) + "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-backupRetention)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base) {
			continue
		}
		suffix := strings.TrimPrefix(name, base)
		t, err := time.Parse("20060102", suffix)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			os.Remove(filepath.Join(dir, name))
			slog.Info("Pruned old status backup", "file", name)
		}
	}
}

// latestBackup returns the contents of the most recent dated backup,
// or nil if none exist.
func (s *Store) latestBackup() []byte {
	dir := filepath.Dir(s.path)
	base := filepath.Base(s.path) + "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var latest string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base) {
			continue
		}
		suffix := strings.TrimPrefix(name, base)
		if _, err := time.Parse("20060102", suffix); err != nil {
			continue
		}
		if name > latest {
			latest = name
		}
	}
	if latest == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, latest))
	if err != nil {
		return nil
	}
	slog.Info("Falling back to status backup", "file", latest)
	return data
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("Failed to read status store", "error", err)
		}
		// Try backup
		data = s.latestBackup()
		if data == nil {
			return
		}
	}

	if !s.tryLoadData(data) {
		// Main file corrupt — try backup
		slog.Warn("Main status store failed to parse, trying backup")
		backup := s.latestBackup()
		if backup != nil {
			s.tryLoadData(backup)
		}
	}
}

// tryLoadData attempts to parse and load store data. Returns true on success.
func (s *Store) tryLoadData(data []byte) bool {
	// Use raw struct so budget_checks field type mismatch doesn't block
	// loading the rest of the store (probe rings, order, etc.).
	var raw persistedStoreRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Warn("Failed to parse status store", "error", err)
		return false
	}

	s.order = raw.Order
	for key, pr := range raw.Rings {
		s.probes[key] = &probeRing{
			name:      pr.Name,
			typ:       pr.Type,
			group:     pr.Group,
			target:    pr.Target,
			entries:   pr.Entries,
			downSince: pr.DownSince,
		}
	}

	// Try new format (map[string]persistedBudget) first, fall back to
	// legacy format (map[string]bool) for backwards compatibility.
	if len(raw.BudgetChecks) > 0 {
		var newFmt map[string]persistedBudget
		if err := json.Unmarshal(raw.BudgetChecks, &newFmt); err == nil {
			s.budgetChecks = make(map[string]budgetState, len(newFmt))
			for key, pb := range newFmt {
				s.budgetChecks[key] = budgetState{
					violated:              pb.Violated,
					consecutiveViolations: pb.ConsecutiveViolations,
				}
			}
		} else {
			// Legacy: map[string]bool
			var oldFmt map[string]bool
			if err := json.Unmarshal(raw.BudgetChecks, &oldFmt); err == nil {
				s.budgetChecks = make(map[string]budgetState, len(oldFmt))
				for key, violated := range oldFmt {
					s.budgetChecks[key] = budgetState{violated: violated}
				}
				slog.Info("Migrated legacy budget checks format", "count", len(oldFmt))
			} else {
				slog.Warn("Failed to parse budget checks", "error", err)
			}
		}
	}

	slog.Info("Loaded status store from disk", "probes", len(s.probes), "budget_checks", len(s.budgetChecks))
	return true
}
