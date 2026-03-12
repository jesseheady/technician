package notify

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
	"github.com/monkeyWzr/technician/internal/probe"
)

// mockSender records events sent to it.
type mockSender struct {
	mu     sync.Mutex
	events []Event
	err    error // if set, Send returns this error
}

func (m *mockSender) Send(_ context.Context, event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return m.err
}

func (m *mockSender) sent() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Event, len(m.events))
	copy(cp, m.events)
	return cp
}

func (m *mockSender) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
}

// newTestManager creates a Manager with a single mock sender subscribed to all events.
func newTestManager(mock *mockSender, cooldown time.Duration) *Manager {
	return &Manager{
		webhooks: []webhook{{
			sender:   mock,
			events:   map[EventType]bool{EventProbeDown: true, EventProbeUp: true, EventBudgetViolation: true},
			cooldown: cooldown,
		}},
		sem:          make(chan struct{}, maxConcurrentSends),
		probeStates:  make(map[string]bool),
		budgetStates: make(map[string]bool),
		lastSent:     make(map[string]time.Time),
	}
}

func makeResult(name string, success bool) *probe.Result {
	return &probe.Result{
		Name:      name,
		Type:      config.ProbeTypeHTTP,
		Success:   success,
		Timestamp: time.Now(),
	}
}

// wait briefly for async dispatch goroutines to complete.
func waitForDispatch() {
	time.Sleep(50 * time.Millisecond)
}

func TestNilManagerIsSafe(t *testing.T) {
	var m *Manager
	ctx := context.Background()
	m.HandleResult(ctx, makeResult("x", true))
	m.HandleBudgetViolation(ctx, "x", "duration", true)
	// No panic = pass
}

func TestNewManagerReturnsNilWhenEmpty(t *testing.T) {
	m := NewManager(nil)
	if m != nil {
		t.Fatal("expected nil manager for empty config")
	}
	m = NewManager([]config.WebhookConfig{})
	if m != nil {
		t.Fatal("expected nil manager for zero-length config")
	}
}

func TestFirstResultEstablishesBaseline(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// First result should not trigger any notification
	m.HandleResult(ctx, makeResult("test", false))
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected no events on first result, got %d", len(mock.sent()))
	}
}

func TestProbeDownTransition(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Establish baseline (up)
	m.HandleResult(ctx, makeResult("test", true))
	// Transition to down
	m.HandleResult(ctx, makeResult("test", false))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventProbeDown {
		t.Fatalf("expected probe_down, got %s", events[0].Type)
	}
	if events[0].Probe != "test" {
		t.Fatalf("expected probe name 'test', got %s", events[0].Probe)
	}
}

func TestProbeUpTransition(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Establish baseline (down)
	m.HandleResult(ctx, makeResult("test", false))
	// Transition to up
	m.HandleResult(ctx, makeResult("test", true))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventProbeUp {
		t.Fatalf("expected probe_up, got %s", events[0].Type)
	}
}

func TestNoNotificationOnSameState(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Establish baseline (up)
	m.HandleResult(ctx, makeResult("test", true))
	// Same state — no transition
	m.HandleResult(ctx, makeResult("test", true))
	m.HandleResult(ctx, makeResult("test", true))
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected no events for same state, got %d", len(mock.sent()))
	}
}

func TestInfraErrorsIgnored(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Establish baseline (up)
	m.HandleResult(ctx, makeResult("test", true))

	// Infra error should not change state
	infraResult := makeResult("test", false)
	infraResult.InfraError = true
	m.HandleResult(ctx, infraResult)
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected no events for infra error, got %d", len(mock.sent()))
	}

	// After infra error, a real failure should trigger (since baseline is still "up")
	m.HandleResult(ctx, makeResult("test", false))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event after infra error then real failure, got %d", len(events))
	}
	if events[0].Type != EventProbeDown {
		t.Fatalf("expected probe_down, got %s", events[0].Type)
	}
}

func TestProbeDownIncludesDetails(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	m.HandleResult(ctx, makeResult("test", true))

	failResult := makeResult("test", false)
	failResult.Error = "connection refused"
	failResult.StatusCode = 503
	m.HandleResult(ctx, failResult)
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Details["error"] != "connection refused" {
		t.Fatalf("expected error detail, got %q", events[0].Details["error"])
	}
	if events[0].Details["status_code"] != "503" {
		t.Fatalf("expected status_code detail, got %q", events[0].Details["status_code"])
	}
}

func TestCooldownSuppressesDuplicates(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 1*time.Hour) // very long cooldown
	ctx := context.Background()

	// up -> down (fires)
	m.HandleResult(ctx, makeResult("test", true))
	m.HandleResult(ctx, makeResult("test", false))
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event, got %d", len(mock.sent()))
	}

	// down -> up -> down again (up fires, but second down is suppressed by cooldown)
	m.HandleResult(ctx, makeResult("test", true))
	waitForDispatch()

	// probe_up should fire since it's a different event type
	if len(mock.sent()) != 2 {
		t.Fatalf("expected 2 events (down + up), got %d", len(mock.sent()))
	}

	m.HandleResult(ctx, makeResult("test", false))
	waitForDispatch()

	// probe_down again — should be suppressed by cooldown
	if len(mock.sent()) != 2 {
		t.Fatalf("expected 2 events (second down suppressed), got %d", len(mock.sent()))
	}
}

