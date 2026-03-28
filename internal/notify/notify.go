package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/m0nkey/technician/internal/config"
	"github.com/m0nkey/technician/internal/probe"
)

// EventType identifies the kind of notification.
type EventType string

const (
	EventProbeDown       EventType = "probe_down"
	EventProbeUp         EventType = "probe_up"
	EventBudgetViolation EventType = "budget_violation"
	EventCertExpiring    EventType = "cert_expiring"
)

// Severity classifies how urgent a notification is.
type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Event is a single notification payload.
type Event struct {
	Type      EventType
	Severity  Severity
	Probe     string
	ProbeType config.ProbeType
	Message   string
	Details   map[string]string
	Timestamp time.Time
}

// Sender delivers an event to an external service.
type Sender interface {
	Send(ctx context.Context, event Event) error
}

type webhook struct {
	sender     Sender
	events     map[EventType]bool
	severities map[Severity]bool // nil = all severities
	cooldown   time.Duration
}

// Manager tracks probe state transitions and dispatches notifications
// to configured webhook endpoints.
type Manager struct {
	webhooks []webhook
	wg       sync.WaitGroup
	sem      chan struct{} // limits concurrent outbound webhook sends

	mu           sync.Mutex
	probeStates  map[string]bool      // probe key -> last success (for recovery detection)
	failCounts   map[string]int       // probe key -> consecutive failure count
	notifiedDown map[string]bool      // probe key -> true if down notification already sent
	budgetStates map[string]bool      // "probe:metric" -> last violated
	certStates   map[string]Severity  // "cert:probeName" -> last notified severity
	lastSent     map[string]time.Time // "probe:eventType" -> last sent
}

const maxConcurrentSends = 4

// consecutiveFailThreshold is the number of consecutive failures required
// before a probe_down notification is dispatched. This prevents transient
// blips from triggering alerts.
const consecutiveFailThreshold = 3

// NewManager creates a Manager from the webhook configs. Returns nil
// if no webhooks are configured (callers can safely call nil Manager methods).
func NewManager(cfgWebhooks []config.WebhookConfig) *Manager {
	if len(cfgWebhooks) == 0 {
		return nil
	}

	m := &Manager{
		sem:          make(chan struct{}, maxConcurrentSends),
		probeStates:  make(map[string]bool),
		failCounts:   make(map[string]int),
		notifiedDown: make(map[string]bool),
		budgetStates: make(map[string]bool),
		certStates:   make(map[string]Severity),
		lastSent:     make(map[string]time.Time),
	}

	for _, wc := range cfgWebhooks {
		var s Sender
		switch wc.Type {
		case "discord":
			s = NewDiscordSender(wc.URL)
		case "slack":
			s = NewSlackSender(wc.URL)
		default:
			s = NewGenericSender(wc.URL)
		}

		events := make(map[EventType]bool)
		if len(wc.Events) == 0 {
			// Default: all events
			events[EventProbeDown] = true
			events[EventProbeUp] = true
			events[EventBudgetViolation] = true
			events[EventCertExpiring] = true
		} else {
			for _, e := range wc.Events {
				events[EventType(e)] = true
			}
		}

		cooldown := 5 * time.Minute
		if wc.Cooldown > 0 {
			cooldown = wc.Cooldown
		}

		var severities map[Severity]bool
		if len(wc.Severities) > 0 {
			severities = make(map[Severity]bool)
			for _, sev := range wc.Severities {
				severities[Severity(sev)] = true
			}
		}

		m.webhooks = append(m.webhooks, webhook{sender: s, events: events, severities: severities, cooldown: cooldown})
	}

	slog.Info("Webhook notifications enabled", "endpoints", len(m.webhooks))
	return m
}

