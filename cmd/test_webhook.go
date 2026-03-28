package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/m0nkey/technician/internal/config"
	"github.com/m0nkey/technician/internal/notify"
	"github.com/spf13/cobra"
)

var testWebhookCmd = &cobra.Command{
	Use:   "test-webhook",
	Short: "Send a test notification to all configured webhooks",
	RunE:  runTestWebhook,
}

func init() {
	rootCmd.AddCommand(testWebhookCmd)
}

func runTestWebhook(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	if len(cfg.Webhooks) == 0 {
		return fmt.Errorf("no webhooks configured in %s", cfgFile)
	}

	slog.Info("Sending test notification", "webhooks", len(cfg.Webhooks))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	event := notify.Event{
		Type:      notify.EventProbeDown,
		Probe:     "test-alert",
		ProbeType: "http",
		Message:   "Test alert - webhook pipeline validation",
		Details:   map[string]string{"error": "This is a test. No action required."},
		Timestamp: time.Now(),
	}

	var failed int
	for i, wc := range cfg.Webhooks {
		var sender notify.Sender
		switch wc.Type {
		case "discord":
			sender = notify.NewDiscordSender(wc.URL)
		case "slack":
			sender = notify.NewSlackSender(wc.URL)
		default:
			sender = notify.NewGenericSender(wc.URL)
		}

		if err := sender.Send(ctx, event); err != nil {
			slog.Error("Webhook failed", "index", i, "type", wc.Type, "error", err)
			failed++
		} else {
			slog.Info("Webhook delivered", "index", i, "type", wc.Type)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d webhooks failed", failed, len(cfg.Webhooks))
	}

	slog.Info("All webhooks delivered successfully")
	return nil
}
