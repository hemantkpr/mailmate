package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/oauth2"

	"github.com/hemantkpr/mailmate/internal/domain"
	"github.com/hemantkpr/mailmate/internal/provider"
)

// Auth manages OAuth flows and token lifecycle.
type Auth struct {
	google    *provider.Google
	microsoft *provider.Microsoft
	tokens    domain.TokenRepository
	users     domain.UserRepository
	redis     *redis.Client
	messenger domain.MessageSender
	logger    *zap.Logger
	baseURL   string
}

// NewAuth creates an Auth service.
func NewAuth(
	google *provider.Google,
	microsoft *provider.Microsoft,
	tokens domain.TokenRepository,
	users domain.UserRepository,
	rdb *redis.Client,
	messenger domain.MessageSender,
	logger *zap.Logger,
	baseURL string,
) *Auth {
	return &Auth{
		google:    google,
		microsoft: microsoft,
		tokens:    tokens,
		users:     users,
		redis:     rdb,
		messenger: messenger,
		logger:    logger,
		baseURL:   baseURL,
	}
}

// OAuthState stores the mapping between OAuth state and user phone number.
const oauthStatePrefix = "oauth_state:"
const oauthStateTTL = 10 * time.Minute

// GenerateConnectLinks sends OAuth connection links to the user.
func (a *Auth) GenerateConnectLinks(ctx context.Context, phone string) (string, error) {
	// Generate state tokens
	googleState, err := a.generateState(ctx, phone, "google")
	if err != nil {
		return "", fmt.Errorf("generate google state: %w", err)
	}
	msState, err := a.generateState(ctx, phone, "microsoft")
	if err != nil {
		return "", fmt.Errorf("generate microsoft state: %w", err)
	}

	googleURL := a.google.AuthURL(googleState)
	msURL := a.microsoft.AuthURL(msState)

	msg := fmt.Sprintf("🔗 *Connect your accounts:*\n\n"+
		"📧 *Gmail + Google Calendar:*\n%s\n\n"+
		"📧 *Outlook + Microsoft Calendar:*\n%s\n\n"+
		"_Links expire in 10 minutes._", googleURL, msURL)

	return msg, nil
}

// HandleGoogleCallback processes the Google OAuth callback.
func (a *Auth) HandleGoogleCallback(ctx context.Context, code, state string) error {
	phone, err := a.resolveState(ctx, state)
	if err != nil {
		return fmt.Errorf("invalid state: %w", err)
	}

	token, err := a.google.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}

	user, err := a.users.GetUserByPhone(ctx, phone)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found for phone: %s", phone)
	}

	oauthToken := &domain.OAuthToken{
		UserID:       user.ID,
		Provider:     domain.ProviderGoogle,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenExpiry:  token.Expiry,
		Scopes:       "gmail.readonly,calendar",
	}

	if err := a.tokens.SaveToken(ctx, oauthToken); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	if err := a.messenger.SendMessage(ctx, phone,
		"✅ *Google account connected!*\nI can now access your Gmail and Google Calendar.\n\nTry: \"What meetings do I have today?\""); err != nil {
		a.logger.Error("failed to send confirmation", zap.Error(err))
	}

	return nil
}

// HandleMicrosoftCallback processes the Microsoft OAuth callback.
func (a *Auth) HandleMicrosoftCallback(ctx context.Context, code, state string) error {
	phone, err := a.resolveState(ctx, state)
	if err != nil {
		return fmt.Errorf("invalid state: %w", err)
	}

	token, err := a.microsoft.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}

	user, err := a.users.GetUserByPhone(ctx, phone)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found for phone: %s", phone)
	}

	oauthToken := &domain.OAuthToken{
		UserID:       user.ID,
		Provider:     domain.ProviderMicrosoft,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenExpiry:  token.Expiry,
		Scopes:       "Mail.Read,Calendars.ReadWrite",
	}

	if err := a.tokens.SaveToken(ctx, oauthToken); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	if err := a.messenger.SendMessage(ctx, phone,
		"✅ *Microsoft account connected!*\nI can now access your Outlook and Microsoft Calendar.\n\nTry: \"Show my emails\""); err != nil {
		a.logger.Error("failed to send confirmation", zap.Error(err))
	}

	return nil
}

// GetValidToken retrieves a valid (non-expired) token, refreshing if needed.
func (a *Auth) GetValidToken(ctx context.Context, userID string, prov domain.Provider) (*domain.OAuthToken, error) {
	token, err := a.tokens.GetToken(ctx, userID, prov)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, nil
	}

	// Refresh if token expires within 5 minutes
	if time.Until(token.TokenExpiry) < 5*time.Minute {
		var refreshed *domain.OAuthToken
		switch prov {
		case domain.ProviderGoogle:
			refreshed, err = a.google.RefreshToken(ctx, token)
		case domain.ProviderMicrosoft:
			refreshed, err = a.microsoft.RefreshToken(ctx, token)
		}
		if err != nil {
			a.logger.Warn("failed to refresh token", zap.String("user_id", userID), zap.Error(err))
			return token, nil // Return existing token; may still work
		}
		if err := a.tokens.SaveToken(ctx, refreshed); err != nil {
			a.logger.Error("failed to save refreshed token", zap.Error(err))
		}
		return refreshed, nil
	}

	return token, nil
}

// DisconnectProvider removes OAuth tokens for a provider.
func (a *Auth) DisconnectProvider(ctx context.Context, userID string, prov domain.Provider) error {
	return a.tokens.DeleteToken(ctx, userID, prov)
}

// HasConnectedProviders checks if a user has any connected providers.
func (a *Auth) HasConnectedProviders(ctx context.Context, userID string) (bool, error) {
	googleToken, err := a.tokens.GetToken(ctx, userID, domain.ProviderGoogle)
	if err != nil {
		return false, err
	}
	if googleToken != nil {
		return true, nil
	}

	msToken, err := a.tokens.GetToken(ctx, userID, domain.ProviderMicrosoft)
	if err != nil {
		return false, err
	}
	return msToken != nil, nil
}

func (a *Auth) generateState(ctx context.Context, phone, provider string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	state := hex.EncodeToString(b)

	key := oauthStatePrefix + state
	value := phone + "|" + provider
	if err := a.redis.Set(ctx, key, value, oauthStateTTL).Err(); err != nil {
		return "", fmt.Errorf("store state: %w", err)
	}
	return state, nil
}

func (a *Auth) resolveState(ctx context.Context, state string) (string, error) {
	key := oauthStatePrefix + state
	val, err := a.redis.GetDel(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("state not found or expired: %w", err)
	}
	// val format: "phone|provider"
	parts := splitFirst(val, '|')
	return parts, nil
}

func splitFirst(s string, sep byte) string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i]
		}
	}
	return s
}

// ExchangeGoogleToken is a helper that wraps the OAuth exchange.
func (a *Auth) ExchangeGoogleToken(ctx context.Context, code string) (*oauth2.Token, error) {
	return a.google.Exchange(ctx, code)
}

// ExchangeMicrosoftToken is a helper that wraps the OAuth exchange.
func (a *Auth) ExchangeMicrosoftToken(ctx context.Context, code string) (*oauth2.Token, error) {
	return a.microsoft.Exchange(ctx, code)
}
