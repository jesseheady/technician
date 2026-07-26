package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// captureServer returns a test server that records the last request body and
// replies with the given status.
func captureServer(t *testing.T, status int, gotBody *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*gotBody = string(b)
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

var sampleEvent = Event{
	Type:      EventCheckDown,
	Severity:  SeverityCritical,
	Check:     "api-health",
	Message:   "api-health is down",
	Details:   map[string]string{"error": "connection refused"},
	Timestamp: time.Unix(1_700_000_000, 0),
}

func TestSendersPostValidJSONAndSucceed(t *testing.T) {
	cases := map[string]func(url string) Sender{
		"generic": func(u string) Sender { return NewGenericSender(u) },
		"slack":   func(u string) Sender { return NewSlackSender(u) },
		"discord": func(u string) Sender { return NewDiscordSender(u) },
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			var body string
			srv := captureServer(t, http.StatusOK, &body)

			if err := mk(srv.URL).Send(context.Background(), sampleEvent); err != nil {
				t.Fatalf("Send: %v", err)
			}
			if !json.Valid([]byte(body)) {
				t.Fatalf("posted body is not valid JSON: %q", body)
			}
			if !strings.Contains(body, "api-health") {
				t.Errorf("payload missing check name; got %q", body)
			}
		})
	}
}

func TestSendersReturnErrorOnNon2xx(t *testing.T) {
	senders := map[string]func(url string) Sender{
		"generic": func(u string) Sender { return NewGenericSender(u) },
		"slack":   func(u string) Sender { return NewSlackSender(u) },
		"discord": func(u string) Sender { return NewDiscordSender(u) },
	}
	for name, mk := range senders {
		t.Run(name, func(t *testing.T) {
			var body string
			srv := captureServer(t, http.StatusInternalServerError, &body)

			if err := mk(srv.URL).Send(context.Background(), sampleEvent); err == nil {
				t.Error("expected error on 500 response, got nil")
			}
		})
	}
}

// Slack encodes severity into the text prefix and colour; assert that mapping
// rather than just that a request went out.
func TestSlackEncodesSeverityAndColor(t *testing.T) {
	var body string
	srv := captureServer(t, http.StatusOK, &body)

	warn := sampleEvent
	warn.Type = EventBudgetViolation
	warn.Severity = SeverityWarning
	if err := NewSlackSender(srv.URL).Send(context.Background(), warn); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var payload slackPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("unmarshal slack payload: %v", err)
	}
	if !strings.HasPrefix(payload.Text, "[WARNING]") {
		t.Errorf("text = %q, want [WARNING] prefix", payload.Text)
	}
	if len(payload.Attachments) != 1 || payload.Attachments[0].Color != "#f5a623" {
		t.Errorf("warning colour = %+v, want amber #f5a623", payload.Attachments)
	}
}
