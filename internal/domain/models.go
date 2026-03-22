package domain

import (
	"context"
	"time"
)

// Provider represents an email/calendar provider.
type Provider string

const (
	ProviderGoogle    Provider = "google"
	ProviderMicrosoft Provider = "microsoft"
)

// Intent represents a parsed user intent from NLP.
type Intent string

const (
	IntentConnectEmail      Intent = "connect_email"
	IntentDisconnectEmail   Intent = "disconnect_email"
	IntentListEmails        Intent = "list_emails"
	IntentListMeetings      Intent = "list_meetings"
	IntentRescheduleMeeting Intent = "reschedule_meeting"
	IntentCreateMeeting     Intent = "create_meeting"
	IntentCancelMeeting     Intent = "cancel_meeting"
	IntentStartTracking     Intent = "start_tracking"
	IntentLogTracking       Intent = "log_tracking"
	IntentViewTracking      Intent = "view_tracking"
	IntentStopTracking      Intent = "stop_tracking"
	IntentDailySummary      Intent = "daily_summary"
	IntentHelp              Intent = "help"
	IntentUnknown           Intent = "unknown"
)

// User represents a registered user.
type User struct {
	ID          string    `json:"id" db:"id"`
	PhoneNumber string    `json:"phone_number" db:"phone_number"`
	Name        string    `json:"name" db:"name"`
	Timezone    string    `json:"timezone" db:"timezone"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// OAuthToken stores encrypted OAuth credentials.
type OAuthToken struct {
	ID           string    `json:"id" db:"id"`
	UserID       string    `json:"user_id" db:"user_id"`
	Provider     Provider  `json:"provider" db:"provider"`
	AccessToken  string    `json:"access_token" db:"access_token"`
	RefreshToken string    `json:"refresh_token" db:"refresh_token"`
	TokenExpiry  time.Time `json:"token_expiry" db:"token_expiry"`
	Scopes       string    `json:"scopes" db:"scopes"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// Email represents an email message.
type Email struct {
	ID      string    `json:"id"`
	From    string    `json:"from"`
	To      []string  `json:"to"`
	Subject string    `json:"subject"`
	Snippet string    `json:"snippet"`
	Date    time.Time `json:"date"`
	IsRead  bool      `json:"is_read"`
}

// CalendarEvent represents a calendar event.
type CalendarEvent struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Attendees   []string  `json:"attendees"`
	Provider    Provider  `json:"provider"`
}

