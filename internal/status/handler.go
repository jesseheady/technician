package status

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"time"
)

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
	"probeCount": func(probes []ProbeState) int {
		return len(probes)
	},
	"hasViolation": func(checks []BudgetCheck) bool {
		for _, c := range checks {
			if c.Severity != "pass" {
				return true
			}
		}
		return false
	},
	"groupStatus": func(probes []ProbeState) string {
		down := 0
		errCount := 0
		for _, p := range probes {
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
		if down == len(probes) {
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
}).Parse(pageHTML))

// Handler returns an http.Handler that serves the status page and API.
func Handler(store *Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		json.NewEncoder(w).Encode(store.Snapshot())
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		pageTmpl.Execute(w, store.Snapshot())
	})

	return mux
}

const pageHTML = `{{define "probe-card"}}
  <div class="probe{{if hasViolation .BudgetChecks}} budget-warn{{end}}" data-type="{{.Type}}">
    <div class="probe-head">
      <div class="probe-left">
        <div class="dot {{.Status}}"></div>
        <span class="name">{{.Name}}</span>
      </div>
      <div class="probe-right">
        <span class="probe-type">{{.Type}}</span>
        <span class="uptime {{if eq .Uptime "100%"}}perfect{{else if eq .Uptime "—"}}{{else}}good{{end}}">{{.Uptime}}</span>
      </div>
    </div>
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
    <div class="probe-meta">
      {{if .Latest}}
      <span>{{relTime .Latest.Timestamp}}</span>
      {{if .Latest.StatusCode}}<span><span class="val">{{.Latest.StatusCode}}</span></span>{{end}}
      <span><span class="val">{{fmtMs .Latest.DurationMs}}</span></span>
      {{end}}
      {{if .DownSince}}<span class="down-since">{{.DownSince}}</span>{{end}}
      {{if eq .Status "error"}}<span class="error-label">probe error</span>{{end}}
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
  </div>
{{end}}<!DOCTYPE html>
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

/* type filter — radios and labels are bare siblings in body */
.type-filters{display:none}
input[name=type-filter]{display:none}
input[name=type-filter]+label{font-size:11px;font-family:var(--mono);color:var(--text-mute);background:var(--surface);border:1px solid var(--border);border-radius:4px;padding:4px 10px;cursor:pointer;transition:all .15s;user-select:none;display:inline-block;margin:0 2px 12px 0}
input[name=type-filter]+label:hover{color:var(--text-dim);border-color:var(--border-hi)}
input[name=type-filter]:checked+label{color:var(--text);border-color:var(--border-hi);background:var(--bg)}

/* filter mechanics: hide probes that don't match selected type (works through details) */
{{range .Types}}#filter-{{.}}:checked ~ .probes .probe:not([data-type="{{.}}"]){display:none}
#filter-{{.}}:checked ~ .group .probes .probe:not([data-type="{{.}}"]){display:none}
{{end}}

/* groups — collapsible via <details> */
.group{margin-bottom:16px}
.group>summary{display:flex;align-items:center;gap:8px;font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.06em;color:var(--text-dim);padding:8px 4px;cursor:pointer;list-style:none;user-select:none}
.group>summary::-webkit-details-marker{display:none}
.group>summary::before{content:"";display:inline-block;width:0;height:0;border-left:5px solid var(--text-mute);border-top:4px solid transparent;border-bottom:4px solid transparent;transition:transform .15s}
.group[open]>summary::before{transform:rotate(90deg)}
.group>summary .group-count{font-weight:400;color:var(--text-mute);font-family:var(--mono);font-size:11px}
.group>summary .group-dot{width:6px;height:6px;border-radius:50%;flex-shrink:0}

/* probe list */
.probes{display:flex;flex-direction:column;gap:1px;background:var(--border);border:1px solid var(--border);border-radius:var(--radius)}
.probe{background:var(--surface);padding:16px 20px;position:relative}
.probe:first-child{border-radius:var(--radius) var(--radius) 0 0}
.probe:last-child{border-radius:0 0 var(--radius) var(--radius)}
.probe:only-child{border-radius:var(--radius)}
.probe-head{display:flex;align-items:center;justify-content:space-between;margin-bottom:10px}
.probe-left{display:flex;align-items:center;gap:8px;font-size:13px;font-weight:500;min-width:0}
.probe-left .name{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.probe-right{display:flex;align-items:center;gap:10px;flex-shrink:0}
.probe-type{font-size:11px;color:var(--text-mute);font-family:var(--mono);background:var(--bg);padding:2px 6px;border-radius:4px}
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

/* probe meta row */
.probe-meta{display:flex;align-items:center;gap:12px;font-size:12px;color:var(--text-dim);font-family:var(--mono);margin-top:8px;flex-wrap:wrap}
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
.probe.budget-warn{border-left:3px solid var(--amber)}

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
    {{- else}}Waiting for probes…
    {{- end -}}
  </div>
</div>

{{if .Probes}}
<div class="summary" id="summary">
  <span class="stat"><span class="stat-val {{if eq .Summary.Down 0}}good{{else}}bad{{end}}">{{.Summary.Up}}/{{.Summary.Total}}</span> probes up</span>
  {{if gt .Summary.Down 0}}<span class="sep">&middot;</span>
  <span class="stat"><span class="stat-val bad">{{.Summary.Down}}</span> down</span>{{end}}
  {{if gt .Summary.Error 0}}<span class="sep">&middot;</span>
  <span class="stat"><span class="stat-val warn">{{.Summary.Error}}</span> errors</span>{{end}}
  {{if gt .Summary.BudgetTotal 0}}<span class="sep">&middot;</span>
  <span class="stat">Budgets: <span class="stat-val {{if eq .Summary.BudgetViolations 0}}good{{else if gt .Summary.BudgetViolations 2}}bad{{else}}warn{{end}}">{{sub .Summary.BudgetTotal .Summary.BudgetViolations}}/{{.Summary.BudgetTotal}}</span> passing</span>{{end}}
</div>

{{if gt (len .Types) 1}}
<input type="radio" name="type-filter" id="filter-all" checked>
<label for="filter-all">all</label>
{{range .Types}}
<input type="radio" name="type-filter" id="filter-{{.}}">
<label for="filter-{{.}}">{{.}}</label>
{{end}}
{{end}}

{{if gt (len .Groups) 1}}
  {{range $i, $g := .Groups}}
  <details class="group" data-group="{{if .Name}}{{.Name}}{{else}}General{{end}}">
    <summary>
      <span class="group-dot dot {{groupStatus .Probes}}"></span>
      {{if .Name}}{{.Name}}{{else}}General{{end}}
      <span class="group-count">{{probeCount .Probes}}</span>
    </summary>
    <div class="probes">
      {{range .Probes}}{{template "probe-card" .}}{{end}}
    </div>
  </details>
  {{end}}
{{else}}
<div class="probes">
  {{range .Probes}}{{template "probe-card" .}}{{end}}
</div>
{{end}}
{{else}}
<div class="empty">No probes have reported yet.</div>
{{end}}

<div class="footer">
  Powered by <a href="https://github.com/jesseheady/technician">Technician</a>
</div>

<script>
(function(){
  var KEY='techst';
  // Restore open/closed state from localStorage
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
  // Augment tooltips with local time
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
  // Auto-refresh via fetch (no full page reload)
  setInterval(function(){
    fetch(location.href,{headers:{'Accept':'text/html'}}).then(function(r){return r.text()}).then(function(html){
      var doc=new DOMParser().parseFromString(html,'text/html');
      // Preserve details open state
      var states={};
      document.querySelectorAll('details[data-group]').forEach(function(d){
        states[d.getAttribute('data-group')]=d.open;
      });
      // Update banner + summary
      var nb=doc.querySelector('.banner'), ob=document.querySelector('.banner');
      if(nb&&ob) ob.replaceWith(nb);
      var ns2=doc.getElementById('summary'), os2=document.getElementById('summary');
      if(ns2&&os2) os2.replaceWith(ns2);
      // Update probe groups/list
      var groups=document.querySelectorAll('.group');
      var newGroups=doc.querySelectorAll('.group');
      if(groups.length&&newGroups.length===groups.length){
        groups.forEach(function(g,i){
          var ng=newGroups[i];
          var gName=g.getAttribute('data-group');
          // Update probes content but keep open state
          var op=g.querySelector('.probes'), np=ng.querySelector('.probes');
          if(op&&np) op.innerHTML=np.innerHTML;
          // Update summary (status dot + count)
          var os=g.querySelector('summary'), ns=ng.querySelector('summary');
          if(os&&ns) os.innerHTML=ns.innerHTML;
          g.open=gName in states?states[gName]:(i===0);
        });
      } else {
        // Fallback: replace entire content area
        var flat=document.querySelector('.probes:not(.group .probes)');
        var nf=doc.querySelector('.probes:not(.group .probes)');
        if(flat&&nf) flat.innerHTML=nf.innerHTML;
      }
    }).catch(function(){});
  },10000);
})();
</script>
</body>
</html>`
