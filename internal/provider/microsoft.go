package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/hemantkpr/mailmate/internal/config"
	"github.com/hemantkpr/mailmate/internal/domain"
)

// Microsoft implements EmailProvider, CalendarProvider, and TokenRefresher for Microsoft Graph API.
type Microsoft struct {
	oauthConfig *oauth2.Config
	tenantID    string
}

// NewMicrosoft creates a Microsoft provider.
func NewMicrosoft(cfg config.MicrosoftConfig) *Microsoft {
	return &Microsoft{
		tenantID: cfg.TenantID,
		oauthConfig: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes: []string{
				"openid",
				"profile",
				"email",
				"offline_access",
				"Mail.Read",
				"Calendars.ReadWrite",
			},
			Endpoint: oauth2.Endpoint{
				AuthURL:  fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize", cfg.TenantID),
				TokenURL: fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", cfg.TenantID),
			},
		},
	}
}

// AuthURL returns the Microsoft OAuth consent URL.
func (m *Microsoft) AuthURL(state string) string {
	return m.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// Exchange exchanges an authorization code for tokens.
func (m *Microsoft) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return m.oauthConfig.Exchange(ctx, code)
}

func (m *Microsoft) httpClient(token *domain.OAuthToken) *http.Client {
	t := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.TokenExpiry,
		TokenType:    "Bearer",
	}
	return oauth2.NewClient(context.Background(), m.oauthConfig.TokenSource(context.Background(), t))
}

const graphBaseURL = "https://graph.microsoft.com/v1.0"

// --- EmailProvider ---

func (m *Microsoft) GetRecentEmails(ctx context.Context, token *domain.OAuthToken, maxResults int) ([]domain.Email, error) {
	endpoint := fmt.Sprintf("%s/me/mailfolders/inbox/messages?$top=%d&$orderby=receivedDateTime desc&$select=id,from,toRecipients,subject,bodyPreview,receivedDateTime,isRead",
		graphBaseURL, maxResults)

	body, err := m.graphGet(ctx, token, endpoint)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	var result struct {
		Value []struct {
			ID           string `json:"id"`
			Subject      string `json:"subject"`
			BodyPreview  string `json:"bodyPreview"`
			IsRead       bool   `json:"isRead"`
			ReceivedDate string `json:"receivedDateTime"`
			From         struct {
				EmailAddress struct {
					Name    string `json:"name"`
					Address string `json:"address"`
				} `json:"emailAddress"`
			} `json:"from"`
			ToRecipients []struct {
				EmailAddress struct {
					Address string `json:"address"`
				} `json:"emailAddress"`
			} `json:"toRecipients"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse messages: %w", err)
	}

	var emails []domain.Email
	for _, msg := range result.Value {
		email := domain.Email{
			ID:      msg.ID,
			From:    fmt.Sprintf("%s <%s>", msg.From.EmailAddress.Name, msg.From.EmailAddress.Address),
			Subject: msg.Subject,
			Snippet: msg.BodyPreview,
			IsRead:  msg.IsRead,
		}
		if t, err := time.Parse(time.RFC3339, msg.ReceivedDate); err == nil {
			email.Date = t
		}
		for _, to := range msg.ToRecipients {
			email.To = append(email.To, to.EmailAddress.Address)
		}
		emails = append(emails, email)
	}
	return emails, nil
}

func (m *Microsoft) GetUnreadCount(ctx context.Context, token *domain.OAuthToken) (int, error) {
	endpoint := fmt.Sprintf("%s/me/mailfolders/inbox?$select=unreadItemCount", graphBaseURL)
	body, err := m.graphGet(ctx, token, endpoint)
	if err != nil {
		return 0, fmt.Errorf("get unread count: %w", err)
	}

	var result struct {
		UnreadItemCount int `json:"unreadItemCount"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("parse unread count: %w", err)
	}
	return result.UnreadItemCount, nil
}

// --- CalendarProvider ---

func (m *Microsoft) GetUpcomingEvents(ctx context.Context, token *domain.OAuthToken, duration time.Duration) ([]domain.CalendarEvent, error) {
	now := time.Now().UTC()
	end := now.Add(duration)
	endpoint := fmt.Sprintf(
		"%s/me/calendarview?startDateTime=%s&endDateTime=%s&$orderby=start/dateTime&$top=50&$select=id,subject,bodyPreview,location,start,end,attendees",
		graphBaseURL,
		url.QueryEscape(now.Format(time.RFC3339)),
		url.QueryEscape(end.Format(time.RFC3339)),
	)

	body, err := m.graphGet(ctx, token, endpoint)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}

	var result struct {
		Value []msGraphEvent `json:"value"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse events: %w", err)
	}

	var events []domain.CalendarEvent
	for _, ev := range result.Value {
		events = append(events, msEventToDomain(ev))
	}
	return events, nil
}

func (m *Microsoft) FindEvent(ctx context.Context, token *domain.OAuthToken, query string) (*domain.CalendarEvent, error) {
	now := time.Now().UTC()
	end := now.Add(30 * 24 * time.Hour)
	endpoint := fmt.Sprintf(
		"%s/me/calendarview?startDateTime=%s&endDateTime=%s&$filter=contains(subject,'%s')&$top=5&$select=id,subject,bodyPreview,location,start,end,attendees",
		graphBaseURL,
		url.QueryEscape(now.Add(-24*time.Hour).Format(time.RFC3339)),
		url.QueryEscape(end.Format(time.RFC3339)),
		url.QueryEscape(query),
	)

	body, err := m.graphGet(ctx, token, endpoint)
	if err != nil {
		return nil, fmt.Errorf("find event: %w", err)
	}

	var result struct {
		Value []msGraphEvent `json:"value"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse events: %w", err)
	}

	if len(result.Value) == 0 {
		return nil, nil
	}

	event := msEventToDomain(result.Value[0])
	return &event, nil
}

