package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"

	"github.com/hemantkpr/mailmate/internal/config"
	"github.com/hemantkpr/mailmate/internal/domain"
)

// Gemini implements IntentParser using Google Gemini API.
type Gemini struct {
	client *genai.Client
	model  string
}

// NewGemini creates a Gemini intent parser.
func NewGemini(cfg config.GeminiConfig) (*Gemini, error) {
	client, err := genai.NewClient(context.Background(), option.WithAPIKey(cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}
	return &Gemini{
		client: client,
		model:  cfg.Model,
	}, nil
}

// Close closes the Gemini client.
func (g *Gemini) Close() {
	if g.client != nil {
		g.client.Close()
	}
}

const systemPrompt = `You are MailMate, an intelligent WhatsApp assistant that helps users manage their emails, calendar, and personal tracking goals.

Your job is to understand the user's message and classify it into one of these intents:
- connect_email: User wants to connect their email account (Gmail or Outlook)
- disconnect_email: User wants to disconnect an email account
- list_emails: User wants to see recent emails
- list_meetings: User wants to see upcoming meetings/events
- reschedule_meeting: User wants to move/reschedule a meeting to a different time
- create_meeting: User wants to create a new meeting/event
- cancel_meeting: User wants to cancel/delete a meeting
- start_tracking: User wants to start tracking something (gym, habits, goals, etc.)
- log_tracking: User wants to log progress on a tracked item
- view_tracking: User wants to see their tracking progress
- stop_tracking: User wants to stop tracking something
- daily_summary: User wants a summary of their day (emails + meetings)
- help: User needs help or wants to know what you can do
- unknown: Cannot determine intent

You MUST respond with ONLY valid JSON (no markdown, no code fences) in this exact format:
{
  "intent": "<one of the intents above>",
  "confidence": <0.0 to 1.0>,
  "entities": {
    "provider": "google or microsoft (if mentioned)",
    "person_name": "name if mentioned",
    "meeting_subject": "meeting title if mentioned",
    "new_time": "time reference if mentioned (e.g. 'tomorrow 3pm')",
    "duration_minutes": 60,
    "tracking_subject": "what to track if mentioned",
    "tracking_duration_days": 90,
    "tracking_notes": "notes for tracking entry",
    "tracking_completed": true,
    "date": "date reference",
    "attendees": ["email1@example.com"],
    "location": "location if mentioned"
  }
}

Only include entity fields that are relevant. Always extract relevant entities from the message. Be smart about interpreting natural language.`

// ParseIntent uses Gemini to extract structured intent from user messages.
func (g *Gemini) ParseIntent(ctx context.Context, message string, history []domain.ConversationMessage) (*domain.ParsedIntent, error) {
	model := g.client.GenerativeModel(g.model)
	model.SystemInstruction = genai.NewUserContent(genai.Text(systemPrompt))
	model.ResponseMIMEType = "application/json"

	cs := model.StartChat()

	// Add conversation history for context
	for _, h := range history {
		role := "user"
		if h.Role == "assistant" {
			role = "model"
		}
		cs.History = append(cs.History, &genai.Content{
			Parts: []genai.Part{genai.Text(h.Message)},
			Role:  role,
		})
	}

	resp, err := cs.SendMessage(ctx, genai.Text(message))
	if err != nil {
		return nil, fmt.Errorf("gemini chat: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return &domain.ParsedIntent{
			Intent:     domain.IntentUnknown,
			RawMessage: message,
			Entities:   make(map[string]interface{}),
		}, nil
	}

	text := fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])

	var result struct {
		Intent     string                 `json:"intent"`
		Confidence float64                `json:"confidence"`
		Entities   map[string]interface{} `json:"entities"`
	}

	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return &domain.ParsedIntent{
			Intent:     domain.IntentUnknown,
			RawMessage: message,
			Entities:   make(map[string]interface{}),
		}, nil
	}

	return &domain.ParsedIntent{
		Intent:     mapIntent(result.Intent),
		Confidence: result.Confidence,
		Entities:   result.Entities,
		RawMessage: message,
	}, nil
}

// GenerateResponse generates a natural language response for the user.
func (g *Gemini) GenerateResponse(ctx context.Context, systemCtx, userMessage string) (string, error) {
	model := g.client.GenerativeModel(g.model)
	model.SystemInstruction = genai.NewUserContent(genai.Text(systemCtx))

	resp, err := model.GenerateContent(ctx, genai.Text(userMessage))
	if err != nil {
		return "", fmt.Errorf("gemini generate: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "I'm sorry, I couldn't generate a response.", nil
	}
	return strings.TrimSpace(fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])), nil
}

func mapIntent(s string) domain.Intent {
	intents := map[string]domain.Intent{
		"connect_email":      domain.IntentConnectEmail,
		"disconnect_email":   domain.IntentDisconnectEmail,
		"list_emails":        domain.IntentListEmails,
		"list_meetings":      domain.IntentListMeetings,
		"reschedule_meeting": domain.IntentRescheduleMeeting,
		"create_meeting":     domain.IntentCreateMeeting,
		"cancel_meeting":     domain.IntentCancelMeeting,
		"start_tracking":     domain.IntentStartTracking,
		"log_tracking":       domain.IntentLogTracking,
		"view_tracking":      domain.IntentViewTracking,
		"stop_tracking":      domain.IntentStopTracking,
		"daily_summary":      domain.IntentDailySummary,
		"help":               domain.IntentHelp,
	}
	if intent, ok := intents[s]; ok {
		return intent
	}
	return domain.IntentUnknown
}