func TestCooldownExpiresAllowsRetrigger(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 1*time.Millisecond) // very short cooldown
	ctx := context.Background()

	// up -> down
	m.HandleResult(ctx, makeResult("test", true))
	m.HandleResult(ctx, makeResult("test", false))
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event, got %d", len(mock.sent()))
	}

	// Wait for cooldown to expire
	time.Sleep(5 * time.Millisecond)

	// down -> up -> down again (should fire since cooldown expired)
	m.HandleResult(ctx, makeResult("test", true))
	m.HandleResult(ctx, makeResult("test", false))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 3 {
		t.Fatalf("expected 3 events (down, up, down), got %d", len(events))
	}
}

func TestEventFiltering(t *testing.T) {
	mock := &mockSender{}
	// Only subscribe to probe_down events
	m := &Manager{
		webhooks: []webhook{{
			sender:   mock,
			events:   map[EventType]bool{EventProbeDown: true},
			cooldown: 0,
		}},
		sem:          make(chan struct{}, maxConcurrentSends),
		probeStates:  make(map[string]bool),
		budgetStates: make(map[string]bool),
		lastSent:     make(map[string]time.Time),
	}
	ctx := context.Background()

	// up -> down (should fire)
	m.HandleResult(ctx, makeResult("test", true))
	m.HandleResult(ctx, makeResult("test", false))
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event, got %d", len(mock.sent()))
	}

	// down -> up (should NOT fire — not subscribed)
	m.HandleResult(ctx, makeResult("test", true))
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event (up filtered out), got %d", len(mock.sent()))
	}
}

func TestBudgetViolationNewTransition(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// First violation — should fire
	m.HandleBudgetViolation(ctx, "test", "duration", true)
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventBudgetViolation {
		t.Fatalf("expected budget_violation, got %s", events[0].Type)
	}
}

func TestBudgetViolationSameStateSuppressed(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// First violation fires
	m.HandleBudgetViolation(ctx, "test", "duration", true)
	waitForDispatch()
	// Same state — should not fire again
	m.HandleBudgetViolation(ctx, "test", "duration", true)
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event (duplicate suppressed), got %d", len(mock.sent()))
	}
}

func TestBudgetViolationRefiresAfterRecovery(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// First violation
	m.HandleBudgetViolation(ctx, "test", "duration", true)
	waitForDispatch()
	// Recovery (clears state)
	m.HandleBudgetViolation(ctx, "test", "duration", false)
	waitForDispatch()
	// Re-violation — should fire again
	m.HandleBudgetViolation(ctx, "test", "duration", true)
	waitForDispatch()

	events := mock.sent()
	if len(events) != 2 {
		t.Fatalf("expected 2 events (violation, re-violation), got %d", len(events))
	}
}

func TestBudgetNotViolatedNeverFires(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	m.HandleBudgetViolation(ctx, "test", "duration", false)
	m.HandleBudgetViolation(ctx, "test", "duration", false)
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected no events for non-violations, got %d", len(mock.sent()))
	}
}

func TestMultipleProbesIndependent(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Establish baselines for two probes
	m.HandleResult(ctx, makeResult("a", true))
	m.HandleResult(ctx, makeResult("b", true))

	// Only probe "a" goes down
	m.HandleResult(ctx, makeResult("a", false))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Probe != "a" {
		t.Fatalf("expected probe 'a', got %s", events[0].Probe)
	}
}

func TestMultipleWebhooks(t *testing.T) {
	mock1 := &mockSender{}
	mock2 := &mockSender{}
	m := &Manager{
		webhooks: []webhook{
			{sender: mock1, events: map[EventType]bool{EventProbeDown: true}, cooldown: 0},
			{sender: mock2, events: map[EventType]bool{EventProbeDown: true, EventProbeUp: true}, cooldown: 0},
		},
		sem:          make(chan struct{}, maxConcurrentSends),
		probeStates:  make(map[string]bool),
		budgetStates: make(map[string]bool),
		lastSent:     make(map[string]time.Time),
	}
	ctx := context.Background()

	m.HandleResult(ctx, makeResult("test", true))
	m.HandleResult(ctx, makeResult("test", false))
	waitForDispatch()

	// Both should get probe_down
	if len(mock1.sent()) != 1 {
		t.Fatalf("webhook 1: expected 1 event, got %d", len(mock1.sent()))
	}
	if len(mock2.sent()) != 1 {
		t.Fatalf("webhook 2: expected 1 event, got %d", len(mock2.sent()))
	}

	// Recover — only mock2 subscribes to probe_up
	m.HandleResult(ctx, makeResult("test", true))
	waitForDispatch()

	if len(mock1.sent()) != 1 {
		t.Fatalf("webhook 1: expected 1 event (up filtered), got %d", len(mock1.sent()))
	}
	if len(mock2.sent()) != 2 {
		t.Fatalf("webhook 2: expected 2 events, got %d", len(mock2.sent()))
	}
}
