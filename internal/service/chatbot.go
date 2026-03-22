package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/hemantkpr/mailmate/internal/domain"
	"github.com/hemantkpr/mailmate/internal/provider"
)

// Chatbot orchestrates message handling and intent routing.
type Chatbot struct {
	auth         *Auth
	tracker      *Tracker
	google       *provider.Google
	microsoft    *provider.Microsoft
	messenger    domain.MessageSender
	parser       *provider.OpenAI
	users        domain.UserRepository
	tokens       domain.TokenRepository
	conversation domain.ConversationRepository
	logger       *zap.Logger
}

// NewChatbot creates a Chatbot service.
func NewChatbot(
	auth *Auth,
	tracker *Tracker,
	google *provider.Google,
	microsoft *provider.Microsoft,
	messenger domain.MessageSender,
	parser *provider.OpenAI,
	users domain.UserRepository,
	tokens domain.TokenRepository,
	conversation domain.ConversationRepository,
	logger *zap.Logger,
) *Chatbot {
	return &Chatbot{
		auth:         auth,
		tracker:      tracker,
		google:       google,
		microsoft:    microsoft,
		messenger:    messenger,
		parser:       parser,
		users:        users,
		tokens:       tokens,
		conversation: conversation,
		logger:       logger,
	}
}

// HandleMessage processes an incoming WhatsApp message.
func (c *Chatbot) HandleMessage(ctx context.Context, phone, message string) {
	c.logger.Info("incoming message", zap.String("phone", phone), zap.String("message", message))

	// Get or create user
	user, err := c.users.GetUserByPhone(ctx, phone)
	if err != nil {
		c.logger.Error("get user failed", zap.Error(err))
		c.sendError(ctx, phone)
		return
	}

	if user == nil {
		user = &domain.User{
			PhoneNumber: phone,
			Name:        "",
			Timezone:    "UTC",
		}
		if err := c.users.CreateUser(ctx, user); err != nil {
			c.logger.Error("create user failed", zap.Error(err))
			c.sendError(ctx, phone)
			return
		}
		// Welcome new user
		welcome := "👋 *Welcome to MailMate!*\n\n" +
			"I help you manage your emails, calendar, and personal goals — all from WhatsApp.\n\n" +
			"To get started, connect your email account. Type *\"connect email\"* or *\"help\"* for more options."
		c.send(ctx, phone, welcome)

		c.saveMessage(ctx, user.ID, "user", message)
		c.saveMessage(ctx, user.ID, "assistant", welcome)
		return
	}

	// Save incoming message
	c.saveMessage(ctx, user.ID, "user", message)

	// Get conversation history for context
	history, err := c.conversation.GetRecentMessages(ctx, user.ID, 10)
	if err != nil {
		c.logger.Warn("get history failed", zap.Error(err))
		history = nil
	}

	// Parse intent
	parsed, err := c.parser.ParseIntent(ctx, message, history)
	if err != nil {
		c.logger.Error("parse intent failed", zap.Error(err))
		c.send(ctx, phone, "Sorry, I had trouble understanding that. Please try again or type *help*.")
		return
	}

	c.logger.Info("parsed intent",
		zap.String("intent", string(parsed.Intent)),
		zap.Float64("confidence", parsed.Confidence))

	// Route to handler
	response := c.routeIntent(ctx, user, parsed)
	c.send(ctx, phone, response)
	c.saveMessage(ctx, user.ID, "assistant", response)
}

func (c *Chatbot) routeIntent(ctx context.Context, user *domain.User, intent *domain.ParsedIntent) string {
	switch intent.Intent {
	case domain.IntentConnectEmail:
		return c.handleConnectEmail(ctx, user)
	case domain.IntentDisconnectEmail:
		return c.handleDisconnectEmail(ctx, user, intent)
	case domain.IntentListEmails:
		return c.handleListEmails(ctx, user)
	case domain.IntentListMeetings:
		return c.handleListMeetings(ctx, user)
	case domain.IntentRescheduleMeeting:
		return c.handleRescheduleMeeting(ctx, user, intent)
	case domain.IntentCreateMeeting:
		return c.handleCreateMeeting(ctx, user, intent)
	case domain.IntentCancelMeeting:
		return c.handleCancelMeeting(ctx, user, intent)
	case domain.IntentStartTracking:
		return c.handleStartTracking(ctx, user, intent)
	case domain.IntentLogTracking:
		return c.handleLogTracking(ctx, user, intent)
	case domain.IntentViewTracking:
		return c.handleViewTracking(ctx, user)
	case domain.IntentStopTracking:
		return c.handleStopTracking(ctx, user, intent)
	case domain.IntentDailySummary:
		return c.handleDailySummary(ctx, user)
	case domain.IntentHelp:
		return c.handleHelp()
	default:
		return c.handleUnknown(ctx, intent)
	}
}

