package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/probe"
)

// EventType identifies the kind of notification.
type EventType string

const (
	EventProbeDown       EventType = "probe_down"
	EventProbeUp         EventType = "probe_up"
	EventBudgetViolation EventType = "budget_violation"
)

// Event is a single notification payload.
type Event struct {
	Type      EventType
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
	sender   Sender
	events   map[EventType]bool
	cooldown time.Duration
}

// Manager tracks probe state transitions and dispatches notifications
// to configured webhook endpoints.
type Manager struct {
	webhooks []webhook
	wg       sync.WaitGroup
	sem      chan struct{} // limits concurrent outbound webhook sends

	mu           sync.Mutex
	probeStates  map[string]bool      // probe key -> last success
	budgetStates map[string]bool      // "probe:metric" -> last violated
	lastSent     map[string]time.Time // "probe:eventType" -> last sent
}

const maxConcurrentSends = 4

// NewManager creates a Manager from the webhook configs. Returns nil
// if no webhooks are configured (callers can safely call nil Manager methods).
func NewManager(cfgWebhooks []config.WebhookConfig) *Manager {
	if len(cfgWebhooks) == 0 {
		return nil
	}

	m := &Manager{
		sem:          make(chan struct{}, maxConcurrentSends),
		probeStates:  make(map[string]bool),
		budgetStates: make(map[string]bool),
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
		} else {
			for _, e := range wc.Events {
				events[EventType(e)] = true
			}
		}

		cooldown := 5 * time.Minute
		if wc.Cooldown > 0 {
			cooldown = wc.Cooldown
		}

		m.webhooks = append(m.webhooks, webhook{sender: s, events: events, cooldown: cooldown})
	}

	slog.Info("Webhook notifications enabled", "endpoints", len(m.webhooks))
	return m
}

// HandleResult detects probe state transitions (up->down, down->up)
// and sends notifications. Safe to call on a nil Manager.
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
	prevSuccess, seen := m.probeStates[key]
	m.probeStates[key] = result.Success

	if !seen {
		return // first result — establish baseline, don't notify
	}

	var event *Event
	if prevSuccess && !result.Success {
		details := make(map[string]string)
		if result.Error != "" {
			details["error"] = result.Error
		}
		if result.StatusCode > 0 {
			details["status_code"] = fmt.Sprintf("%d", result.StatusCode)
		}
		event = &Event{
			Type:      EventProbeDown,
			Probe:     result.Name,
			ProbeType: result.Type,
			Message:   fmt.Sprintf("Probe %s is down", result.Name),
			Details:   details,
			Timestamp: result.Timestamp,
		}
	} else if !prevSuccess && result.Success {
		event = &Event{
			Type:      EventProbeUp,
			Probe:     result.Name,
			ProbeType: result.Type,
			Message:   fmt.Sprintf("Probe %s is back up", result.Name),
			Timestamp: result.Timestamp,
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
			Type:    EventBudgetViolation,
			Probe:   probeName,
			Message: fmt.Sprintf("Budget violation: %s exceeds %s threshold", probeName, metric),
			Details: map[string]string{"metric": metric},
			Timestamp: time.Now(),
		})
	}
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
