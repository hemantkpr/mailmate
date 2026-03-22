package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"

	"github.com/hemantkpr/mailmate/internal/config"
	"github.com/hemantkpr/mailmate/internal/domain"
)

// Google implements EmailProvider, CalendarProvider, and TokenRefresher for Google APIs.
type Google struct {
	oauthConfig *oauth2.Config
}

// NewGoogle creates a Google provider.
func NewGoogle(cfg config.GoogleConfig) *Google {
	return &Google{
		oauthConfig: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes: []string{
				gmail.GmailReadonlyScope,
				calendar.CalendarScope,
				"https://www.googleapis.com/auth/userinfo.email",
			},
			Endpoint: google.Endpoint,
		},
	}
}

// AuthURL returns the Google OAuth consent URL.
func (g *Google) AuthURL(state string) string {
	return g.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

// Exchange exchanges an authorization code for tokens.
func (g *Google) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return g.oauthConfig.Exchange(ctx, code)
}

func (g *Google) tokenSource(token *domain.OAuthToken) oauth2.TokenSource {
	t := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.TokenExpiry,
		TokenType:    "Bearer",
	}
	return g.oauthConfig.TokenSource(context.Background(), t)
}

func (g *Google) httpClient(token *domain.OAuthToken) *http.Client {
	return oauth2.NewClient(context.Background(), g.tokenSource(token))
}

// --- EmailProvider ---

func (g *Google) GetRecentEmails(ctx context.Context, token *domain.OAuthToken, maxResults int) ([]domain.Email, error) {
	srv, err := gmail.NewService(ctx, option.WithHTTPClient(g.httpClient(token)))
	if err != nil {
		return nil, fmt.Errorf("create gmail service: %w", err)
	}

	list, err := srv.Users.Messages.List("me").
		MaxResults(int64(maxResults)).
		Q("in:inbox").
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	var emails []domain.Email
	for _, msg := range list.Messages {
		full, err := srv.Users.Messages.Get("me", msg.Id).
			Format("metadata").
			MetadataHeaders("From", "To", "Subject", "Date").
			Context(ctx).
			Do()
		if err != nil {
			continue
		}

		email := domain.Email{
			ID:      full.Id,
			Snippet: full.Snippet,
			IsRead:  !contains(full.LabelIds, "UNREAD"),
		}

		for _, h := range full.Payload.Headers {
			switch h.Name {
			case "From":
				email.From = h.Value
			case "To":
				email.To = strings.Split(h.Value, ",")
			case "Subject":
				email.Subject = h.Value
			case "Date":
				if t, err := parseEmailDate(h.Value); err == nil {
					email.Date = t
				}
			}
		}
		emails = append(emails, email)
	}
	return emails, nil
}

func (g *Google) GetUnreadCount(ctx context.Context, token *domain.OAuthToken) (int, error) {
	srv, err := gmail.NewService(ctx, option.WithHTTPClient(g.httpClient(token)))
	if err != nil {
		return 0, fmt.Errorf("create gmail service: %w", err)
	}

	list, err := srv.Users.Messages.List("me").
		Q("in:inbox is:unread").
		MaxResults(1).
		Context(ctx).
		Do()
	if err != nil {
		return 0, fmt.Errorf("list unread: %w", err)
	}

	return int(list.ResultSizeEstimate), nil
}

// --- CalendarProvider ---

func (g *Google) GetUpcomingEvents(ctx context.Context, token *domain.OAuthToken, duration time.Duration) ([]domain.CalendarEvent, error) {
	srv, err := calendar.NewService(ctx, option.WithHTTPClient(g.httpClient(token)))
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}

	now := time.Now()
	events, err := srv.Events.List("primary").
		TimeMin(now.Format(time.RFC3339)).
		TimeMax(now.Add(duration).Format(time.RFC3339)).
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(50).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	var result []domain.CalendarEvent
	for _, item := range events.Items {
		event := googleEventToDomain(item)
		result = append(result, event)
	}
	return result, nil
}

func (g *Google) FindEvent(ctx context.Context, token *domain.OAuthToken, query string) (*domain.CalendarEvent, error) {
	srv, err := calendar.NewService(ctx, option.WithHTTPClient(g.httpClient(token)))
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}

	now := time.Now()
	events, err := srv.Events.List("primary").
		Q(query).
		TimeMin(now.Add(-24 * time.Hour).Format(time.RFC3339)).
		TimeMax(now.Add(30 * 24 * time.Hour).Format(time.RFC3339)).
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(5).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("search events: %w", err)
	}

	if len(events.Items) == 0 {
		return nil, nil
	}

	event := googleEventToDomain(events.Items[0])
	return &event, nil
}