// TrackedItem represents something being tracked over time.
type TrackedItem struct {
	ID          string    `json:"id" db:"id"`
	UserID      string    `json:"user_id" db:"user_id"`
	Title       string    `json:"title" db:"title"`
	Description string    `json:"description" db:"description"`
	StartDate   time.Time `json:"start_date" db:"start_date"`
	EndDate     time.Time `json:"end_date" db:"end_date"`
	Active      bool      `json:"active" db:"active"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// TrackedEntry represents a single log entry in a tracked item.
type TrackedEntry struct {
	ID            string    `json:"id" db:"id"`
	TrackedItemID string    `json:"tracked_item_id" db:"tracked_item_id"`
	EntryDate     time.Time `json:"entry_date" db:"entry_date"`
	Notes         string    `json:"notes" db:"notes"`
	Completed     bool      `json:"completed" db:"completed"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// NotificationPreference stores user notification settings.
type NotificationPreference struct {
	ID                     string    `json:"id" db:"id"`
	UserID                 string    `json:"user_id" db:"user_id"`
	MeetingReminderMinutes int       `json:"meeting_reminder_minutes" db:"meeting_reminder_minutes"`
	DailySummaryEnabled    bool      `json:"daily_summary_enabled" db:"daily_summary_enabled"`
	DailySummaryTime       string    `json:"daily_summary_time" db:"daily_summary_time"`
	EmailNotifications     bool      `json:"email_notifications" db:"email_notifications"`
	CreatedAt              time.Time `json:"created_at" db:"created_at"`
	UpdatedAt              time.Time `json:"updated_at" db:"updated_at"`
}

// ConversationMessage stores chat history for context.
type ConversationMessage struct {
	ID        string    `json:"id" db:"id"`
	UserID    string    `json:"user_id" db:"user_id"`
	Role      string    `json:"role" db:"role"`
	Message   string    `json:"message" db:"message"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ParsedIntent is the result of NLP processing.
type ParsedIntent struct {
	Intent     Intent                 `json:"intent"`
	Confidence float64                `json:"confidence"`
	Entities   map[string]interface{} `json:"entities"`
	RawMessage string                 `json:"raw_message"`
}

// --- Repository Interfaces ---

// UserRepository defines user data access operations.
type UserRepository interface {
	CreateUser(ctx context.Context, user *User) error
	GetUserByPhone(ctx context.Context, phone string) (*User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
	UpdateUser(ctx context.Context, user *User) error
}

// TokenRepository defines OAuth token data access.
type TokenRepository interface {
	SaveToken(ctx context.Context, token *OAuthToken) error
	GetToken(ctx context.Context, userID string, provider Provider) (*OAuthToken, error)
	DeleteToken(ctx context.Context, userID string, provider Provider) error
	GetTokensByProvider(ctx context.Context, provider Provider) ([]OAuthToken, error)
}

// TrackerRepository defines tracker data access.
type TrackerRepository interface {
	CreateTrackedItem(ctx context.Context, item *TrackedItem) error
	GetActiveTrackedItems(ctx context.Context, userID string) ([]TrackedItem, error)
	GetTrackedItemByID(ctx context.Context, id string) (*TrackedItem, error)
	DeactivateTrackedItem(ctx context.Context, id string) error
	CreateEntry(ctx context.Context, entry *TrackedEntry) error
	GetEntries(ctx context.Context, itemID string, start, end time.Time) ([]TrackedEntry, error)
	GetTodayEntry(ctx context.Context, itemID string) (*TrackedEntry, error)
}

// PreferenceRepository defines notification preference access.
type PreferenceRepository interface {
	GetPreferences(ctx context.Context, userID string) (*NotificationPreference, error)
	SavePreferences(ctx context.Context, pref *NotificationPreference) error
}

// ConversationRepository stores chat history.
type ConversationRepository interface {
	SaveMessage(ctx context.Context, msg *ConversationMessage) error
	GetRecentMessages(ctx context.Context, userID string, limit int) ([]ConversationMessage, error)
	CleanOldMessages(ctx context.Context, before time.Time) error
}

// --- Provider Interfaces ---

// EmailProvider defines email read operations.
type EmailProvider interface {
	GetRecentEmails(ctx context.Context, token *OAuthToken, maxResults int) ([]Email, error)
	GetUnreadCount(ctx context.Context, token *OAuthToken) (int, error)
}

// CalendarProvider defines calendar operations.
type CalendarProvider interface {
	GetUpcomingEvents(ctx context.Context, token *OAuthToken, duration time.Duration) ([]CalendarEvent, error)
	FindEvent(ctx context.Context, token *OAuthToken, query string) (*CalendarEvent, error)
	UpdateEvent(ctx context.Context, token *OAuthToken, event *CalendarEvent) error
	CreateEvent(ctx context.Context, token *OAuthToken, event *CalendarEvent) error
	DeleteEvent(ctx context.Context, token *OAuthToken, eventID string) error
}

// TokenRefresher refreshes expired OAuth tokens.
type TokenRefresher interface {
	RefreshToken(ctx context.Context, token *OAuthToken) (*OAuthToken, error)
}

// MessageSender sends messages via WhatsApp.
type MessageSender interface {
	SendMessage(ctx context.Context, to, message string) error
}

// IntentParser parses natural language messages into structured intents.
type IntentParser interface {
	ParseIntent(ctx context.Context, message string, history []ConversationMessage) (*ParsedIntent, error)
}
