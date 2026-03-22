package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/hemantkpr/mailmate/internal/domain"
)

// Postgres implements all repository interfaces using PostgreSQL.
type Postgres struct {
	pool      *pgxpool.Pool
	encryptor *Encryptor
	logger    *zap.Logger
}

// NewPostgres creates a new PostgreSQL store.
func NewPostgres(pool *pgxpool.Pool, encryptor *Encryptor, logger *zap.Logger) *Postgres {
	return &Postgres{
		pool:      pool,
		encryptor: encryptor,
		logger:    logger,
	}
}

// --- UserRepository ---

func (p *Postgres) CreateUser(ctx context.Context, user *domain.User) error {
	query := `INSERT INTO users (phone_number, name, timezone)
		VALUES ($1, $2, $3) RETURNING id, created_at, updated_at`
	return p.pool.QueryRow(ctx, query, user.PhoneNumber, user.Name, user.Timezone).
		Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

func (p *Postgres) GetUserByPhone(ctx context.Context, phone string) (*domain.User, error) {
	user := &domain.User{}
	query := `SELECT id, phone_number, name, timezone, created_at, updated_at FROM users WHERE phone_number = $1`
	err := p.pool.QueryRow(ctx, query, phone).
		Scan(&user.ID, &user.PhoneNumber, &user.Name, &user.Timezone, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user by phone: %w", err)
	}
	return user, nil
}

func (p *Postgres) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	user := &domain.User{}
	query := `SELECT id, phone_number, name, timezone, created_at, updated_at FROM users WHERE id = $1`
	err := p.pool.QueryRow(ctx, query, id).
		Scan(&user.ID, &user.PhoneNumber, &user.Name, &user.Timezone, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return user, nil
}

func (p *Postgres) UpdateUser(ctx context.Context, user *domain.User) error {
	query := `UPDATE users SET name = $2, timezone = $3 WHERE id = $1`
	_, err := p.pool.Exec(ctx, query, user.ID, user.Name, user.Timezone)
	return err
}

// --- TokenRepository ---

func (p *Postgres) SaveToken(ctx context.Context, token *domain.OAuthToken) error {
	encAccessToken, err := p.encryptor.Encrypt(token.AccessToken)
	if err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}
	encRefreshToken, err := p.encryptor.Encrypt(token.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypt refresh token: %w", err)
	}

	query := `INSERT INTO oauth_tokens (user_id, provider, access_token, refresh_token, token_expiry, scopes)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, provider)
		DO UPDATE SET access_token = $3, refresh_token = $4, token_expiry = $5, scopes = $6
		RETURNING id, created_at, updated_at`
	return p.pool.QueryRow(ctx, query,
		token.UserID, token.Provider, encAccessToken, encRefreshToken, token.TokenExpiry, token.Scopes,
	).Scan(&token.ID, &token.CreatedAt, &token.UpdatedAt)
}

