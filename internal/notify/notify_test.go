package notify

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/m0nkey/technician/internal/config"
	"github.com/m0nkey/technician/internal/check"
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
			events:   map[EventType]bool{EventCheckDown: true, EventCheckUp: true, EventBudgetViolation: true, EventCertExpiring: true},
			cooldown: cooldown,
		}},
		sem:          make(chan struct{}, maxConcurrentSends),
		checkStates:  make(map[string]bool),
		failCounts:   make(map[string]int),
		notifiedDown: make(map[string]bool),
		budgetStates: make(map[string]bool),
		certStates:   make(map[string]Severity),
		lastSent:     make(map[string]time.Time),
	}
}

// sendFailures sends N consecutive failures for the given check name.
func sendFailures(m *Manager, ctx context.Context, name string, n int) {
	for i := 0; i < n; i++ {
		m.HandleResult(ctx, makeResult(name, false))
	}
}

func makeResult(name string, success bool) *check.Result {
	return &check.Result{
		Name:      name,
		Type:      config.CheckTypeHTTP,
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

func TestCheckDownTransition(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Establish baseline (up)
	m.HandleResult(ctx, makeResult("test", true))
	// Need consecutiveFailThreshold failures to trigger notification
	sendFailures(m, ctx, "test", consecutiveFailThreshold)
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventCheckDown {
		t.Fatalf("expected check_down, got %s", events[0].Type)
	}
	if events[0].Check != "test" {
		t.Fatalf("expected check name 'test', got %s", events[0].Check)
	}
}

func TestCheckDownNotFiredBeforeThreshold(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	m.HandleResult(ctx, makeResult("test", true))
	// Send one fewer than the threshold
	sendFailures(m, ctx, "test", consecutiveFailThreshold-1)
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected no events before threshold, got %d", len(mock.sent()))
	}
}

func TestTransientFailureResetsCounter(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	m.HandleResult(ctx, makeResult("test", true))
	// Fail twice (below threshold)
	sendFailures(m, ctx, "test", consecutiveFailThreshold-1)
	// Succeed — resets counter
	m.HandleResult(ctx, makeResult("test", true))
	// Fail twice again (below threshold)
	sendFailures(m, ctx, "test", consecutiveFailThreshold-1)
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected no events (counter reset by success), got %d", len(mock.sent()))
	}
}

func TestCheckUpTransition(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Establish baseline (up), then go down past threshold
	m.HandleResult(ctx, makeResult("test", true))
	sendFailures(m, ctx, "test", consecutiveFailThreshold)
	waitForDispatch()

	if len(mock.sent()) != 1 || mock.sent()[0].Type != EventCheckDown {
		t.Fatalf("expected check_down event, got %v", mock.sent())
	}

	// Recover
	m.HandleResult(ctx, makeResult("test", true))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 2 {
		t.Fatalf("expected 2 events (down + up), got %d", len(events))
	}
	if events[1].Type != EventCheckUp {
		t.Fatalf("expected check_up, got %s", events[1].Type)
	}
}

func TestNoRecoveryWithoutPriorDownNotification(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Establish baseline (down) — no notification on first result
	m.HandleResult(ctx, makeResult("test", false))
	// Recover — but we never notified down, so no up notification
	m.HandleResult(ctx, makeResult("test", true))
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected no events (no prior down notification), got %d", len(mock.sent()))
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

	// Infra error should not change state or increment fail counter
	infraResult := makeResult("test", false)
	infraResult.InfraError = true
	m.HandleResult(ctx, infraResult)
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected no events for infra error, got %d", len(mock.sent()))
	}

	// After infra error, real failures should still need full threshold
	sendFailures(m, ctx, "test", consecutiveFailThreshold)
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event after threshold failures, got %d", len(events))
	}
	if events[0].Type != EventCheckDown {
		t.Fatalf("expected check_down, got %s", events[0].Type)
	}
}

func TestCheckDownIncludesDetails(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	m.HandleResult(ctx, makeResult("test", true))

	// Send threshold-1 plain failures, then one with details
	sendFailures(m, ctx, "test", consecutiveFailThreshold-1)
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
	if events[0].Details["consecutive_failures"] != fmt.Sprintf("%d", consecutiveFailThreshold) {
		t.Fatalf("expected consecutive_failures detail, got %q", events[0].Details["consecutive_failures"])
	}
}

