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

// DiscordSender posts notifications to a Discord webhook using the native
// Discord embed format (not the /slack compatibility endpoint).
type DiscordSender struct {
	URL    string
	Client http.Client
}

// NewDiscordSender creates a DiscordSender with a pre-configured timeout.
func NewDiscordSender(url string) *DiscordSender {
	return &DiscordSender{URL: url, Client: http.Client{Timeout: 10 * time.Second}}
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	Color       int                 `json:"color"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Timestamp   string              `json:"timestamp"`
	Footer      *discordEmbedFooter `json:"footer,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordEmbedFooter struct {
	Text string `json:"text"`
}

const (
	colorRed    = 0xFF0000 // critical: check down, cert expiry critical
	colorAmber  = 0xF5A623 // warning: budget violation, cert expiry warning
	colorGreen  = 0x00FF00 // recovery: check up
)

func (d *DiscordSender) Send(ctx context.Context, event Event) error {
	embed := discordEmbed{
		Timestamp: event.Timestamp.UTC().Format(time.RFC3339),
		Footer:    &discordEmbedFooter{Text: "Technician"},
	}

	switch event.Type {
	case EventCheckDown:
		embed.Title = fmt.Sprintf("Check Down: %s", event.Check)
		embed.Color = colorRed
		var desc strings.Builder
		desc.WriteString(fmt.Sprintf("Check **%s**", event.Check))
		if event.CheckType != "" {
			desc.WriteString(fmt.Sprintf(" (%s)", event.CheckType))
		}
		desc.WriteString(" is down.")
		embed.Description = desc.String()
		if errMsg, ok := event.Details["error"]; ok && errMsg != "" {
			embed.Fields = append(embed.Fields, discordEmbedField{
				Name: "Error", Value: errMsg, Inline: false,
			})
		}
		if sc, ok := event.Details["status_code"]; ok {
			embed.Fields = append(embed.Fields, discordEmbedField{
				Name: "Status Code", Value: sc, Inline: true,
			})
		}

	case EventCheckUp:
		embed.Title = fmt.Sprintf("Check Recovered: %s", event.Check)
		embed.Color = colorGreen
		embed.Description = fmt.Sprintf("Check **%s** is back up.", event.Check)

	case EventBudgetViolation:
		embed.Title = fmt.Sprintf("Budget Violation: %s", event.Check)
		embed.Color = colorAmber
		metric := event.Details["metric"]
		embed.Description = fmt.Sprintf("Check **%s** exceeds **%s** budget threshold.", event.Check, metric)

	case EventCertExpiring:
		severityLabel := strings.ToUpper(string(event.Severity))
		embed.Title = fmt.Sprintf("[%s] Certificate Expiring: %s", severityLabel, event.Check)
		if event.Severity == SeverityCritical {
			embed.Color = colorRed
		} else {
			embed.Color = colorAmber
		}
		embed.Description = event.Message
		if days, ok := event.Details["days_remaining"]; ok {
			embed.Fields = append(embed.Fields, discordEmbedField{
				Name: "Days Remaining", Value: days, Inline: true,
			})
		}
		if expiry, ok := event.Details["expiry"]; ok {
			embed.Fields = append(embed.Fields, discordEmbedField{
				Name: "Expiry Date", Value: expiry, Inline: true,
			})
		}
		if subject, ok := event.Details["subject"]; ok && subject != "" {
			embed.Fields = append(embed.Fields, discordEmbedField{
				Name: "Subject", Value: subject, Inline: true,
			})
		}
	}

	payload := discordPayload{Embeds: []discordEmbed{embed}}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("discord webhook request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned %d", resp.StatusCode)
	}
	return nil
}