func (p *Postgres) GetToken(ctx context.Context, userID string, provider domain.Provider) (*domain.OAuthToken, error) {
	token := &domain.OAuthToken{}
	query := `SELECT id, user_id, provider, access_token, refresh_token, token_expiry, scopes, created_at, updated_at
		FROM oauth_tokens WHERE user_id = $1 AND provider = $2`
	err := p.pool.QueryRow(ctx, query, userID, provider).
		Scan(&token.ID, &token.UserID, &token.Provider,
			&token.AccessToken, &token.RefreshToken, &token.TokenExpiry,
			&token.Scopes, &token.CreatedAt, &token.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	token.AccessToken, err = p.encryptor.Decrypt(token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("decrypt access token: %w", err)
	}
	token.RefreshToken, err = p.encryptor.Decrypt(token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("decrypt refresh token: %w", err)
	}

	return token, nil
}

func (p *Postgres) DeleteToken(ctx context.Context, userID string, provider domain.Provider) error {
	query := `DELETE FROM oauth_tokens WHERE user_id = $1 AND provider = $2`
	_, err := p.pool.Exec(ctx, query, userID, provider)
	return err
}

func (p *Postgres) GetTokensByProvider(ctx context.Context, provider domain.Provider) ([]domain.OAuthToken, error) {
	query := `SELECT id, user_id, provider, access_token, refresh_token, token_expiry, scopes, created_at, updated_at
		FROM oauth_tokens WHERE provider = $1`
	rows, err := p.pool.Query(ctx, query, provider)
	if err != nil {
		return nil, fmt.Errorf("query tokens by provider: %w", err)
	}
	defer rows.Close()

	var tokens []domain.OAuthToken
	for rows.Next() {
		var t domain.OAuthToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Provider,
			&t.AccessToken, &t.RefreshToken, &t.TokenExpiry,
			&t.Scopes, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		t.AccessToken, err = p.encryptor.Decrypt(t.AccessToken)
		if err != nil {
			p.logger.Warn("failed to decrypt access token", zap.String("token_id", t.ID), zap.Error(err))
			continue
		}
		t.RefreshToken, err = p.encryptor.Decrypt(t.RefreshToken)
		if err != nil {
			p.logger.Warn("failed to decrypt refresh token", zap.String("token_id", t.ID), zap.Error(err))
			continue
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// --- TrackerRepository ---

func (p *Postgres) CreateTrackedItem(ctx context.Context, item *domain.TrackedItem) error {
	query := `INSERT INTO tracked_items (user_id, title, description, start_date, end_date)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, active, created_at`
	return p.pool.QueryRow(ctx, query,
		item.UserID, item.Title, item.Description, item.StartDate, item.EndDate,
	).Scan(&item.ID, &item.Active, &item.CreatedAt)
}

func (p *Postgres) GetActiveTrackedItems(ctx context.Context, userID string) ([]domain.TrackedItem, error) {
	query := `SELECT id, user_id, title, description, start_date, end_date, active, created_at
		FROM tracked_items WHERE user_id = $1 AND active = TRUE ORDER BY created_at DESC`
	rows, err := p.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("query tracked items: %w", err)
	}
	defer rows.Close()

	var items []domain.TrackedItem
	for rows.Next() {
		var item domain.TrackedItem
		if err := rows.Scan(&item.ID, &item.UserID, &item.Title, &item.Description,
			&item.StartDate, &item.EndDate, &item.Active, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tracked item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) GetTrackedItemByID(ctx context.Context, id string) (*domain.TrackedItem, error) {
	item := &domain.TrackedItem{}
	query := `SELECT id, user_id, title, description, start_date, end_date, active, created_at
		FROM tracked_items WHERE id = $1`
	err := p.pool.QueryRow(ctx, query, id).
		Scan(&item.ID, &item.UserID, &item.Title, &item.Description,
			&item.StartDate, &item.EndDate, &item.Active, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tracked item: %w", err)
	}
	return item, nil
}

func (p *Postgres) DeactivateTrackedItem(ctx context.Context, id string) error {
	query := `UPDATE tracked_items SET active = FALSE WHERE id = $1`
	_, err := p.pool.Exec(ctx, query, id)
	return err
}

func (p *Postgres) CreateEntry(ctx context.Context, entry *domain.TrackedEntry) error {
	query := `INSERT INTO tracked_entries (tracked_item_id, entry_date, notes, completed)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tracked_item_id, entry_date)
		DO UPDATE SET notes = $3, completed = $4
		RETURNING id, created_at`
	return p.pool.QueryRow(ctx, query,
		entry.TrackedItemID, entry.EntryDate, entry.Notes, entry.Completed,
	).Scan(&entry.ID, &entry.CreatedAt)
}

func (p *Postgres) GetEntries(ctx context.Context, itemID string, start, end time.Time) ([]domain.TrackedEntry, error) {
	query := `SELECT id, tracked_item_id, entry_date, notes, completed, created_at
		FROM tracked_entries WHERE tracked_item_id = $1 AND entry_date >= $2 AND entry_date <= $3
		ORDER BY entry_date ASC`
	rows, err := p.pool.Query(ctx, query, itemID, start, end)
	if err != nil {
		return nil, fmt.Errorf("query entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.TrackedEntry
	for rows.Next() {
		var e domain.TrackedEntry
		if err := rows.Scan(&e.ID, &e.TrackedItemID, &e.EntryDate, &e.Notes, &e.Completed, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (p *Postgres) GetTodayEntry(ctx context.Context, itemID string) (*domain.TrackedEntry, error) {
	entry := &domain.TrackedEntry{}
	query := `SELECT id, tracked_item_id, entry_date, notes, completed, created_at
		FROM tracked_entries WHERE tracked_item_id = $1 AND entry_date = CURRENT_DATE`
	err := p.pool.QueryRow(ctx, query, itemID).
		Scan(&entry.ID, &entry.TrackedItemID, &entry.EntryDate, &entry.Notes, &entry.Completed, &entry.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get today entry: %w", err)
	}
	return entry, nil
}

// --- PreferenceRepository ---

func (p *Postgres) GetPreferences(ctx context.Context, userID string) (*domain.NotificationPreference, error) {
	pref := &domain.NotificationPreference{}
	query := `SELECT id, user_id, meeting_reminder_minutes, daily_summary_enabled,
		daily_summary_time, email_notifications, created_at, updated_at
		FROM notification_preferences WHERE user_id = $1`
	err := p.pool.QueryRow(ctx, query, userID).
		Scan(&pref.ID, &pref.UserID, &pref.MeetingReminderMinutes,
			&pref.DailySummaryEnabled, &pref.DailySummaryTime,
			&pref.EmailNotifications, &pref.CreatedAt, &pref.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get preferences: %w", err)
	}
	return pref, nil
}

func (p *Postgres) SavePreferences(ctx context.Context, pref *domain.NotificationPreference) error {
	query := `INSERT INTO notification_preferences
		(user_id, meeting_reminder_minutes, daily_summary_enabled, daily_summary_time, email_notifications)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			meeting_reminder_minutes = $2,
			daily_summary_enabled = $3,
			daily_summary_time = $4,
			email_notifications = $5
		RETURNING id, created_at, updated_at`
	return p.pool.QueryRow(ctx, query,
		pref.UserID, pref.MeetingReminderMinutes,
		pref.DailySummaryEnabled, pref.DailySummaryTime,
		pref.EmailNotifications,
	).Scan(&pref.ID, &pref.CreatedAt, &pref.UpdatedAt)
}

// --- ConversationRepository ---

func (p *Postgres) SaveMessage(ctx context.Context, msg *domain.ConversationMessage) error {
	query := `INSERT INTO conversation_history (user_id, role, message) VALUES ($1, $2, $3) RETURNING id, created_at`
	return p.pool.QueryRow(ctx, query, msg.UserID, msg.Role, msg.Message).
		Scan(&msg.ID, &msg.CreatedAt)
}

func (p *Postgres) GetRecentMessages(ctx context.Context, userID string, limit int) ([]domain.ConversationMessage, error) {
	query := `SELECT id, user_id, role, message, created_at
		FROM conversation_history WHERE user_id = $1
		ORDER BY created_at DESC LIMIT $2`
	rows, err := p.pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var msgs []domain.ConversationMessage
	for rows.Next() {
		var m domain.ConversationMessage
		if err := rows.Scan(&m.ID, &m.UserID, &m.Role, &m.Message, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to get chronological order
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

func (p *Postgres) CleanOldMessages(ctx context.Context, before time.Time) error {
	query := `DELETE FROM conversation_history WHERE created_at < $1`
	_, err := p.pool.Exec(ctx, query, before)
	return err
}

// Ping checks database connectivity.
func (p *Postgres) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}