func TestCooldownSuppressesDuplicates(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 1*time.Hour) // very long cooldown
	ctx := context.Background()

	// up -> down (fires after threshold)
	m.HandleResult(ctx, makeResult("test", true))
	sendFailures(m, ctx, "test", consecutiveFailThreshold)
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event, got %d", len(mock.sent()))
	}

	// down -> up -> down again (up fires, but second down is suppressed by cooldown)
	m.HandleResult(ctx, makeResult("test", true))
	waitForDispatch()

	// check_up should fire since it's a different event type
	if len(mock.sent()) != 2 {
		t.Fatalf("expected 2 events (down + up), got %d", len(mock.sent()))
	}

	sendFailures(m, ctx, "test", consecutiveFailThreshold)
	waitForDispatch()

	// check_down again — should be suppressed by cooldown
	if len(mock.sent()) != 2 {
		t.Fatalf("expected 2 events (second down suppressed), got %d", len(mock.sent()))
	}
}

func TestCooldownExpiresAllowsRetrigger(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 1*time.Millisecond) // very short cooldown
	ctx := context.Background()

	// up -> down (after threshold)
	m.HandleResult(ctx, makeResult("test", true))
	sendFailures(m, ctx, "test", consecutiveFailThreshold)
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event, got %d", len(mock.sent()))
	}

	// Wait for cooldown to expire
	time.Sleep(5 * time.Millisecond)

	// down -> up -> down again (should fire since cooldown expired)
	m.HandleResult(ctx, makeResult("test", true))
	sendFailures(m, ctx, "test", consecutiveFailThreshold)
	waitForDispatch()

	events := mock.sent()
	if len(events) != 3 {
		t.Fatalf("expected 3 events (down, up, down), got %d", len(events))
	}
}

func TestEventFiltering(t *testing.T) {
	mock := &mockSender{}
	// Only subscribe to check_down events
	m := &Manager{
		webhooks: []webhook{{
			sender:   mock,
			events:   map[EventType]bool{EventCheckDown: true},
			cooldown: 0,
		}},
		sem:          make(chan struct{}, maxConcurrentSends),
		checkStates:  make(map[string]bool),
		failCounts:   make(map[string]int),
		notifiedDown: make(map[string]bool),
		budgetStates: make(map[string]bool),
		certStates:   make(map[string]Severity),
		lastSent:     make(map[string]time.Time),
	}
	ctx := context.Background()

	// up -> down (should fire after threshold)
	m.HandleResult(ctx, makeResult("test", true))
	sendFailures(m, ctx, "test", consecutiveFailThreshold)
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

func TestMultipleChecksIndependent(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Establish baselines for two checks
	m.HandleResult(ctx, makeResult("a", true))
	m.HandleResult(ctx, makeResult("b", true))

	// Only check "a" goes down
	sendFailures(m, ctx, "a", consecutiveFailThreshold)
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Check != "a" {
		t.Fatalf("expected check 'a', got %s", events[0].Check)
	}
}

func TestMultipleWebhooks(t *testing.T) {
	mock1 := &mockSender{}
	mock2 := &mockSender{}
	m := &Manager{
		webhooks: []webhook{
			{sender: mock1, events: map[EventType]bool{EventCheckDown: true}, cooldown: 0},
			{sender: mock2, events: map[EventType]bool{EventCheckDown: true, EventCheckUp: true}, cooldown: 0},
		},
		sem:          make(chan struct{}, maxConcurrentSends),
		checkStates:  make(map[string]bool),
		failCounts:   make(map[string]int),
		notifiedDown: make(map[string]bool),
		budgetStates: make(map[string]bool),
		certStates:   make(map[string]Severity),
		lastSent:     make(map[string]time.Time),
	}
	ctx := context.Background()

	m.HandleResult(ctx, makeResult("test", true))
	sendFailures(m, ctx, "test", consecutiveFailThreshold)
	waitForDispatch()

	// Both should get check_down
	if len(mock1.sent()) != 1 {
		t.Fatalf("webhook 1: expected 1 event, got %d", len(mock1.sent()))
	}
	if len(mock2.sent()) != 1 {
		t.Fatalf("webhook 2: expected 1 event, got %d", len(mock2.sent()))
	}

	// Recover — only mock2 subscribes to check_up
	m.HandleResult(ctx, makeResult("test", true))
	waitForDispatch()

	if len(mock1.sent()) != 1 {
		t.Fatalf("webhook 1: expected 1 event (up filtered), got %d", len(mock1.sent()))
	}
	if len(mock2.sent()) != 2 {
		t.Fatalf("webhook 2: expected 2 events, got %d", len(mock2.sent()))
	}
}

// --- Severity filtering tests ---

func TestSeverityFilterBlocksWarnings(t *testing.T) {
	mock := &mockSender{}
	// Only subscribe to critical severity
	m := &Manager{
		webhooks: []webhook{{
			sender:     mock,
			events:     map[EventType]bool{EventCheckDown: true, EventCertExpiring: true, EventBudgetViolation: true},
			severities: map[Severity]bool{SeverityCritical: true},
			cooldown:   0,
		}},
		sem:          make(chan struct{}, maxConcurrentSends),
		checkStates:  make(map[string]bool),
		failCounts:   make(map[string]int),
		notifiedDown: make(map[string]bool),
		budgetStates: make(map[string]bool),
		certStates:   make(map[string]Severity),
		lastSent:     make(map[string]time.Time),
	}
	ctx := context.Background()

	// Budget violation is severity=warning — should be filtered
	m.HandleBudgetViolation(ctx, "test", "duration", true)
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected 0 events (warning filtered), got %d", len(mock.sent()))
	}

	// Check down is severity=critical — should pass
	m.HandleResult(ctx, makeResult("test", true))
	sendFailures(m, ctx, "test", consecutiveFailThreshold)
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event (critical passes), got %d", len(mock.sent()))
	}
	if mock.sent()[0].Severity != SeverityCritical {
		t.Fatalf("expected critical severity, got %s", mock.sent()[0].Severity)
	}
}

