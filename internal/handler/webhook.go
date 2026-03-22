package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/hemantkpr/mailmate/internal/service"
)

// Webhook handles incoming WhatsApp messages from Twilio.
type Webhook struct {
	chatbot *service.Chatbot
	logger  *zap.Logger
}

// NewWebhook creates a Webhook handler.
func NewWebhook(chatbot *service.Chatbot, logger *zap.Logger) *Webhook {
	return &Webhook{chatbot: chatbot, logger: logger}
}

// HandleIncoming processes incoming WhatsApp webhook messages.
func (w *Webhook) HandleIncoming(c *gin.Context) {
	body := c.PostForm("Body")
	from := c.PostForm("From")

	if from == "" || body == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	// Normalize phone number (Twilio sends "whatsapp:+1234567890")
	phone := from
	if !strings.HasPrefix(phone, "whatsapp:") {
		phone = "whatsapp:" + phone
	}

	body = strings.TrimSpace(body)

	// Process asynchronously — use background context since the request
	// context is canceled after we return the response.
	go w.chatbot.HandleMessage(context.Background(), phone, body)

	// Return empty TwiML to acknowledge receipt
	c.Header("Content-Type", "text/xml")
	c.String(http.StatusOK, "<?xml version=\"1.0\" encoding=\"UTF-8\"?><Response></Response>")
}
