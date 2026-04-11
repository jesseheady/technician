package status

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"time"

	"github.com/m0nkey/technician/internal/config"
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
	case config.CheckTypeGRPC, config.CheckTypeSMTP, config.CheckTypeNTP:
		return "Services"
	case config.CheckTypeTLS, config.CheckTypeBGP, config.CheckTypeDomainExpiry:
		return "Security"
	default:
		return "Other"
	}
}

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
		json.NewEncoder(&buf).Encode(store.Snapshot())
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

const pageHTML = `{{define "check-card"}}
  <details class="check{{if hasViolation .BudgetChecks}} budget-warn{{end}}" data-type="{{.Type}}" data-category="{{checkCategory .Type}}" data-name="{{.Name}}"{{if ne .Status "up"}} open{{end}}>
    <summary class="check-head">
      <div class="check-left">
        <div class="dot {{.Status}}"></div>
        <span class="name">{{.Name}}</span>
      </div>
      <div class="check-right">
        <span class="check-type">{{.Type}}</span>
        <span class="uptime {{if eq .Uptime "100%"}}perfect{{else if eq .Uptime "—"}}{{else}}good{{end}}">{{.Uptime}}</span>
      </div>
    </summary>
    {{if .History}}
    {{$max := maxDuration .History}}
    <div class="bars">
      {{range .History}}
      <div class="bar {{if .Success}}up{{else if .InfraError}}error{{else}}down{{end}}" style="height:{{barHeight .DurationMs $max}}px" data-tip="{{barTip .}}" data-ts="{{isoTime .Timestamp}}"></div>
      {{end}}
    </div>
    {{end}}
    {{if .Latency}}
    <div class="percentiles">
      <span>p50 <span class="val">{{fmtMs .Latency.P50}}</span></span>
      <span>p90 <span class="val">{{fmtMs .Latency.P90}}</span></span>
      <span>p95 <span class="val">{{fmtMs .Latency.P95}}</span></span>
      <span>p99 <span class="val">{{fmtMs .Latency.P99}}</span></span>
    </div>
    {{end}}
    {{if .Timing}}
    <div class="timing-legend">
      {{if .Timing.DNSMs}}<span><span class="legend-dot dns"></span>dns {{fmtMs .Timing.DNSMs}}</span>{{end}}
      {{if .Timing.TLSMs}}<span><span class="legend-dot tls"></span>tls {{fmtMs .Timing.TLSMs}}</span>{{end}}
      <span><span class="legend-dot ttfb"></span>ttfb {{fmtMs .Timing.TTFBMs}}</span>
      {{if .Timing.TransferMs}}<span><span class="legend-dot transfer"></span>transfer {{fmtMs .Timing.TransferMs}}</span>{{end}}
    </div>
    {{end}}
    <div class="check-meta">
      {{if .Latest}}
      <span>{{relTime .Latest.Timestamp}}</span>
      {{if .Latest.StatusCode}}<span><span class="val">{{.Latest.StatusCode}}</span></span>{{end}}
      <span><span class="val">{{fmtMs .Latest.DurationMs}}</span></span>
      {{end}}
      {{if .DownSince}}<span class="down-since">{{.DownSince}}</span>{{end}}
      {{if eq .Status "error"}}<span class="error-label">check error</span>{{end}}
    </div>
    {{if .Latest}}
    {{if and (eq .Type "tls") (not .Latest.CertExpiry.IsZero)}}
    <div class="type-meta">
      <span class="meta-badge {{certExpiryClass .Latest.CertDaysRemaining}}">expires {{fmtDate .Latest.CertExpiry}} ({{.Latest.CertDaysRemaining}}d)</span>
      {{if .Latest.CertValid}}<span class="meta-badge good">chain valid</span>{{else}}<span class="meta-badge bad">chain invalid</span>{{end}}
    </div>
    {{end}}
    {{if and (eq .Type "icmp") (ne .Latest.ICMPPacketLoss 0.0)}}
    <div class="type-meta">
      <span class="meta-badge {{if ge .Latest.ICMPPacketLoss 20.0}}bad{{else if ge .Latest.ICMPPacketLoss 5.0}}warn{{else}}good{{end}}">loss {{printf "%.0f" .Latest.ICMPPacketLoss}}%</span>
    </div>
    {{end}}
    {{if and (eq .Type "ntp") (ne .Latest.NTPOffsetMs 0.0)}}
    <div class="type-meta">
      <span class="meta-badge {{if gt .Latest.NTPOffsetMs 100.0}}warn{{else if lt .Latest.NTPOffsetMs -100.0}}warn{{else}}good{{end}}">offset {{printf "%.1f" .Latest.NTPOffsetMs}}ms</span>
    </div>
    {{end}}
    {{if and (eq .Type "grpc") (ne .Latest.GRPCStatus "")}}
    <div class="type-meta">
      <span class="meta-badge {{if eq .Latest.GRPCStatus "SERVING"}}good{{else}}bad{{end}}">{{.Latest.GRPCStatus}}</span>
    </div>
    {{end}}
    {{end}}
    {{if .BudgetChecks}}
    <div class="budget-row">
      {{range .BudgetChecks}}<span class="budget-badge {{.Severity}}">{{.Metric}}</span>{{end}}
    </div>
    {{end}}
  </details>
{{end}}{{define "domain-checks"}}{{$domains := groupByDomain .}}{{range $domains}}{{if eq (len .Checks) 1}}{{template "check-card" (index .Checks 0)}}{{else}}<details class="domain-wrap" data-domain="{{.Domain}}"{{if ne (domainStatus .Checks) "up"}} open{{end}}>
    <summary class="domain-head">
      <div class="dot {{domainStatus .Checks}}"></div>
      <span class="domain-name">{{.Domain}}</span>
      <span class="domain-info">{{len .Checks}} checks · {{domainTypes .Checks}}</span>
    </summary>
    <div class="domain-checks">
      {{range .Checks}}{{template "check-card" .}}{{end}}
    </div>
  </details>{{end}}{{end}}{{end}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if .Service}}{{.Service}}{{else}}Technician{{end}} — Status</title>
<style>
*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
:root{
  --bg:#0a0a0b;
  --surface:#111113;
  --border:#1e1e22;
  --border-hi:#2a2a30;
  --text:#ededef;
  --text-dim:#7a7a82;
  --text-mute:#4a4a52;
  --green:#00cc88;
  --green-dim:rgba(0,204,136,.12);
  --amber:#f5a623;
  --amber-dim:rgba(245,166,35,.12);
  --red:#ef4444;
  --red-dim:rgba(239,68,68,.12);
  --radius:8px;
  --font:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;
  --mono:"SF Mono",SFMono-Regular,ui-monospace,"DejaVu Sans Mono",Menlo,Consolas,monospace;
}
html{background:var(--bg);color:var(--text);font-family:var(--font);font-size:14px;-webkit-font-smoothing:antialiased}
body{max-width:720px;margin:0 auto;padding:48px 20px 80px}
a{color:var(--text-dim);text-decoration:none;transition:color .15s}
a:hover{color:var(--text)}

/* header */
.header{display:flex;align-items:center;justify-content:space-between;margin-bottom:40px}
.header h1{font-size:16px;font-weight:600;letter-spacing:-.01em}
.header h1 span{color:var(--text-dim);font-weight:400}
.site-badge{display:inline-flex;align-items:center;gap:6px;font-size:11px;font-family:var(--mono);color:var(--text-dim);background:var(--surface);border:1px solid var(--border);border-radius:4px;padding:3px 8px;margin-left:10px;vertical-align:middle}
.links{display:flex;gap:16px;font-size:12px}

/* banner */
.banner{border:1px solid var(--border);border-radius:var(--radius);padding:16px 20px;margin-bottom:12px;display:flex;align-items:center;gap:12px}
.banner.operational{border-color:color-mix(in srgb,var(--green) 30%,var(--border));background:var(--green-dim)}
.banner.degraded{border-color:color-mix(in srgb,var(--amber) 30%,var(--border));background:var(--amber-dim)}
.banner.down{border-color:color-mix(in srgb,var(--red) 30%,var(--border));background:var(--red-dim)}
.banner.pending{border-color:var(--border);background:var(--surface)}
.dot{width:8px;height:8px;border-radius:50%;flex-shrink:0}
.dot.up,.dot.operational{background:var(--green);box-shadow:0 0 6px var(--green)}
.dot.degraded,.dot.error{background:var(--amber);box-shadow:0 0 6px var(--amber)}
.dot.down{background:var(--red);box-shadow:0 0 6px var(--red)}
.dot.pending{background:var(--text-mute)}
.banner-text{font-size:13px;font-weight:500}

/* summary strip */
.summary{display:flex;align-items:center;gap:6px;flex-wrap:wrap;font-size:12px;font-family:var(--mono);color:var(--text-dim);padding:0 4px;margin-bottom:20px}
.summary .sep{color:var(--text-mute)}
.summary .stat{display:inline-flex;align-items:center;gap:4px}
.summary .stat-val{color:var(--text);font-weight:500}
.summary .stat-val.warn{color:var(--amber)}
.summary .stat-val.bad{color:var(--red)}
.summary .stat-val.good{color:var(--green)}

/* tabs */
.controls{margin-bottom:16px}
.tabs{display:flex;gap:4px;flex-wrap:wrap;margin-bottom:8px;overflow-x:auto;-webkit-overflow-scrolling:touch}
.tab{font-size:12px;font-family:var(--mono);color:var(--text-mute);background:var(--surface);border:1px solid var(--border);border-radius:6px;padding:6px 12px;cursor:pointer;transition:all .15s;user-select:none;display:inline-flex;align-items:center;gap:6px;white-space:nowrap}
.tab:hover{color:var(--text-dim);border-color:var(--border-hi)}
.tab.active{color:var(--text);border-color:var(--border-hi);background:var(--bg)}
.tab .dot{width:6px;height:6px}
.tab-count{font-size:10px;color:var(--text-mute);background:var(--bg);padding:1px 5px;border-radius:3px}
.tab.active .tab-count{background:var(--surface)}
.search-row{display:flex;gap:6px;align-items:center;flex-wrap:wrap}
.search-input{flex:1;min-width:140px;font-size:12px;font-family:var(--mono);color:var(--text);background:var(--surface);border:1px solid var(--border);border-radius:6px;padding:6px 12px;outline:none;transition:border-color .15s}
.search-input:focus{border-color:var(--border-hi)}
.search-input::placeholder{color:var(--text-mute)}
.filter-hidden{display:none!important}

/* groups — collapsible via <details> */
.group{margin-bottom:16px}
.group>summary{display:flex;align-items:center;gap:8px;font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.06em;color:var(--text-dim);padding:8px 4px;cursor:pointer;list-style:none;user-select:none}
.group>summary::-webkit-details-marker{display:none}
.group>summary::before{content:"";display:inline-block;width:0;height:0;border-left:5px solid var(--text-mute);border-top:4px solid transparent;border-bottom:4px solid transparent;transition:transform .15s}
.group[open]>summary::before{transform:rotate(90deg)}
.group>summary .group-count{font-weight:400;color:var(--text-mute);font-family:var(--mono);font-size:11px}
.group>summary .group-dot{width:6px;height:6px;border-radius:50%;flex-shrink:0}

/* check list */
.checks{display:flex;flex-direction:column;gap:1px;background:var(--border);border:1px solid var(--border);border-radius:var(--radius)}
.check{background:var(--surface);padding:16px 20px;position:relative}
.check>summary{cursor:pointer;list-style:none;user-select:none;border-radius:inherit;transition:background .15s}
.check>summary::-webkit-details-marker{display:none}
.check>summary:hover{background:color-mix(in srgb,var(--border) 30%,var(--surface))}
.check-left::before{content:"";display:inline-block;width:0;height:0;border-left:4px solid var(--text-mute);border-top:3px solid transparent;border-bottom:3px solid transparent;transition:transform .15s;flex-shrink:0;opacity:.35}
.check[open] .check-left::before{transform:rotate(90deg);opacity:.55}
.check:first-child,.domain-wrap:first-child{border-radius:var(--radius) var(--radius) 0 0}
.check:last-child,.domain-wrap:last-child{border-radius:0 0 var(--radius) var(--radius)}
.check:only-child,.domain-wrap:only-child{border-radius:var(--radius)}

/* domain grouping within check list */
.domain-wrap{background:var(--surface);position:relative}
.domain-wrap>summary{display:flex;align-items:center;gap:8px;padding:12px 20px;cursor:pointer;list-style:none;user-select:none;font-size:13px}
.domain-wrap>summary::-webkit-details-marker{display:none}
.domain-wrap>summary::before{content:"";display:inline-block;width:0;height:0;border-left:4px solid var(--text-mute);border-top:3px solid transparent;border-bottom:3px solid transparent;transition:transform .15s;flex-shrink:0}
.domain-wrap[open]>summary::before{transform:rotate(90deg)}
.domain-name{font-weight:500;color:var(--text)}
.domain-info{font-size:11px;font-family:var(--mono);color:var(--text-mute)}
.domain-checks{border-top:1px solid var(--border)}
.domain-checks .check{padding:12px 20px 12px 36px}
.domain-checks .check:last-child{border-radius:0}
.domain-wrap:last-child .domain-checks .check:last-child{border-radius:0 0 var(--radius) var(--radius)}

/* controls */
.toggle-btn{font-size:11px;font-family:var(--mono);color:var(--text-mute);background:var(--surface);border:1px solid var(--border);border-radius:4px;padding:6px 10px;cursor:pointer;transition:all .15s;user-select:none}
.toggle-btn:hover{color:var(--text-dim);border-color:var(--border-hi)}
.toggle-btn.active{color:var(--text);border-color:var(--border-hi);background:var(--bg)}
.check-head{display:flex;align-items:center;justify-content:space-between}
.check[open]>.check-head{margin-bottom:10px}
.check-left{display:flex;align-items:center;gap:8px;font-size:13px;font-weight:500;min-width:0}
.check-left .name{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.check-right{display:flex;align-items:center;gap:10px;flex-shrink:0}
.check-type{font-size:11px;color:var(--text-mute);font-family:var(--mono);background:var(--bg);padding:2px 6px;border-radius:4px}
.uptime{font-size:12px;font-family:var(--mono);font-weight:500}
.uptime.perfect{color:var(--green)}
.uptime.good{color:var(--green)}
.uptime.warn{color:var(--amber)}
.uptime.bad{color:var(--red)}

/* bars */
.bars{display:flex;align-items:flex-end;gap:1px;height:32px;overflow:visible}
.bar{flex:1;min-width:2px;max-width:6px;border-radius:1px 1px 0 0;position:relative;overflow:visible}
.bar.up{background:var(--green)}
.bar.down{background:var(--red)}
.bar.error{background:var(--amber)}
.bar::after{content:attr(data-tip);display:none;position:absolute;bottom:calc(100% + 6px);left:50%;transform:translateX(-50%);background:#1a1a1e;border:1px solid var(--border-hi);border-radius:6px;padding:6px 10px;font-size:11px;font-family:var(--mono);white-space:nowrap;z-index:10;pointer-events:none;color:var(--text)}
.bar:hover::after{display:block}

/* check meta row */
.check-meta{display:flex;align-items:center;gap:12px;font-size:12px;color:var(--text-dim);font-family:var(--mono);margin-top:8px;flex-wrap:wrap}
.val{color:var(--text)}
.down-since{color:var(--red);font-weight:500}
.error-label{color:var(--amber);font-weight:500}

/* percentiles */
.percentiles{display:flex;align-items:center;gap:12px;font-size:12px;color:var(--text-dim);font-family:var(--mono);margin-top:8px}

/* timing legend */
.timing-legend{display:flex;align-items:center;gap:12px;font-size:11px;color:var(--text-dim);font-family:var(--mono);margin-top:4px}
.legend-dot{display:inline-block;width:8px;height:8px;border-radius:2px;margin-right:3px;vertical-align:middle}
.legend-dot.dns{background:#6366f1}
.legend-dot.tls{background:#a855f7}
.legend-dot.ttfb{background:#ec4899}
.legend-dot.transfer{background:#f59e0b}

/* budget badges */
.budget-row{display:flex;align-items:center;gap:6px;flex-wrap:wrap;margin-top:6px}
.budget-badge{font-size:10px;font-family:var(--mono);padding:2px 6px;border-radius:3px;display:inline-flex;align-items:center;gap:3px}
.budget-badge.pass{color:var(--green);background:var(--green-dim)}
.budget-badge.warn{color:var(--amber);background:var(--amber-dim);font-weight:500}
.budget-badge.fail{color:var(--red);background:var(--red-dim);font-weight:500}
.check.budget-warn{border-left:3px solid var(--amber)}

/* type-specific metadata */
.type-meta{display:flex;align-items:center;gap:6px;flex-wrap:wrap;margin-top:6px}
.meta-badge{font-size:11px;font-family:var(--mono);padding:2px 8px;border-radius:3px;display:inline-flex;align-items:center}
.meta-badge.good{color:var(--green);background:var(--green-dim)}
.meta-badge.warn{color:var(--amber);background:var(--amber-dim)}
.meta-badge.bad{color:var(--red);background:var(--red-dim)}

.empty{text-align:center;padding:40px 20px;color:var(--text-dim);font-size:13px}
.footer{margin-top:40px;text-align:center;font-size:11px;color:var(--text-mute)}
</style>
</head>
<body>

<div class="header">
  <h1>
    {{if .Service}}{{.Service}}{{else}}Technician{{end}} <span>Status</span>
    {{if .Site}}<span class="site-badge">{{.Site.City}}, {{.Site.Country}} · {{.Site.Code}}</span>{{end}}
  </h1>
  <div class="links">
    <a href="/metrics">/metrics</a>
    <a href="/health">/health</a>
    <a href="/api/status">API</a>
  </div>
</div>

<div class="banner {{.Overall}}">
  <div class="dot {{.Overall}}"></div>
  <div class="banner-text">
    {{- if eq .Overall "operational"}}All systems operational
    {{- else if eq .Overall "degraded"}}Partial degradation
    {{- else if eq .Overall "down"}}Major outage
    {{- else}}Waiting for checks…
    {{- end -}}
  </div>
</div>

{{if .Checks}}
<div class="summary" id="summary">
  <span class="stat"><span class="stat-val {{if eq .Summary.Down 0}}good{{else}}bad{{end}}">{{.Summary.Up}}/{{.Summary.Total}}</span> checks up</span>
  {{if gt .Summary.Down 0}}<span class="sep">&middot;</span>
  <span class="stat"><span class="stat-val bad">{{.Summary.Down}}</span> down</span>{{end}}
  {{if gt .Summary.Error 0}}<span class="sep">&middot;</span>
  <span class="stat"><span class="stat-val warn">{{.Summary.Error}}</span> errors</span>{{end}}
  {{if gt .Summary.BudgetTotal 0}}<span class="sep">&middot;</span>
  <span class="stat">Budgets: <span class="stat-val {{if eq .Summary.BudgetViolations 0}}good{{else if gt .Summary.BudgetViolations 2}}bad{{else}}warn{{end}}">{{sub .Summary.BudgetTotal .Summary.BudgetViolations}}/{{.Summary.BudgetTotal}}</span> passing</span>{{end}}
</div>

{{if gt (numCategories .Checks) 1}}
<div class="controls">
<nav class="tabs" id="tabs">
  <button class="tab active" data-cat="all">All <span class="tab-count">{{len .Checks}}</span></button>
  {{range categories .Checks}}<button class="tab" data-cat="{{.Name}}"><span class="dot {{.Status}}"></span>{{.Name}} <span class="tab-count">{{.Count}}</span></button>{{end}}
</nav>
<div class="search-row">
  <input type="text" class="search-input" id="search" placeholder="Filter checks…" autocomplete="off">
  <button class="toggle-btn" id="issues-only">issues only</button>
  <button class="toggle-btn" id="toggle-all" title="Expand or collapse all">expand all</button>
</div>
</div>
{{else}}
<div class="controls">
<div class="search-row">
  <input type="text" class="search-input" id="search" placeholder="Filter checks…" autocomplete="off">
  <button class="toggle-btn" id="issues-only">issues only</button>
  <button class="toggle-btn" id="toggle-all" title="Expand or collapse all">expand all</button>
</div>
</div>
{{end}}

{{if gt (len .Groups) 1}}
  {{range $i, $g := .Groups}}
  <details class="group" data-group="{{if .Name}}{{.Name}}{{else}}General{{end}}">
    <summary>
      <span class="group-dot dot {{groupStatus .Checks}}"></span>
      {{if .Name}}{{.Name}}{{else}}General{{end}}
      <span class="group-count">{{checkCount .Checks}}</span>
    </summary>
    <div class="checks">
      {{template "domain-checks" .Checks}}
    </div>
  </details>
  {{end}}
{{else}}
<div class="checks">
  {{template "domain-checks" .Checks}}
</div>
{{end}}
{{else}}
<div class="empty">No checks have reported yet.</div>
{{end}}

<div class="footer">
  Powered by <a href="https://github.com/m0nkey/technician">Technician</a>
</div>

<script>
(function(){
  var KEY='techst';
  var DKEY='techst-d';
  var PKEY='techst-p';
  var TKEY='techst-tab';

  // ── Category filter mapping ──
  var catTypes={
    'Network':['icmp','tcp','udp','dns','traceroute'],
    'Web':['http','playwright'],
    'Services':['grpc','smtp','ntp'],
    'Security':['tls','bgp','domain_expiry']
  };
  var activeTab=localStorage.getItem(TKEY)||'all';
  var issuesActive=false;

  // ── Filtering engine ──
  function applyFilters(){
    var search=(document.getElementById('search')||{}).value||'';
    search=search.toLowerCase();
    document.querySelectorAll('.check[data-name]').forEach(function(p){
      var type=p.getAttribute('data-type');
      var name=(p.getAttribute('data-name')||'').toLowerCase();
      var dot=p.querySelector('.check-head .dot');
      var st=dot?dot.className:'';
      var catMatch=activeTab==='all'||(catTypes[activeTab]&&catTypes[activeTab].indexOf(type)!==-1);
      var searchMatch=!search||name.indexOf(search)!==-1;
      var issueMatch=!issuesActive||/down|error|degraded/.test(st);
      if(catMatch&&searchMatch&&issueMatch) p.classList.remove('filter-hidden');
      else p.classList.add('filter-hidden');
    });
    // Hide empty domain wraps
    document.querySelectorAll('.domain-wrap').forEach(function(dw){
      dw.classList.toggle('filter-hidden',!dw.querySelector('.check[data-name]:not(.filter-hidden)'));
    });
    // Hide empty groups
    document.querySelectorAll('.group').forEach(function(g){
      g.classList.toggle('filter-hidden',!g.querySelector('.check[data-name]:not(.filter-hidden)'));
    });
    // Update empty state
    document.querySelectorAll('.checks').forEach(function(container){
      var anyVisible=container.querySelector('.check[data-name]:not(.filter-hidden)');
      container.style.display=anyVisible?'':'none';
    });
  }

  // ── Tab switching ──
  var tabs=document.querySelectorAll('.tab[data-cat]');
  tabs.forEach(function(tab){
    tab.classList.toggle('active',tab.getAttribute('data-cat')===activeTab);
    tab.addEventListener('click',function(){
      tabs.forEach(function(t){t.classList.remove('active');});
      tab.classList.add('active');
      activeTab=tab.getAttribute('data-cat');
      localStorage.setItem(TKEY,activeTab);
      applyFilters();
    });
  });

  // ── Search ──
  var searchInput=document.getElementById('search');
  if(searchInput) searchInput.addEventListener('input',applyFilters);

  // ── Issues only toggle ──
  var issuesBtn=document.getElementById('issues-only');
  if(issuesBtn) issuesBtn.addEventListener('click',function(){
    issuesActive=!issuesActive;
    issuesBtn.classList.toggle('active',issuesActive);
    applyFilters();
  });

  // ── Expand/collapse all (respects filters) ──
  var toggleBtn=document.getElementById('toggle-all');
  if(toggleBtn){
    toggleBtn.addEventListener('click',function(){
      var all=document.querySelectorAll('details.group:not(.filter-hidden),.domain-wrap:not(.filter-hidden),details.check:not(.filter-hidden)');
      var allOpen=true;
      all.forEach(function(d){if(!d.open)allOpen=false;});
      var next=!allOpen;
      all.forEach(function(d){d.open=next;});
      toggleBtn.textContent=next?'collapse all':'expand all';
      var s={};
      document.querySelectorAll('details[data-group]').forEach(function(d){
        s[d.getAttribute('data-group')]=d.open;
      });
      localStorage.setItem(KEY,JSON.stringify(s));
    });
  }

  // ── Restore group open/closed state ──
  var saved=JSON.parse(localStorage.getItem(KEY)||'{}');
  document.querySelectorAll('details[data-group]').forEach(function(d,i){
    var g=d.getAttribute('data-group');
    if(g in saved) { if(saved[g]) d.open=true; else d.open=false; }
    else if(i===0) d.open=true;
    d.addEventListener('toggle',function(){
      var s=JSON.parse(localStorage.getItem(KEY)||'{}');
      s[d.getAttribute('data-group')]=d.open;
      localStorage.setItem(KEY,JSON.stringify(s));
    });
  });

  // ── Restore domain-wrap and check card states ──
  var dSaved=JSON.parse(localStorage.getItem(DKEY)||'{}');
  document.querySelectorAll('.domain-wrap[data-domain]').forEach(function(d){
    var g=d.closest('[data-group]');
    var k=(g?g.getAttribute('data-group'):'')+'\0'+d.getAttribute('data-domain');
    if(k in dSaved) d.open=dSaved[k];
  });
  var pSaved=JSON.parse(localStorage.getItem(PKEY)||'{}');
  document.querySelectorAll('.check[data-name]').forEach(function(d){
    var nm=d.getAttribute('data-name');
    if(nm in pSaved) d.open=pSaved[nm];
  });

  // ── Persist domain-wrap and check card toggles ──
  document.addEventListener('toggle',function(e){
    var t=e.target;
    if(!t.matches) return;
    if(t.matches('.domain-wrap[data-domain]')){
      var g=t.closest('[data-group]');
      var k=(g?g.getAttribute('data-group'):'')+'\0'+t.getAttribute('data-domain');
      var s=JSON.parse(localStorage.getItem(DKEY)||'{}');
      s[k]=t.open;
      localStorage.setItem(DKEY,JSON.stringify(s));
    } else if(t.matches('.check[data-name]')){
      var s=JSON.parse(localStorage.getItem(PKEY)||'{}');
      s[t.getAttribute('data-name')]=t.open;
      localStorage.setItem(PKEY,JSON.stringify(s));
    }
  },true);

  // ── Tooltip local time ──
  function addLocalTime(bar){
    if(bar.dataset.done) return;
    var ts=bar.dataset.ts;
    if(!ts) return;
    var d=new Date(ts);
    var local=d.toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'});
    bar.dataset.tip=bar.dataset.tip.replace(/^\d{2}:\d{2} UTC/,function(m){return m+' / '+local+' local'});
    bar.dataset.done='1';
  }
  document.querySelectorAll('.bar[data-ts]').forEach(addLocalTime);
  document.addEventListener('mouseover',function(e){
    var bar=e.target.closest('.bar[data-ts]');
    if(bar) addLocalTime(bar);
  });

  // ── Auto-refresh via JSON API ──
  setInterval(function(){
    fetch('/api/status').then(function(r){return r.json()}).then(function(data){
      var banner=document.querySelector('.banner');
      if(banner){
        banner.className='banner '+data.overall;
        var dot=banner.querySelector('.dot');
        if(dot) dot.className='dot '+data.overall;
        var txt=banner.querySelector('.banner-text');
        if(txt){
          var labels={operational:'All systems operational',degraded:'Partial degradation',down:'Major outage',pending:'Waiting for checks\u2026'};
          txt.textContent=labels[data.overall]||data.overall;
        }
      }
      var sum=document.getElementById('summary');
      if(sum&&data.summary){
        var s=data.summary;
        sum.innerHTML='<span class="stat"><span class="stat-val '+(s.down===0?'good':'bad')+'">'+s.up+'/'+s.total+'</span> checks up</span>'
          +(s.down>0?' <span class="sep">&middot;</span> <span class="stat"><span class="stat-val bad">'+s.down+'</span> down</span>':'')
          +(s.error>0?' <span class="sep">&middot;</span> <span class="stat"><span class="stat-val warn">'+s.error+'</span> errors</span>':'')
          +(s.budget_total>0?' <span class="sep">&middot;</span> <span class="stat">Budgets: <span class="stat-val '+(s.budget_violations===0?'good':s.budget_violations>2?'bad':'warn')+'">'+(s.budget_total-s.budget_violations)+'/'+s.budget_total+'</span> passing</span>':'');
        }
      if(data.checks){
        data.checks.forEach(function(p){
          var card=document.querySelector('.check[data-name="'+p.name+'"]');
          if(!card) return;
          var dot=card.querySelector('.check-head .dot');
          if(dot) dot.className='dot '+p.status;
          var uptime=card.querySelector('.uptime');
          if(uptime) uptime.textContent=p.uptime;
        });
      }
      applyFilters();
    }).catch(function(){});
  },10000);

  // ── Apply initial filters ──
  applyFilters();
})();
</script>
</body>
</html>`
