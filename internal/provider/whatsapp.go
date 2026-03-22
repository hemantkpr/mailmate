package provider

import (
	"context"
	"fmt"

	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"

	twilio "github.com/twilio/twilio-go"

	"github.com/hemantkpr/mailmate/internal/config"
)

// WhatsApp implements MessageSender using Twilio's WhatsApp API.
type WhatsApp struct {
	client *twilio.RestClient
	from   string
}

// NewWhatsApp creates a WhatsApp message sender.
func NewWhatsApp(cfg config.TwilioConfig) *WhatsApp {
	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: cfg.AccountSID,
		Password: cfg.AuthToken,
	})
	return &WhatsApp{
		client: client,
		from:   cfg.WhatsAppFrom,
	}
}

// SendMessage sends a WhatsApp message via Twilio.
func (w *WhatsApp) SendMessage(ctx context.Context, to, message string) error {
	chunks := splitMessage(message, 1500)
	for _, chunk := range chunks {
		params := &twilioApi.CreateMessageParams{}
		params.SetFrom(w.from)
		params.SetTo(to)
		params.SetBody(chunk)

		_, err := w.client.Api.CreateMessage(params)
		if err != nil {
			return fmt.Errorf("send whatsapp message: %w", err)
		}
	}
	return nil
}

func splitMessage(msg string, maxLen int) []string {
	if len(msg) <= maxLen {
		return []string{msg}
	}

	var chunks []string
	runes := []rune(msg)
	for len(runes) > 0 {
		end := maxLen
		if end > len(runes) {
			end = len(runes)
		}
		// Try to split at a newline or space
		if end < len(runes) {
			for i := end; i > end-200 && i > 0; i-- {
				if runes[i] == '\n' || runes[i] == ' ' {
					end = i + 1
					break
				}
			}
		}
		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}
