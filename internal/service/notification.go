package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/hemantkpr/mailmate/internal/domain"
	"github.com/hemantkpr/mailmate/internal/provider"
)

// Notification handles scheduled notifications (daily summaries, meeting reminders, tracker reminders).
type Notification struct {
	auth         *Auth
	tracker      *Tracker
	google       *provider.Google
	microsoft    *provider.Microsoft
	messenger    domain.MessageSender
	users        domain.UserRepository
	tokens       domain.TokenRepository
	prefs        domain.PreferenceRepository
	conversation domain.ConversationRepository
	cron         *cron.Cron
	logger       *zap.Logger
}

// NewNotification creates a Notification service.
func NewNotification(
	auth *Auth,
	tracker *Tracker,
	google *provider.Google,
	microsoft *provider.Microsoft,
	messenger domain.MessageSender,
	users domain.UserRepository,
	tokens domain.TokenRepository,
	prefs domain.PreferenceRepository,
	conversation domain.ConversationRepository,
	logger *zap.Logger,
) *Notification {
	return &Notification{
		auth:         auth,
		tracker:      tracker,
		google:       google,
		microsoft:    microsoft,
		messenger:    messenger,
		users:        users,
		tokens:       tokens,
		prefs:        prefs,
		conversation: conversation,
		cron:         cron.New(),
		logger:       logger,
	}
}

// Start begins the cron scheduler for notifications.
func (n *Notification) Start() error {
	// Daily summary at 8 AM UTC
	_, err := n.cron.AddFunc("0 8 * * *", func() {
		n.sendDailySummaries()
	})
	if err != nil {
		return fmt.Errorf("add daily summary cron: %w", err)
	}

	// Meeting reminders every 5 minutes
	_, err = n.cron.AddFunc("*/5 * * * *", func() {
		n.sendMeetingReminders()
	})
	if err != nil {
		return fmt.Errorf("add meeting reminder cron: %w", err)
	}

	// Tracking reminders at 6 PM UTC
	_, err = n.cron.AddFunc("0 18 * * *", func() {
		n.sendTrackingReminders()
	})
	if err != nil {
		return fmt.Errorf("add tracking reminder cron: %w", err)
	}

	// Clean old conversation history daily at 3 AM
	_, err = n.cron.AddFunc("0 3 * * *", func() {
		ctx := context.Background()
		cutoff := time.Now().Add(-7 * 24 * time.Hour)
		if err := n.conversation.CleanOldMessages(ctx, cutoff); err != nil {
			n.logger.Error("clean old messages", zap.Error(err))
		}
	})
	if err != nil {
		return fmt.Errorf("add cleanup cron: %w", err)
	}

	n.cron.Start()
	n.logger.Info("notification scheduler started")
	return nil
}

// Stop shuts down the cron scheduler.
func (n *Notification) Stop() {
	ctx := n.cron.Stop()
	<-ctx.Done()
}

func (n *Notification) sendDailySummaries() {
	ctx := context.Background()

	// Get all users with Google tokens
	googleTokens, err := n.tokens.GetTokensByProvider(ctx, domain.ProviderGoogle)
	if err != nil {
		n.logger.Error("get google tokens for daily summary", zap.Error(err))
	}

	msTokens, err := n.tokens.GetTokensByProvider(ctx, domain.ProviderMicrosoft)
	if err != nil {
		n.logger.Error("get microsoft tokens for daily summary", zap.Error(err))
	}

	// Merge unique user IDs
	userIDs := make(map[string]bool)
	for _, t := range googleTokens {
		userIDs[t.UserID] = true
	}
	for _, t := range msTokens {
		userIDs[t.UserID] = true
	}

	for userID := range userIDs {
		n.sendDailySummaryForUser(ctx, userID)
	}
}