func (c *Chatbot) handleConnectEmail(ctx context.Context, user *domain.User) string {
	msg, err := c.auth.GenerateConnectLinks(ctx, user.PhoneNumber)
	if err != nil {
		c.logger.Error("generate connect links", zap.Error(err))
		return "Sorry, I couldn't generate connection links. Please try again."
	}
	return msg
}

func (c *Chatbot) handleDisconnectEmail(ctx context.Context, user *domain.User, intent *domain.ParsedIntent) string {
	prov := c.extractProvider(intent)
	if prov == "" {
		// Disconnect all
		_ = c.auth.DisconnectProvider(ctx, user.ID, domain.ProviderGoogle)
		_ = c.auth.DisconnectProvider(ctx, user.ID, domain.ProviderMicrosoft)
		return "✅ All email accounts have been disconnected."
	}
	if err := c.auth.DisconnectProvider(ctx, user.ID, domain.Provider(prov)); err != nil {
		return "Sorry, I couldn't disconnect that account."
	}
	return fmt.Sprintf("✅ %s account disconnected.", strings.Title(prov))
}

func (c *Chatbot) handleListEmails(ctx context.Context, user *domain.User) string {
	var allEmails []domain.Email

	// Try Google
	if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderGoogle); token != nil {
		emails, err := c.google.GetRecentEmails(ctx, token, 5)
		if err != nil {
			c.logger.Warn("get google emails", zap.Error(err))
		} else {
			allEmails = append(allEmails, emails...)
		}
	}

	// Try Microsoft
	if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderMicrosoft); token != nil {
		emails, err := c.microsoft.GetRecentEmails(ctx, token, 5)
		if err != nil {
			c.logger.Warn("get microsoft emails", zap.Error(err))
		} else {
			allEmails = append(allEmails, emails...)
		}
	}

	if len(allEmails) == 0 {
		hasProviders, _ := c.auth.HasConnectedProviders(ctx, user.ID)
		if !hasProviders {
			return "You haven't connected any email accounts yet. Type *\"connect email\"* to get started."
		}
		return "📭 No recent emails found."
	}

	var sb strings.Builder
	sb.WriteString("📧 *Recent Emails:*\n\n")
	for i, email := range allEmails {
		if i >= 10 {
			break
		}
		readIcon := "📩"
		if email.IsRead {
			readIcon = "📨"
		}
		sb.WriteString(fmt.Sprintf("%s *%s*\nFrom: %s\n_%s_\n\n",
			readIcon, email.Subject, email.From, email.Snippet))
	}
	return sb.String()
}

