package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"github.com/hemantkpr/mailmate/internal/config"
	"github.com/hemantkpr/mailmate/internal/domain"
)

// OpenAI implements IntentParser using OpenAI function calling.
type OpenAI struct {
	client *openai.Client
	model  string
}

// NewOpenAI creates an OpenAI intent parser.
func NewOpenAI(cfg config.OpenAIConfig) *OpenAI {
	return &OpenAI{
		client: openai.NewClient(cfg.APIKey),
		model:  cfg.Model,
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

Always extract relevant entities from the message. Be smart about interpreting natural language.
For time references, interpret them relative to the current context (e.g., "tomorrow" means the next day).`

// ParseIntent uses OpenAI function calling to extract structured intent from user messages.
func (o *OpenAI) ParseIntent(ctx context.Context, message string, history []domain.ConversationMessage) (*domain.ParsedIntent, error) {
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
	}

	// Add recent conversation history for context
	for _, h := range history {
		role := openai.ChatMessageRoleUser
		if h.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    role,
			Content: h.Message,
		})
	}

	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: message,
	})

	resp, err := o.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    o.model,
		Messages: messages,
		Tools: []openai.Tool{
			{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        "classify_intent",
					Description: "Classify the user's message into an intent and extract entities",
					Parameters:  intentSchema(),
				},
			},
		},
		ToolChoice: openai.ToolChoice{
			Type: openai.ToolTypeFunction,
			Function: openai.ToolFunction{
				Name: "classify_intent",
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return &domain.ParsedIntent{
			Intent:     domain.IntentUnknown,
			RawMessage: message,
			Entities:   make(map[string]interface{}),
		}, nil
	}

	choice := resp.Choices[0]
	if len(choice.Message.ToolCalls) == 0 {
		return &domain.ParsedIntent{
			Intent:     domain.IntentUnknown,
			RawMessage: message,
			Entities:   make(map[string]interface{}),
		}, nil
	}

	var result struct {
		Intent     string                 `json:"intent"`
		Confidence float64                `json:"confidence"`
		Entities   map[string]interface{} `json:"entities"`
	}

	args := choice.Message.ToolCalls[0].Function.Arguments
	if err := json.Unmarshal([]byte(args), &result); err != nil {
		return nil, fmt.Errorf("parse function args: %w", err)
	}

	return &domain.ParsedIntent{
		Intent:     mapIntent(result.Intent),
		Confidence: result.Confidence,
		Entities:   result.Entities,
		RawMessage: message,
	}, nil
}

// GenerateResponse generates a natural language response for the user.
func (o *OpenAI) GenerateResponse(ctx context.Context, systemCtx, userMessage string) (string, error) {
	resp, err := o.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: o.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemCtx},
			{Role: openai.ChatMessageRoleUser, Content: userMessage},
		},
		MaxTokens:   500,
		Temperature: 0.7,
	})
	if err != nil {
		return "", fmt.Errorf("generate response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "I'm sorry, I couldn't generate a response.", nil
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func intentSchema() json.RawMessage {
	schema := `{
		"type": "object",
		"properties": {
			"intent": {
				"type": "string",
				"enum": [
					"connect_email", "disconnect_email", "list_emails", "list_meetings",
					"reschedule_meeting", "create_meeting", "cancel_meeting",
					"start_tracking", "log_tracking", "view_tracking", "stop_tracking",
					"daily_summary", "help", "unknown"
				],
				"description": "The classified intent of the user's message"
			},
			"confidence": {
				"type": "number",
				"minimum": 0,
				"maximum": 1,
				"description": "Confidence score for the classified intent"
			},
			"entities": {
				"type": "object",
				"properties": {
					"provider": {
						"type": "string",
						"enum": ["google", "microsoft"],
						"description": "Email provider (google for Gmail, microsoft for Outlook)"
					},
					"person_name": {
						"type": "string",
						"description": "Name of a person mentioned in the message"
					},
					"meeting_subject": {
						"type": "string",
						"description": "Subject or title of a meeting"
					},
					"new_time": {
						"type": "string",
						"description": "New time for rescheduling (ISO 8601 or natural language like 'tomorrow 3pm')"
					},
					"duration_minutes": {
						"type": "integer",
						"description": "Duration in minutes for a meeting"
					},
					"tracking_subject": {
						"type": "string",
						"description": "What the user wants to track (e.g., 'gym progress', 'meditation')"
					},
					"tracking_duration_days": {
						"type": "integer",
						"description": "Number of days for tracking"
					},
					"tracking_notes": {
						"type": "string",
						"description": "Notes for a tracking log entry"
					},
					"tracking_completed": {
						"type": "boolean",
						"description": "Whether the tracking entry is marked as completed"
					},
					"date": {
						"type": "string",
						"description": "A date reference in the message"
					},
					"attendees": {
						"type": "array",
						"items": {"type": "string"},
						"description": "List of attendee emails"
					},
					"location": {
						"type": "string",
						"description": "Location for a meeting"
					}
				},
				"description": "Extracted entities from the message"
			}
		},
		"required": ["intent", "confidence", "entities"]
	}`
	return json.RawMessage(schema)
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
