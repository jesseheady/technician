package status

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

// DomainGroup groups checks that share the same target domain/host.
type DomainGroup struct {
	Domain string
	Checks []CheckState
}

// CategoryInfo represents a category of check types for tab navigation.
type CategoryInfo struct {
	Name   string
	Status string
	Count  int
}

// categoryForCheckType maps a check type to its display category.
func categoryForCheckType(t config.CheckType) string {
	switch t {
	case config.CheckTypeICMP, config.CheckTypeTCP, config.CheckTypeUDP, config.CheckTypeDNS, config.CheckTypeTraceroute:
		return "Network"
	case config.CheckTypeHTTP, config.CheckTypePlaywright:
		return "Web"
	case config.CheckTypeGRPC, config.CheckTypeSMTP, config.CheckTypeNTP, config.CheckTypeWebSocket:
		return "Services"
	case config.CheckTypeTLS, config.CheckTypeBGP, config.CheckTypeDomainExpiry:
		return "Security"
	default:
		return "Other"
	}
}

//go:embed page.html
var pageHTML string

var pageTmpl = template.Must(template.New("page").Funcs(template.FuncMap{
	"sub": func(a, b int) int { return a - b },
	"fmtMs": func(ms float64) string {
		if ms < 1000 {
			return fmt.Sprintf("%dms", int(math.Round(ms)))
		}
		return fmt.Sprintf("%.2fs", ms/1000)
	},
	"relTime": func(t time.Time) string {
		d := time.Since(t).Seconds()
		switch {
		case d < 60:
			return fmt.Sprintf("%ds ago", int(d))
		case d < 3600:
			return fmt.Sprintf("%dm ago", int(d/60))
		case d < 86400:
			return fmt.Sprintf("%dh ago", int(d/3600))
		default:
			return fmt.Sprintf("%dd ago", int(d/86400))
		}
	},
	"barHeight": func(ms, maxMs float64) int {
		if maxMs <= 0 {
			return 4
		}
		h := int((ms / maxMs) * 32)
		if h < 4 {
			return 4
		}
		return h
	},
	"maxDuration": func(entries []Entry) float64 {
		var m float64
		for _, e := range entries {
			if e.DurationMs > m {
				m = e.DurationMs
			}
		}
		if m <= 0 {
			return 1
		}
		return m
	},
	"checkCount": func(checks []CheckState) int {
		return len(checks)
	},
	"hasViolation": func(checks []BudgetCheck) bool {
		for _, c := range checks {
			if c.Severity != "pass" {
				return true
			}
		}
		return false
	},
	"groupStatus": func(checks []CheckState) string {
		down := 0
		errCount := 0
		for _, p := range checks {
			if p.Status == "down" {
				down++
			} else if p.Status == "error" {
				errCount++
			}
		}
		if down == 0 && errCount == 0 {
			return "up"
		}
		if down == 0 && errCount > 0 {
			return "error"
		}
		if down == len(checks) {
			return "down"
		}
		return "degraded"
	},
	"barTip": func(e Entry) string {
		s := e.Timestamp.UTC().Format("15:04 UTC")
		s += fmt.Sprintf(" · %dms", int(math.Round(e.DurationMs)))
		if e.StatusCode > 0 {
			s += fmt.Sprintf(" · %d", e.StatusCode)
		}
		if e.DNSMs > 0 {
			s += fmt.Sprintf(" · dns %dms", int(math.Round(e.DNSMs)))
		}
		if e.TLSMs > 0 {
			s += fmt.Sprintf(" · tls %dms", int(math.Round(e.TLSMs)))
		}
		if e.TTFBMs > 0 {
			s += fmt.Sprintf(" · ttfb %dms", int(math.Round(e.TTFBMs)))
		}
		if e.NTPOffsetMs != 0 {
			s += fmt.Sprintf(" · offset %.1fms", e.NTPOffsetMs)
		}
		if e.CertDaysRemaining != 0 || e.CertValid {
			s += fmt.Sprintf(" · cert %dd", e.CertDaysRemaining)
			if !e.CertValid {
				s += " (invalid)"
			}
		}
		if e.ICMPPacketLoss > 0 {
			s += fmt.Sprintf(" · loss %.0f%%", e.ICMPPacketLoss)
		}
		if e.GRPCStatus != "" {
			s += " · " + e.GRPCStatus
		}
		if e.Error != "" {
			s += " · " + e.Error
		}
		return s
	},
	"fmtDate": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format("2 Jan 2006")
	},
	"certExpiryClass": func(days int) string {
		switch {
		case days <= 7:
			return "bad"
		case days <= 30:
			return "warn"
		default:
			return "good"
		}
	},
	"isoTime": func(t time.Time) string {
		return t.UTC().Format(time.RFC3339)
	},
	"groupByDomain": func(checks []CheckState) []DomainGroup {
		var order []string
		m := make(map[string]*DomainGroup)
		for _, p := range checks {
			key := p.Domain
			if key == "" {
				key = p.Name // fallback: ungrouped
			}
			if g, ok := m[key]; ok {
				g.Checks = append(g.Checks, p)
			} else {
				m[key] = &DomainGroup{Domain: key, Checks: []CheckState{p}}
				order = append(order, key)
			}
		}
		groups := make([]DomainGroup, 0, len(order))
		for _, k := range order {
			groups = append(groups, *m[k])
		}
		return groups
	},
	"domainStatus": func(checks []CheckState) string {
		down, errCount := 0, 0
		for _, p := range checks {
			if p.Status == "down" {
				down++
			} else if p.Status == "error" {
				errCount++
			}
		}
		if down > 0 {
			if down == len(checks) {
				return "down"
			}
			return "degraded"
		}
		if errCount > 0 {
			return "error"
		}
		return "up"
	},
	"domainTypes": func(checks []CheckState) string {
		seen := make(map[string]bool)
		var types []string
		for _, p := range checks {
			t := string(p.Type)
			if !seen[t] {
				seen[t] = true
				types = append(types, t)
			}
		}
		s := ""
		for i, t := range types {
			if i > 0 {
				s += ", "
			}
			s += t
		}
		return s
	},
	"checkCategory": func(t config.CheckType) string {
		return categoryForCheckType(t)
	},
	"categories": func(checks []CheckState) []CategoryInfo {
		order := []string{"Network", "Web", "Services", "Security"}
		counts := make(map[string]int)
		statusPrio := make(map[string]int)
		statusName := make(map[string]string)
		prio := map[string]int{"up": 0, "pending": 1, "error": 2, "degraded": 3, "down": 4}
		for _, p := range checks {
			cat := categoryForCheckType(p.Type)
			counts[cat]++
			if prio[p.Status] > statusPrio[cat] {
				statusPrio[cat] = prio[p.Status]
				statusName[cat] = p.Status
			}
		}
		var result []CategoryInfo
		for _, name := range order {
			if counts[name] > 0 {
				st := statusName[name]
				if st == "pending" {
					st = "up"
				}
				result = append(result, CategoryInfo{Name: name, Status: st, Count: counts[name]})
			}
		}
		return result
	},
	"numCategories": func(checks []CheckState) int {
		seen := make(map[string]bool)
		for _, p := range checks {
			seen[categoryForCheckType(p.Type)] = true
		}
		return len(seen)
	},
}).Parse(pageHTML))

// serveWithETag writes body with ETag/If-None-Match support. Returns 304 if
// the client already has the current version.
func serveWithETag(w http.ResponseWriter, r *http.Request, body []byte, contentType string) {
	hash := sha256.Sum256(body)
	etag := fmt.Sprintf(`"%x"`, hash[:8])

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", etag)

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(body)
}

// Handler returns an http.Handler that serves the status page and API.
func Handler(store *Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(store.Snapshot())
		serveWithETag(w, r, buf.Bytes(), "application/json")
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		var buf bytes.Buffer
		if err := pageTmpl.Execute(&buf, store.Snapshot()); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
		serveWithETag(w, r, buf.Bytes(), "text/html; charset=utf-8")
	})

	return mux
}
