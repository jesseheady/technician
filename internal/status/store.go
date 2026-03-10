package status

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/probe"
)

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
}

// ProbeState is the current state of a single probe.
type ProbeState struct {
	Name      string           `json:"name"`
	Type      config.ProbeType `json:"type"`
	Status    string           `json:"status"`    // "up", "down", "pending"
	DownSince string           `json:"down_since"` // human-readable, e.g. "for 2h 15m"
	Uptime    string           `json:"uptime"`    // e.g. "99.7%"
	Latest    *Entry           `json:"latest,omitempty"`
	History   []Entry          `json:"history"`
}

// ProbeGroup is a named group of probes for the status page.
type ProbeGroup struct {
	Name   string       `json:"name"`
	Probes []ProbeState `json:"probes"`
}

// Snapshot is the full status payload returned by the API.
type Snapshot struct {
	Service   string       `json:"service"`
	Site      *SiteInfo    `json:"site,omitempty"`
	Overall   string       `json:"overall"` // "operational", "degraded", "down"
	Types     []string     `json:"types"`   // distinct probe types present
	Groups    []ProbeGroup `json:"groups"`
	Probes    []ProbeState `json:"probes"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type SiteInfo struct {
	Code    string `json:"code"`
	City    string `json:"city"`
	Country string `json:"country"`
}

// Store holds recent probe results in memory with optional file persistence.
type Store struct {
	mu      sync.RWMutex
	probes  map[string]*probeRing
	order   []string // insertion order of probe names
	service string
	site    *SiteInfo
	path    string // file path for persistence; empty = no persistence
}

type probeRing struct {
	name      string
	typ       config.ProbeType
	group     string
	entries   []Entry
	downSince time.Time // zero if currently up
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
		TTFBMs:     float64(r.TTFBDuration) / float64(time.Millisecond),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := probeKey(r.Type, r.Name)
	ring, ok := s.probes[key]
	if !ok {
		ring = &probeRing{name: r.Name, typ: r.Type, group: r.Group}
		s.probes[key] = ring
		s.order = append(s.order, key)
	}
	if r.Group != "" {
		ring.group = r.Group
	}

	// Track down-since (infra errors don't count as the target being down)
	if r.Success || r.InfraError {
		ring.downSince = time.Time{}
	} else if ring.downSince.IsZero() {
		ring.downSince = r.Timestamp
	}

	ring.entries = append(ring.entries, e)
	if len(ring.entries) > maxHistory {
		ring.entries = ring.entries[len(ring.entries)-maxHistory:]
	}
}

// Snapshot returns the current status of all probes.
func (s *Store) Snapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap := &Snapshot{
		Service:   s.service,
		Site:      s.site,
		UpdatedAt: time.Now(),
	}

	upCount := 0
	downCount := 0
	typesSeen := make(map[string]bool)
	groupMap := make(map[string]*ProbeGroup)
	var groupOrder []string

	for _, key := range s.order {
		ring := s.probes[key]
		typesSeen[string(ring.typ)] = true

		ps := ProbeState{
			Name:    ring.name,
			Type:    ring.typ,
			Status:  "pending",
			Uptime:  uptimePercent(ring.entries),
			History: make([]Entry, len(ring.entries)),
		}
		copy(ps.History, ring.entries)

		if len(ring.entries) > 0 {
			last := ring.entries[len(ring.entries)-1]
			ps.Latest = &last
			if last.Success {
				ps.Status = "up"
				upCount++
			} else if last.InfraError {
				ps.Status = "error"
				// Infra errors don't count as the target being down
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
	typeOrder := []config.ProbeType{config.ProbeTypeHTTP, config.ProbeTypeSMTP, config.ProbeTypeTraceroute, config.ProbeTypePlaywright}
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

	return snap
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
	Entries   []Entry          `json:"entries"`
	DownSince time.Time        `json:"down_since,omitempty"`
}

type persistedStore struct {
	Order []string                 `json:"order"`
	Rings map[string]persistedRing `json:"rings"`
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
			Entries:   ring.entries,
			DownSince: ring.downSince,
		}
	}
	s.mu.RUnlock()

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

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("Failed to read status store", "error", err)
		}
		return
	}

	var ps persistedStore
	if err := json.Unmarshal(data, &ps); err != nil {
		slog.Warn("Failed to parse status store", "error", err)
		return
	}

	s.order = ps.Order
	for key, pr := range ps.Rings {
		s.probes[key] = &probeRing{
			name:      pr.Name,
			typ:       pr.Type,
			group:     pr.Group,
			entries:   pr.Entries,
			downSince: pr.DownSince,
		}
	}

	slog.Info("Loaded status store from disk", "probes", len(s.probes), "path", s.path)
}