func (c *Chatbot) handleListMeetings(ctx context.Context, user *domain.User) string {
	var allEvents []domain.CalendarEvent

	// Try Google Calendar
	if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderGoogle); token != nil {
		events, err := c.google.GetUpcomingEvents(ctx, token, 24*time.Hour)
		if err != nil {
			c.logger.Warn("get google events", zap.Error(err))
		} else {
			allEvents = append(allEvents, events...)
		}
	}

	// Try Microsoft Calendar
	if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderMicrosoft); token != nil {
		events, err := c.microsoft.GetUpcomingEvents(ctx, token, 24*time.Hour)
		if err != nil {
			c.logger.Warn("get microsoft events", zap.Error(err))
		} else {
			allEvents = append(allEvents, events...)
		}
	}

	if len(allEvents) == 0 {
		hasProviders, _ := c.auth.HasConnectedProviders(ctx, user.ID)
		if !hasProviders {
			return "You haven't connected any calendar accounts yet. Type *\"connect email\"* to link your account."
		}
		return "📅 No upcoming meetings in the next 24 hours."
	}

	var sb strings.Builder
	sb.WriteString("📅 *Upcoming Meetings (next 24h):*\n\n")
	for _, event := range allEvents {
		timeStr := event.StartTime.Format("3:04 PM") + " - " + event.EndTime.Format("3:04 PM")
		sb.WriteString(fmt.Sprintf("🗓 *%s*\n⏰ %s\n", event.Title, timeStr))
		if event.Location != "" {
			sb.WriteString(fmt.Sprintf("📍 %s\n", event.Location))
		}
		if len(event.Attendees) > 0 {
			sb.WriteString(fmt.Sprintf("👥 %s\n", strings.Join(event.Attendees, ", ")))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (c *Chatbot) handleRescheduleMeeting(ctx context.Context, user *domain.User, intent *domain.ParsedIntent) string {
	subject := getStringEntity(intent, "meeting_subject")
	personName := getStringEntity(intent, "person_name")
	newTimeStr := getStringEntity(intent, "new_time")

	if subject == "" && personName == "" {
		return "I need more details. Which meeting would you like to reschedule? Please include the meeting name or person."
	}

	searchQuery := subject
	if searchQuery == "" {
		searchQuery = personName
	}

	// Search across providers
	var event *domain.CalendarEvent
	var token *domain.OAuthToken
	var calProvider domain.CalendarProvider

	// Try Google
	if t, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderGoogle); t != nil {
		if e, err := c.google.FindEvent(ctx, t, searchQuery); err == nil && e != nil {
			event = e
			token = t
			calProvider = c.google
		}
	}

	// Try Microsoft if not found
	if event == nil {
		if t, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderMicrosoft); t != nil {
			if e, err := c.microsoft.FindEvent(ctx, t, searchQuery); err == nil && e != nil {
				event = e
				token = t
				calProvider = c.microsoft
			}
		}
	}

	if event == nil {
		return fmt.Sprintf("I couldn't find a meeting matching \"%s\". Could you provide more details?", searchQuery)
	}

	// Parse new time using OpenAI
	if newTimeStr == "" {
		return fmt.Sprintf("I found *%s* at %s. What time would you like to move it to?",
			event.Title, event.StartTime.Format("Mon Jan 2, 3:04 PM"))
	}

	newTime, err := c.parseTimeReference(ctx, newTimeStr, event.StartTime)
	if err != nil {
		return "I couldn't understand the new time. Please specify like \"tomorrow 3pm\" or \"Monday at 2:30 PM\"."
	}

	duration := event.EndTime.Sub(event.StartTime)
	event.StartTime = newTime
	event.EndTime = newTime.Add(duration)

	if err := calProvider.UpdateEvent(ctx, token, event); err != nil {
		c.logger.Error("update event", zap.Error(err))
		return "Sorry, I couldn't update the meeting. Please try again."
	}

	return fmt.Sprintf("✅ *Meeting rescheduled!*\n\n🗓 *%s*\n⏰ %s - %s",
		event.Title,
		event.StartTime.Format("Mon Jan 2, 3:04 PM"),
		event.EndTime.Format("3:04 PM"))
}

func (c *Chatbot) handleCreateMeeting(ctx context.Context, user *domain.User, intent *domain.ParsedIntent) string {
	subject := getStringEntity(intent, "meeting_subject")
	timeStr := getStringEntity(intent, "new_time")
	durationMin := getIntEntity(intent, "duration_minutes")
	location := getStringEntity(intent, "location")

	if subject == "" {
		return "What should the meeting be about? Please provide a title."
	}
	if timeStr == "" {
		return "When should the meeting be? Please provide a time."
	}

	meetingTime, err := c.parseTimeReference(ctx, timeStr, time.Now())
	if err != nil {
		return "I couldn't understand the time. Please specify like \"tomorrow 3pm\" or \"Friday at 10 AM\"."
	}

	if durationMin == 0 {
		durationMin = 30
	}

	event := &domain.CalendarEvent{
		Title:     subject,
		StartTime: meetingTime,
		EndTime:   meetingTime.Add(time.Duration(durationMin) * time.Minute),
		Location:  location,
	}

	// Use first available provider
	if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderGoogle); token != nil {
		if err := c.google.CreateEvent(ctx, token, event); err != nil {
			c.logger.Error("create google event", zap.Error(err))
			return "Sorry, I couldn't create the meeting."
		}
	} else if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderMicrosoft); token != nil {
		if err := c.microsoft.CreateEvent(ctx, token, event); err != nil {
			c.logger.Error("create microsoft event", zap.Error(err))
			return "Sorry, I couldn't create the meeting."
		}
	} else {
		return "You need to connect a calendar first. Type *\"connect email\"*."
	}

	return fmt.Sprintf("✅ *Meeting created!*\n\n🗓 *%s*\n⏰ %s - %s",
		event.Title,
		event.StartTime.Format("Mon Jan 2, 3:04 PM"),
		event.EndTime.Format("3:04 PM"))
}