func (n *Notification) sendDailySummaryForUser(ctx context.Context, userID string) {
	user, err := n.users.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return
	}

	var parts []string

	// Unread email count
	totalUnread := 0
	if token, _ := n.auth.GetValidToken(ctx, userID, domain.ProviderGoogle); token != nil {
		if count, err := n.google.GetUnreadCount(ctx, token); err == nil {
			totalUnread += count
		}
	}
	if token, _ := n.auth.GetValidToken(ctx, userID, domain.ProviderMicrosoft); token != nil {
		if count, err := n.microsoft.GetUnreadCount(ctx, token); err == nil {
			totalUnread += count
		}
	}
	parts = append(parts, fmt.Sprintf("📧 *%d unread emails*", totalUnread))

	// Today's meetings
	var events []domain.CalendarEvent
	if token, _ := n.auth.GetValidToken(ctx, userID, domain.ProviderGoogle); token != nil {
		if evts, err := n.google.GetUpcomingEvents(ctx, token, 24*time.Hour); err == nil {
			events = append(events, evts...)
		}
	}
	if token, _ := n.auth.GetValidToken(ctx, userID, domain.ProviderMicrosoft); token != nil {
		if evts, err := n.microsoft.GetUpcomingEvents(ctx, token, 24*time.Hour); err == nil {
			events = append(events, evts...)
		}
	}

	if len(events) > 0 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📅 *%d meetings today:*\n", len(events)))
		for _, e := range events {
			sb.WriteString(fmt.Sprintf("  • %s at %s\n", e.Title, e.StartTime.Format("3:04 PM")))
		}
		parts = append(parts, sb.String())
	} else {
		parts = append(parts, "📅 No meetings today")
	}

	// Tracking
	if progress, err := n.tracker.GetProgress(ctx, userID); err == nil && progress != "" {
		parts = append(parts, progress)
	}

	msg := "☀️ *Good Morning! Here's your daily summary:*\n\n" + strings.Join(parts, "\n\n")
	if err := n.messenger.SendMessage(ctx, user.PhoneNumber, msg); err != nil {
		n.logger.Error("send daily summary", zap.String("user_id", userID), zap.Error(err))
	}
}

func (n *Notification) sendMeetingReminders() {
	ctx := context.Background()

	googleTokens, err := n.tokens.GetTokensByProvider(ctx, domain.ProviderGoogle)
	if err != nil {
		n.logger.Error("get tokens for reminders", zap.Error(err))
		return
	}

	for _, token := range googleTokens {
		n.checkMeetingReminder(ctx, &token, n.google)
	}

	msTokens, err := n.tokens.GetTokensByProvider(ctx, domain.ProviderMicrosoft)
	if err != nil {
		n.logger.Error("get ms tokens for reminders", zap.Error(err))
		return
	}

	for _, token := range msTokens {
		n.checkMeetingReminder(ctx, &token, n.microsoft)
	}
}

func (n *Notification) checkMeetingReminder(ctx context.Context, token *domain.OAuthToken, cal domain.CalendarProvider) {
	// Get events in the next 20 minutes
	events, err := cal.GetUpcomingEvents(ctx, token, 20*time.Minute)
	if err != nil {
		return
	}

	user, err := n.users.GetUserByID(ctx, token.UserID)
	if err != nil || user == nil {
		return
	}

	for _, event := range events {
		minutesUntil := int(time.Until(event.StartTime).Minutes())
		if minutesUntil >= 10 && minutesUntil <= 15 {
			msg := fmt.Sprintf("⏰ *Meeting in %d minutes!*\n\n🗓 *%s*\n⏰ %s",
				minutesUntil, event.Title, event.StartTime.Format("3:04 PM"))
			if event.Location != "" {
				msg += fmt.Sprintf("\n📍 %s", event.Location)
			}
			if err := n.messenger.SendMessage(ctx, user.PhoneNumber, msg); err != nil {
				n.logger.Error("send reminder", zap.Error(err))
			}
		}
	}
}

func (n *Notification) sendTrackingReminders() {
	ctx := context.Background()

	googleTokens, _ := n.tokens.GetTokensByProvider(ctx, domain.ProviderGoogle)
	msTokens, _ := n.tokens.GetTokensByProvider(ctx, domain.ProviderMicrosoft)

	userIDs := make(map[string]bool)
	for _, t := range googleTokens {
		userIDs[t.UserID] = true
	}
	for _, t := range msTokens {
		userIDs[t.UserID] = true
	}

	for userID := range userIDs {
		user, err := n.users.GetUserByID(ctx, userID)
		if err != nil || user == nil {
			continue
		}

		dueItems, err := n.tracker.GetDueReminders(ctx, userID)
		if err != nil || len(dueItems) == 0 {
			continue
		}

		var sb strings.Builder
		sb.WriteString("💪 *Daily Tracking Reminder:*\n\n")
		for _, item := range dueItems {
			sb.WriteString(fmt.Sprintf("• *%s* — No entry yet today!\n", item.Title))
		}
		sb.WriteString("\n_Reply with your progress to log it._")

		if err := n.messenger.SendMessage(ctx, user.PhoneNumber, sb.String()); err != nil {
			n.logger.Error("send tracking reminder", zap.Error(err))
		}
	}
}