// HandleResult uses consecutive-failure counting to debounce probe
// state transitions. A probe_down notification requires consecutiveFailThreshold
// consecutive failures; probe_up fires immediately on recovery.
// Safe to call on a nil Manager.
func (m *Manager) HandleResult(ctx context.Context, result *probe.Result) {
	if m == nil {
		return
	}

	// Infra errors are transient — don't trigger state transitions
	if result.InfraError {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := string(result.Type) + ":" + result.Name
	_, seen := m.probeStates[key]
	m.probeStates[key] = result.Success

	if !seen {
		return // first result — establish baseline, don't notify
	}

	var event *Event

	if result.Success {
		// Reset failure counter on success
		wasDown := m.notifiedDown[key]
		m.failCounts[key] = 0
		delete(m.notifiedDown, key)

		// Only send recovery if we previously notified about this probe being down
		if wasDown {
			event = &Event{
				Type:      EventProbeUp,
				Probe:     result.Name,
				ProbeType: result.Type,
				Message:   fmt.Sprintf("Probe %s is back up", result.Name),
				Timestamp: result.Timestamp,
			}
		}
	} else {
		m.failCounts[key]++
		count := m.failCounts[key]

		slog.Debug("Probe failure recorded", "probe", result.Name, "consecutive", count, "threshold", consecutiveFailThreshold)

		if count == consecutiveFailThreshold && !m.notifiedDown[key] {
			m.notifiedDown[key] = true
			details := make(map[string]string)
			if result.Error != "" {
				details["error"] = result.Error
			}
			if result.StatusCode > 0 {
				details["status_code"] = fmt.Sprintf("%d", result.StatusCode)
			}
			details["consecutive_failures"] = fmt.Sprintf("%d", count)
			event = &Event{
				Type:      EventProbeDown,
				Severity:  SeverityCritical,
				Probe:     result.Name,
				ProbeType: result.Type,
				Message:   fmt.Sprintf("Probe %s is down (%d consecutive failures)", result.Name, count),
				Details:   details,
				Timestamp: result.Timestamp,
			}
		}
	}

	if event != nil {
		m.dispatch(ctx, *event)
	}
}

// HandleBudgetViolation sends a notification when a budget metric becomes
// newly violated. Safe to call on a nil Manager.
func (m *Manager) HandleBudgetViolation(ctx context.Context, probeName, metric string, violated bool) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := probeName + ":" + metric
	prevViolated := m.budgetStates[key]
	m.budgetStates[key] = violated

	// Only notify on transition to violated
	if violated && !prevViolated {
		m.dispatch(ctx, Event{
			Type:      EventBudgetViolation,
			Severity:  SeverityWarning,
			Probe:     probeName,
			Message:   fmt.Sprintf("Budget violation: %s exceeds %s threshold", probeName, metric),
			Details:   map[string]string{"metric": metric},
			Timestamp: time.Now(),
		})
	}
}

// HandleCertResult checks TLS probe results for certificate expiry and sends
// severity-appropriate notifications. It tracks per-probe cert state so that
// notifications fire on transitions (ok→warning, warning→critical, etc.)
// rather than every probe cycle. Safe to call on a nil Manager.
func (m *Manager) HandleCertResult(ctx context.Context, result *probe.Result) {
	if m == nil || result.Type != config.ProbeTypeTLS {
		return
	}
	if result.CertDaysRemaining == 0 && !result.CertValid {
		return // no cert data (connection failed, etc.)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := "cert:" + result.Name
	var severity Severity
	switch {
	case result.CertDaysRemaining <= result.CertCriticalDays():
		severity = SeverityCritical
	case result.CertDaysRemaining <= result.CertWarnDays():
		severity = SeverityWarning
	default:
		// Cert is healthy — clear any previous state so we re-fire if it
		// enters a warning window again later.
		delete(m.certStates, key)
		return
	}

	prev := m.certStates[key]
	if prev == severity {
		return // already notified at this severity level
	}
	m.certStates[key] = severity

	details := map[string]string{
		"days_remaining": fmt.Sprintf("%d", result.CertDaysRemaining),
		"expiry":         result.CertExpiry.Format("2006-01-02"),
		"subject":        result.CertSubject,
	}
	if !result.CertValid {
		details["chain_valid"] = "false"
	}

	m.dispatch(ctx, Event{
		Type:      EventCertExpiring,
		Severity:  severity,
		Probe:     result.Name,
		ProbeType: result.Type,
		Message:   fmt.Sprintf("TLS certificate for %s expires in %d days", result.Name, result.CertDaysRemaining),
		Details:   details,
		Timestamp: result.Timestamp,
	})
}

// Wait blocks until all in-flight webhook sends have completed.
// Safe to call on a nil Manager.
func (m *Manager) Wait() {
	if m == nil {
		return
	}
	m.wg.Wait()
}

func (m *Manager) dispatch(_ context.Context, event Event) {
	now := time.Now()
	for i, wh := range m.webhooks {
		if !wh.events[event.Type] {
			continue
		}
		// If webhook has severity filter and event has a severity, check it
		if wh.severities != nil && event.Severity != "" && !wh.severities[event.Severity] {
			continue
		}

		cooldownKey := fmt.Sprintf("%d:%s:%s", i, event.Probe, event.Type)
		if last, ok := m.lastSent[cooldownKey]; ok && now.Sub(last) < wh.cooldown {
			slog.Debug("Webhook notification suppressed (cooldown)", "probe", event.Probe, "type", event.Type)
			continue
		}

		m.lastSent[cooldownKey] = now
		m.wg.Add(1)
		go func(s Sender) {
			defer m.wg.Done()
			m.sem <- struct{}{} // acquire
			defer func() { <-m.sem }()
			sendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.Send(sendCtx, event); err != nil {
				slog.Warn("Webhook send failed, retrying", "type", event.Type, "probe", event.Probe, "error", err)
				time.Sleep(2 * time.Second)
				if retryErr := s.Send(sendCtx, event); retryErr != nil {
					slog.Warn("Webhook retry failed", "type", event.Type, "probe", event.Probe, "error", retryErr)
				}
			}
		}(wh.sender)
	}
}