func TestSeverityFilterAllowsWarnings(t *testing.T) {
	mock := &mockSender{}
	// Only subscribe to warning severity
	m := &Manager{
		webhooks: []webhook{{
			sender:     mock,
			events:     map[EventType]bool{EventCheckDown: true, EventBudgetViolation: true},
			severities: map[Severity]bool{SeverityWarning: true},
			cooldown:   0,
		}},
		sem:          make(chan struct{}, maxConcurrentSends),
		checkStates:  make(map[string]bool),
		failCounts:   make(map[string]int),
		notifiedDown: make(map[string]bool),
		budgetStates: make(map[string]bool),
		certStates:   make(map[string]Severity),
		lastSent:     make(map[string]time.Time),
	}
	ctx := context.Background()

	// Budget violation = warning — should pass
	m.HandleBudgetViolation(ctx, "test", "duration", true)
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event (warning passes), got %d", len(mock.sent()))
	}

	// Check down = critical — should be filtered
	m.HandleResult(ctx, makeResult("test2", true))
	sendFailures(m, ctx, "test2", consecutiveFailThreshold)
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event (critical filtered), got %d", len(mock.sent()))
	}
}

func TestNoSeverityFilterPassesAll(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Both warning (budget) and critical (probe_down) should pass
	m.HandleBudgetViolation(ctx, "test", "duration", true)
	m.HandleResult(ctx, makeResult("test2", true))
	sendFailures(m, ctx, "test2", consecutiveFailThreshold)
	waitForDispatch()

	if len(mock.sent()) != 2 {
		t.Fatalf("expected 2 events (no severity filter), got %d", len(mock.sent()))
	}
}

// --- Cert expiring notification tests ---

func makeTLSResult(name string, daysRemaining, warnDays, critDays int) *check.Result {
	return &check.Result{
		Name:              name,
		Type:              config.CheckTypeTLS,
		Success:           true,
		CertDaysRemaining: daysRemaining,
		CertValid:         true,
		CertExpiry:        time.Now().Add(time.Duration(daysRemaining) * 24 * time.Hour),
		CertSubject:       name,
		CertWarnDaysVal:   warnDays,
		CertCritDaysVal:   critDays,
		Timestamp:         time.Now(),
	}
}

func TestCertExpiringWarning(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// 20 days remaining, warn=30, crit=7 → warning
	m.HandleCertResult(ctx, makeTLSResult("example.com", 20, 30, 7))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventCertExpiring {
		t.Fatalf("expected cert_expiring, got %s", events[0].Type)
	}
	if events[0].Severity != SeverityWarning {
		t.Fatalf("expected warning severity, got %s", events[0].Severity)
	}
}

func TestCertExpiringCritical(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// 5 days remaining, warn=30, crit=7 → critical
	m.HandleCertResult(ctx, makeTLSResult("example.com", 5, 30, 7))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Severity != SeverityCritical {
		t.Fatalf("expected critical severity, got %s", events[0].Severity)
	}
}

func TestCertExpiringHealthyCertNoNotification(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// 90 days remaining, warn=30 → healthy, no notification
	m.HandleCertResult(ctx, makeTLSResult("example.com", 90, 30, 7))
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected 0 events for healthy cert, got %d", len(mock.sent()))
	}
}