func (c *Chatbot) handleCancelMeeting(ctx context.Context, user *domain.User, intent *domain.ParsedIntent) string {
	subject := getStringEntity(intent, "meeting_subject")
	personName := getStringEntity(intent, "person_name")

	searchQuery := subject
	if searchQuery == "" {
		searchQuery = personName
	}
	if searchQuery == "" {
		return "Which meeting would you like to cancel?"
	}

	// Search for the meeting
	if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderGoogle); token != nil {
		if event, _ := c.google.FindEvent(ctx, token, searchQuery); event != nil {
			if err := c.google.DeleteEvent(ctx, token, event.ID); err == nil {
				return fmt.Sprintf("✅ Meeting *\"%s\"* has been cancelled.", event.Title)
			}
		}
	}

	if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderMicrosoft); token != nil {
		if event, _ := c.microsoft.FindEvent(ctx, token, searchQuery); event != nil {
			if err := c.microsoft.DeleteEvent(ctx, token, event.ID); err == nil {
				return fmt.Sprintf("✅ Meeting *\"%s\"* has been cancelled.", event.Title)
			}
		}
	}

	return fmt.Sprintf("I couldn't find a meeting matching \"%s\".", searchQuery)
}

func (c *Chatbot) handleStartTracking(ctx context.Context, user *domain.User, intent *domain.ParsedIntent) string {
	subject := getStringEntity(intent, "tracking_subject")
	days := getIntEntity(intent, "tracking_duration_days")

	if subject == "" {
		return "What would you like to track? For example: \"Track my gym progress for 90 days\""
	}
	if days == 0 {
		days = 30 // Default to 30 days
	}

	item, err := c.tracker.StartTracking(ctx, user.ID, subject, "", days)
	if err != nil {
		c.logger.Error("start tracking", zap.Error(err))
		return "Sorry, I couldn't set up tracking."
	}

	return fmt.Sprintf("✅ *Tracking started!*\n\n📊 *%s*\n📅 %s to %s (%d days)\n\n"+
		"I'll send you daily reminders. Log progress by saying:\n"+
		"_\"Did gym today\"_ or _\"Log: chest and arms workout\"_",
		item.Title,
		item.StartDate.Format("Jan 2"),
		item.EndDate.Format("Jan 2"),
		days)
}

func (c *Chatbot) handleLogTracking(ctx context.Context, user *domain.User, intent *domain.ParsedIntent) string {
	notes := getStringEntity(intent, "tracking_notes")
	completed := getBoolEntity(intent, "tracking_completed")

	err := c.tracker.LogEntry(ctx, user.ID, notes, completed)
	if err != nil {
		c.logger.Error("log tracking", zap.Error(err))
		return "Sorry, I couldn't log that. Make sure you have an active tracking goal."
	}

	if completed {
		return "✅ Logged! Great job today! 💪"
	}
	return "✅ Entry logged."
}

func (c *Chatbot) handleViewTracking(ctx context.Context, user *domain.User) string {
	summary, err := c.tracker.GetProgress(ctx, user.ID)
	if err != nil {
		c.logger.Error("view tracking", zap.Error(err))
		return "Sorry, I couldn't retrieve your tracking data."
	}
	return summary
}

func (c *Chatbot) handleStopTracking(ctx context.Context, user *domain.User, intent *domain.ParsedIntent) string {
	subject := getStringEntity(intent, "tracking_subject")
	if err := c.tracker.StopTracking(ctx, user.ID, subject); err != nil {
		return "Sorry, I couldn't stop that tracking goal."
	}
	return "✅ Tracking stopped."
}

func (c *Chatbot) handleDailySummary(ctx context.Context, user *domain.User) string {
	var parts []string

	// Emails summary
	emailSummary := c.getEmailSummary(ctx, user)
	if emailSummary != "" {
		parts = append(parts, emailSummary)
	}

	// Calendar summary
	meetingSummary := c.handleListMeetings(ctx, user)
	parts = append(parts, meetingSummary)

	// Tracking summary
	trackingSummary, _ := c.tracker.GetProgress(ctx, user.ID)
	if trackingSummary != "" {
		parts = append(parts, trackingSummary)
	}

	if len(parts) == 0 {
		return "Connect your email first to get daily summaries. Type *\"connect email\"*."
	}

	return "📋 *Your Daily Summary:*\n\n" + strings.Join(parts, "\n---\n\n")
}