func (m *Microsoft) UpdateEvent(ctx context.Context, token *domain.OAuthToken, event *domain.CalendarEvent) error {
	endpoint := fmt.Sprintf("%s/me/events/%s", graphBaseURL, event.ID)
	payload := msGraphEventPayload{
		Subject: event.Title,
		Body: &msGraphBody{
			ContentType: "text",
			Content:     event.Description,
		},
		Start: &msGraphDateTime{
			DateTime: event.StartTime.Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
		End: &msGraphDateTime{
			DateTime: event.EndTime.Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
	}

	if event.Location != "" {
		payload.Location = &msGraphLocation{DisplayName: event.Location}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	return m.graphPatch(ctx, token, endpoint, data)
}

func (m *Microsoft) CreateEvent(ctx context.Context, token *domain.OAuthToken, event *domain.CalendarEvent) error {
	endpoint := fmt.Sprintf("%s/me/events", graphBaseURL)
	payload := msGraphEventPayload{
		Subject: event.Title,
		Body: &msGraphBody{
			ContentType: "text",
			Content:     event.Description,
		},
		Start: &msGraphDateTime{
			DateTime: event.StartTime.Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
		End: &msGraphDateTime{
			DateTime: event.EndTime.Format("2006-01-02T15:04:05"),
			TimeZone: "UTC",
		},
	}

	if event.Location != "" {
		payload.Location = &msGraphLocation{DisplayName: event.Location}
	}

	var attendees []msGraphAttendee
	for _, a := range event.Attendees {
		attendees = append(attendees, msGraphAttendee{
			EmailAddress: msGraphEmailAddress{Address: a},
			Type:         "required",
		})
	}
	payload.Attendees = attendees

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	body, err := m.graphPost(ctx, token, endpoint, data)
	if err != nil {
		return fmt.Errorf("create event: %w", err)
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err == nil {
		event.ID = created.ID
	}
	return nil
}

func (m *Microsoft) DeleteEvent(ctx context.Context, token *domain.OAuthToken, eventID string) error {
	endpoint := fmt.Sprintf("%s/me/events/%s", graphBaseURL, eventID)
	return m.graphDelete(ctx, token, endpoint)
}

// --- TokenRefresher ---

func (m *Microsoft) RefreshToken(ctx context.Context, token *domain.OAuthToken) (*domain.OAuthToken, error) {
	t := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.TokenExpiry,
		TokenType:    "Bearer",
	}
	ts := m.oauthConfig.TokenSource(ctx, t)
	newToken, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh microsoft token: %w", err)
	}

	refreshed := *token
	refreshed.AccessToken = newToken.AccessToken
	refreshed.TokenExpiry = newToken.Expiry
	if newToken.RefreshToken != "" {
		refreshed.RefreshToken = newToken.RefreshToken
	}
	return &refreshed, nil
}

// --- Graph API Helpers ---

func (m *Microsoft) graphGet(ctx context.Context, token *domain.OAuthToken, endpoint string) ([]byte, error) {
	client := m.httpClient(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return m.doRequest(client, req)
}

func (m *Microsoft) graphPost(ctx context.Context, token *domain.OAuthToken, endpoint string, data []byte) ([]byte, error) {
	client := m.httpClient(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return m.doRequest(client, req)
}

func (m *Microsoft) graphPatch(ctx context.Context, token *domain.OAuthToken, endpoint string, data []byte) error {
	client := m.httpClient(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	_, err = m.doRequest(client, req)
	return err
}

func (m *Microsoft) graphDelete(ctx context.Context, token *domain.OAuthToken, endpoint string) error {
	client := m.httpClient(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	_, err = m.doRequest(client, req)
	return err
}

func (m *Microsoft) doRequest(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("graph API error (status %d): %s", resp.StatusCode, truncate(string(body), 500))
	}
	return body, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// --- Microsoft Graph Types ---

type msGraphEvent struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Body    struct {
		Content string `json:"content"`
	} `json:"body"`
	Location struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	Start struct {
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone"`
	} `json:"end"`
	Attendees []struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"attendees"`
}

type msGraphEventPayload struct {
	Subject   string            `json:"subject"`
	Body      *msGraphBody      `json:"body,omitempty"`
	Start     *msGraphDateTime  `json:"start,omitempty"`
	End       *msGraphDateTime  `json:"end,omitempty"`
	Location  *msGraphLocation  `json:"location,omitempty"`
	Attendees []msGraphAttendee `json:"attendees,omitempty"`
}

type msGraphBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type msGraphDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type msGraphLocation struct {
	DisplayName string `json:"displayName"`
}

type msGraphAttendee struct {
	EmailAddress msGraphEmailAddress `json:"emailAddress"`
	Type         string              `json:"type"`
}

type msGraphEmailAddress struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

func msEventToDomain(ev msGraphEvent) domain.CalendarEvent {
	event := domain.CalendarEvent{
		ID:          ev.ID,
		Title:       ev.Subject,
		Description: ev.Body.Content,
		Location:    ev.Location.DisplayName,
		Provider:    domain.ProviderMicrosoft,
	}

	parseMS := func(dt string) time.Time {
		// Microsoft returns datetime without timezone offset when timeZone is specified
		for _, layout := range []string{
			"2006-01-02T15:04:05.0000000",
			"2006-01-02T15:04:05",
			time.RFC3339,
		} {
			if t, err := time.Parse(layout, strings.TrimSpace(dt)); err == nil {
				return t
			}
		}
		return time.Time{}
	}

	event.StartTime = parseMS(ev.Start.DateTime)
	event.EndTime = parseMS(ev.End.DateTime)

	for _, a := range ev.Attendees {
		event.Attendees = append(event.Attendees, a.EmailAddress.Address)
	}
	return event
}
