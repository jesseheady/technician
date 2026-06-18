package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SlackSender posts notifications to a Slack incoming webhook.
type SlackSender struct {
	URL    string
	Client http.Client
}

// NewSlackSender creates a SlackSender with a pre-configured timeout.
func NewSlackSender(url string) *SlackSender {
	return &SlackSender{URL: url, Client: http.Client{Timeout: 10 * time.Second}}
}

type slackPayload struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

type slackAttachment struct {
	Color string `json:"color"`
	Text  string `json:"text"`
}

func (s *SlackSender) Send(ctx context.Context, event Event) error {
	color := "#ff0000" // default: red (critical)
	switch {
	case event.Type == EventCheckUp:
		color = "#00ff00"
	case event.Severity == SeverityWarning:
		color = "#f5a623"
	}

	text := event.Message
	if event.Severity != "" {
		text = fmt.Sprintf("[%s] %s", strings.ToUpper(string(event.Severity)), text)
	}

	var details string
	for k, v := range event.Details {
		if v != "" {
			details += fmt.Sprintf("%s: %s\n", k, v)
		}
	}
	payload := slackPayload{
		Text: text,
		Attachments: []slackAttachment{
			{Color: color, Text: details},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("slack webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}
	return nil
}