func (g *Google) UpdateEvent(ctx context.Context, token *domain.OAuthToken, event *domain.CalendarEvent) error {
	srv, err := calendar.NewService(ctx, option.WithHTTPClient(g.httpClient(token)))
	if err != nil {
		return fmt.Errorf("create calendar service: %w", err)
	}

	calEvent := &calendar.Event{
		Summary:     event.Title,
		Description: event.Description,
		Location:    event.Location,
		Start: &calendar.EventDateTime{
			DateTime: event.StartTime.Format(time.RFC3339),
		},
		End: &calendar.EventDateTime{
			DateTime: event.EndTime.Format(time.RFC3339),
		},
	}

	if len(event.Attendees) > 0 {
		for _, a := range event.Attendees {
			calEvent.Attendees = append(calEvent.Attendees, &calendar.EventAttendee{Email: a})
		}
	}

	_, err = srv.Events.Update("primary", event.ID, calEvent).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update event: %w", err)
	}
	return nil
}

func (g *Google) CreateEvent(ctx context.Context, token *domain.OAuthToken, event *domain.CalendarEvent) error {
	srv, err := calendar.NewService(ctx, option.WithHTTPClient(g.httpClient(token)))
	if err != nil {
		return fmt.Errorf("create calendar service: %w", err)
	}

	calEvent := &calendar.Event{
		Summary:     event.Title,
		Description: event.Description,
		Location:    event.Location,
		Start: &calendar.EventDateTime{
			DateTime: event.StartTime.Format(time.RFC3339),
		},
		End: &calendar.EventDateTime{
			DateTime: event.EndTime.Format(time.RFC3339),
		},
	}

	for _, a := range event.Attendees {
		calEvent.Attendees = append(calEvent.Attendees, &calendar.EventAttendee{Email: a})
	}

	created, err := srv.Events.Insert("primary", calEvent).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("create event: %w", err)
	}
	event.ID = created.Id
	return nil
}

func (g *Google) DeleteEvent(ctx context.Context, token *domain.OAuthToken, eventID string) error {
	srv, err := calendar.NewService(ctx, option.WithHTTPClient(g.httpClient(token)))
	if err != nil {
		return fmt.Errorf("create calendar service: %w", err)
	}
	return srv.Events.Delete("primary", eventID).Context(ctx).Do()
}

// --- TokenRefresher ---

func (g *Google) RefreshToken(ctx context.Context, token *domain.OAuthToken) (*domain.OAuthToken, error) {
	ts := g.tokenSource(token)
	newToken, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh google token: %w", err)
	}

	refreshed := *token
	refreshed.AccessToken = newToken.AccessToken
	refreshed.TokenExpiry = newToken.Expiry
	if newToken.RefreshToken != "" {
		refreshed.RefreshToken = newToken.RefreshToken
	}
	return &refreshed, nil
}

// GetUserEmail retrieves the authenticated user's email address.
func (g *Google) GetUserEmail(ctx context.Context, token *domain.OAuthToken) (string, error) {
	client := g.httpClient(token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", fmt.Errorf("get userinfo: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read userinfo response: %w", err)
	}

	var info struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("parse userinfo: %w", err)
	}
	return info.Email, nil
}

// --- Helpers ---

func googleEventToDomain(item *calendar.Event) domain.CalendarEvent {
	event := domain.CalendarEvent{
		ID:          item.Id,
		Title:       item.Summary,
		Description: item.Description,
		Location:    item.Location,
		Provider:    domain.ProviderGoogle,
	}

	if item.Start != nil {
		if item.Start.DateTime != "" {
			event.StartTime, _ = time.Parse(time.RFC3339, item.Start.DateTime)
		} else if item.Start.Date != "" {
			event.StartTime, _ = time.Parse("2006-01-02", item.Start.Date)
		}
	}
	if item.End != nil {
		if item.End.DateTime != "" {
			event.EndTime, _ = time.Parse(time.RFC3339, item.End.DateTime)
		} else if item.End.Date != "" {
			event.EndTime, _ = time.Parse("2006-01-02", item.End.Date)
		}
	}

	for _, a := range item.Attendees {
		event.Attendees = append(event.Attendees, a.Email)
	}

	return event
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func parseEmailDate(dateStr string) (time.Time, error) {
	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05 -0700",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, dateStr); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}