func (c *Chatbot) handleHelp() string {
	return `🤖 *MailMate Help*

Here's what I can do:

📧 *Email*
• "Connect email" — Link Gmail or Outlook
• "Show my emails" — View recent emails
• "Disconnect email" — Unlink account

📅 *Calendar*
• "What meetings do I have today?" — View upcoming events
• "Reschedule meeting with John to tomorrow 3pm"
• "Create a meeting: Team sync at 2pm Friday"
• "Cancel my meeting with Sarah"

📊 *Track Goals*
• "Track my gym progress for 90 days"
• "Did gym today" or "Log: ran 5km"
• "Show my tracking progress"
• "Stop tracking gym"

📋 *Summary*
• "Daily summary" — Overview of emails, meetings & goals

_Just type naturally — I'll understand!_`
}

func (c *Chatbot) handleUnknown(ctx context.Context, intent *domain.ParsedIntent) string {
	// Use OpenAI to generate a helpful response
	resp, err := c.parser.GenerateResponse(ctx,
		"You are MailMate, a WhatsApp assistant for email, calendar and goal tracking. The user sent a message you couldn't classify. Provide a helpful, brief response suggesting what you can help with.",
		intent.RawMessage)
	if err != nil {
		return "I'm not sure what you mean. Type *help* to see what I can do."
	}
	return resp
}

func (c *Chatbot) getEmailSummary(ctx context.Context, user *domain.User) string {
	totalUnread := 0

	if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderGoogle); token != nil {
		if count, err := c.google.GetUnreadCount(ctx, token); err == nil {
			totalUnread += count
		}
	}
	if token, _ := c.auth.GetValidToken(ctx, user.ID, domain.ProviderMicrosoft); token != nil {
		if count, err := c.microsoft.GetUnreadCount(ctx, token); err == nil {
			totalUnread += count
		}
	}

	if totalUnread > 0 {
		return fmt.Sprintf("📧 You have *%d unread emails*.", totalUnread)
	}
	return "📧 No unread emails."
}

func (c *Chatbot) parseTimeReference(ctx context.Context, timeStr string, reference time.Time) (time.Time, error) {
	prompt := fmt.Sprintf(
		"Parse this time reference into an ISO 8601 datetime string (YYYY-MM-DDTHH:MM:SS). "+
			"The current reference time is %s. The user said: \"%s\". "+
			"Return ONLY the ISO 8601 datetime string, nothing else.",
		reference.Format(time.RFC3339), timeStr)

	resp, err := c.parser.GenerateResponse(ctx, "You are a datetime parser. Return only an ISO 8601 datetime string.", prompt)
	if err != nil {
		return time.Time{}, err
	}

	resp = strings.TrimSpace(resp)
	parsed, err := time.Parse("2006-01-02T15:04:05", resp)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, resp)
		if err != nil {
			return time.Time{}, fmt.Errorf("could not parse time: %s", resp)
		}
	}
	return parsed, nil
}

func (c *Chatbot) send(ctx context.Context, phone, msg string) {
	if err := c.messenger.SendMessage(ctx, phone, msg); err != nil {
		c.logger.Error("send message failed", zap.String("phone", phone), zap.Error(err))
	}
}

func (c *Chatbot) sendError(ctx context.Context, phone string) {
	c.send(ctx, phone, "Oops! Something went wrong. Please try again.")
}

func (c *Chatbot) saveMessage(ctx context.Context, userID, role, message string) {
	msg := &domain.ConversationMessage{
		UserID:  userID,
		Role:    role,
		Message: message,
	}
	if err := c.conversation.SaveMessage(ctx, msg); err != nil {
		c.logger.Warn("save message failed", zap.Error(err))
	}
}

// --- Entity Helpers ---

func getStringEntity(intent *domain.ParsedIntent, key string) string {
	if v, ok := intent.Entities[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getIntEntity(intent *domain.ParsedIntent, key string) int {
	if v, ok := intent.Entities[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

func getBoolEntity(intent *domain.ParsedIntent, key string) bool {
	if v, ok := intent.Entities[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true // Default to completed for log entries
}

func (c *Chatbot) extractProvider(intent *domain.ParsedIntent) string {
	return getStringEntity(intent, "provider")
}