func TestCertExpiringSameSeveritySuppressed(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// First warning fires
	m.HandleCertResult(ctx, makeTLSResult("example.com", 20, 30, 7))
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event, got %d", len(mock.sent()))
	}

	// Same severity — suppressed
	m.HandleCertResult(ctx, makeTLSResult("example.com", 18, 30, 7))
	waitForDispatch()

	if len(mock.sent()) != 1 {
		t.Fatalf("expected 1 event (duplicate suppressed), got %d", len(mock.sent()))
	}
}

func TestCertExpiringEscalatesWarningToCritical(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Warning first
	m.HandleCertResult(ctx, makeTLSResult("example.com", 20, 30, 7))
	waitForDispatch()

	// Escalate to critical
	m.HandleCertResult(ctx, makeTLSResult("example.com", 5, 30, 7))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 2 {
		t.Fatalf("expected 2 events (warning + critical), got %d", len(events))
	}
	if events[0].Severity != SeverityWarning {
		t.Fatalf("first event: expected warning, got %s", events[0].Severity)
	}
	if events[1].Severity != SeverityCritical {
		t.Fatalf("second event: expected critical, got %s", events[1].Severity)
	}
}

func TestCertExpiringClearsOnRecovery(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// Warning fires
	m.HandleCertResult(ctx, makeTLSResult("example.com", 20, 30, 7))
	waitForDispatch()

	// Cert renewed — healthy
	m.HandleCertResult(ctx, makeTLSResult("example.com", 90, 30, 7))
	waitForDispatch()

	// Enter warning window again — should re-fire
	m.HandleCertResult(ctx, makeTLSResult("example.com", 25, 30, 7))
	waitForDispatch()

	events := mock.sent()
	if len(events) != 2 {
		t.Fatalf("expected 2 events (fire, recover, re-fire), got %d", len(events))
	}
}

func TestCertExpiringNonTLSIgnored(t *testing.T) {
	mock := &mockSender{}
	m := newTestManager(mock, 0)
	ctx := context.Background()

	// HTTP check result — should be ignored by HandleCertResult
	httpResult := makeResult("test", true)
	m.HandleCertResult(ctx, httpResult)
	waitForDispatch()

	if len(mock.sent()) != 0 {
		t.Fatalf("expected 0 events for non-TLS check, got %d", len(mock.sent()))
	}
}

func TestCertExpiringNilManagerSafe(t *testing.T) {
	var m *Manager
	ctx := context.Background()
	m.HandleCertResult(ctx, makeTLSResult("example.com", 5, 30, 7))
	// No panic = pass
}

func TestSeverityFilterWithCertExpiring(t *testing.T) {
	mockWarn := &mockSender{}
	mockCrit := &mockSender{}
	m := &Manager{
		webhooks: []webhook{
			{sender: mockWarn, events: map[EventType]bool{EventCertExpiring: true}, severities: map[Severity]bool{SeverityWarning: true}, cooldown: 0},
			{sender: mockCrit, events: map[EventType]bool{EventCertExpiring: true}, severities: map[Severity]bool{SeverityCritical: true}, cooldown: 0},
		},
		sem:          make(chan struct{}, maxConcurrentSends),
		checkStates:  make(map[string]bool),
		failCounts:   make(map[string]int),
		notifiedDown: make(map[string]bool),
		budgetStates: make(map[string]bool),
		certStates:   make(map[string]Severity),
		lastSent:     make(map[string]time.Time),
	}
	ctx := context.Background()

	// Warning-level cert expiry → only mockWarn receives
	m.HandleCertResult(ctx, makeTLSResult("example.com", 20, 30, 7))
	waitForDispatch()

	if len(mockWarn.sent()) != 1 {
		t.Fatalf("warn webhook: expected 1 event, got %d", len(mockWarn.sent()))
	}
	if len(mockCrit.sent()) != 0 {
		t.Fatalf("crit webhook: expected 0 events, got %d", len(mockCrit.sent()))
	}

	// Escalate to critical → only mockCrit receives
	m.HandleCertResult(ctx, makeTLSResult("example.com", 5, 30, 7))
	waitForDispatch()

	if len(mockWarn.sent()) != 1 {
		t.Fatalf("warn webhook: expected 1 event (critical filtered), got %d", len(mockWarn.sent()))
	}
	if len(mockCrit.sent()) != 1 {
		t.Fatalf("crit webhook: expected 1 event, got %d", len(mockCrit.sent()))
	}
}
