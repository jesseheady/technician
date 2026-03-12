package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GenericSender posts the Event as JSON to any HTTP endpoint.
type GenericSender struct {
	URL    string
	Client http.Client
}

// NewGenericSender creates a GenericSender with a pre-configured timeout.
func NewGenericSender(url string) *GenericSender {
	return &GenericSender{URL: url, Client: http.Client{Timeout: 10 * time.Second}}
}

type genericPayload struct {
	Type      string            `json:"type"`
	Severity  string            `json:"severity,omitempty"`
	Probe     string            `json:"probe"`
	ProbeType string            `json:"probe_type,omitempty"`
	Message   string            `json:"message"`
	Details   map[string]string `json:"details,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

func (g *GenericSender) Send(ctx context.Context, event Event) error {
	payload := genericPayload{
		Type:      string(event.Type),
		Severity:  string(event.Severity),
		Probe:     event.Probe,
		ProbeType: string(event.ProbeType),
		Message:   event.Message,
		Details:   event.Details,
		Timestamp: event.Timestamp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal generic payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create generic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.Client.Do(req)
	if err != nil {
		return fmt.Errorf("generic webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("generic webhook returned %d", resp.StatusCode)
	}
	return nil
}
