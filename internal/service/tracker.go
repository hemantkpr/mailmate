package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/hemantkpr/mailmate/internal/domain"
)

// Tracker manages goal/habit tracking.
type Tracker struct {
	repo   domain.TrackerRepository
	logger *zap.Logger
}

// NewTracker creates a Tracker service.
func NewTracker(repo domain.TrackerRepository, logger *zap.Logger) *Tracker {
	return &Tracker{repo: repo, logger: logger}
}

// StartTracking creates a new tracked item.
func (t *Tracker) StartTracking(ctx context.Context, userID, title, description string, days int) (*domain.TrackedItem, error) {
	now := time.Now()
	item := &domain.TrackedItem{
		UserID:      userID,
		Title:       title,
		Description: description,
		StartDate:   now,
		EndDate:     now.AddDate(0, 0, days),
	}
	if err := t.repo.CreateTrackedItem(ctx, item); err != nil {
		return nil, fmt.Errorf("create tracked item: %w", err)
	}
	return item, nil
}

// LogEntry logs an entry for the user's most recent active tracked item.
func (t *Tracker) LogEntry(ctx context.Context, userID, notes string, completed bool) error {
	items, err := t.repo.GetActiveTrackedItems(ctx, userID)
	if err != nil {
		return fmt.Errorf("get active items: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("no active tracking goals found")
	}

	// Log to the most recently created item
	entry := &domain.TrackedEntry{
		TrackedItemID: items[0].ID,
		EntryDate:     time.Now(),
		Notes:         notes,
		Completed:     completed,
	}
	return t.repo.CreateEntry(ctx, entry)
}

// GetProgress returns a formatted progress summary for all active tracking items.
func (t *Tracker) GetProgress(ctx context.Context, userID string) (string, error) {
	items, err := t.repo.GetActiveTrackedItems(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get active items: %w", err)
	}
	if len(items) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("📊 *Your Tracking Progress:*\n\n")

	for _, item := range items {
		entries, err := t.repo.GetEntries(ctx, item.ID, item.StartDate, time.Now())
		if err != nil {
			t.logger.Warn("get entries failed", zap.String("item_id", item.ID), zap.Error(err))
			continue
		}

		totalDays := int(item.EndDate.Sub(item.StartDate).Hours() / 24)
		elapsedDays := int(time.Since(item.StartDate).Hours() / 24)
		if elapsedDays < 1 {
			elapsedDays = 1
		}
		remainingDays := totalDays - elapsedDays
		if remainingDays < 0 {
			remainingDays = 0
		}

		completedCount := 0
		for _, e := range entries {
			if e.Completed {
				completedCount++
			}
		}

		percentage := 0
		if elapsedDays > 0 {
			percentage = (completedCount * 100) / elapsedDays
		}

		// Progress bar
		bar := progressBar(percentage)

		sb.WriteString(fmt.Sprintf("*%s*\n", item.Title))
		sb.WriteString(fmt.Sprintf("%s %d%%\n", bar, percentage))
		sb.WriteString(fmt.Sprintf("✅ %d/%d days completed\n", completedCount, elapsedDays))
		sb.WriteString(fmt.Sprintf("📅 %d days remaining\n", remainingDays))

		// Show recent entry
		if len(entries) > 0 {
			last := entries[len(entries)-1]
			if last.Notes != "" {
				sb.WriteString(fmt.Sprintf("📝 Latest: _%s_\n", last.Notes))
			}
		}

		// Check today
		todayEntry, _ := t.repo.GetTodayEntry(ctx, item.ID)
		if todayEntry == nil {
			sb.WriteString("⚠️ _No entry for today yet!_\n")
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// StopTracking deactivates a tracked item matching the given subject.
func (t *Tracker) StopTracking(ctx context.Context, userID, subject string) error {
	items, err := t.repo.GetActiveTrackedItems(ctx, userID)
	if err != nil {
		return fmt.Errorf("get active items: %w", err)
	}

	subject = strings.ToLower(subject)
	for _, item := range items {
		if subject == "" || strings.Contains(strings.ToLower(item.Title), subject) {
			return t.repo.DeactivateTrackedItem(ctx, item.ID)
		}
	}
	return fmt.Errorf("no matching tracking goal found")
}

// GetDueReminders returns users who have active tracking items without today's entry.
func (t *Tracker) GetDueReminders(ctx context.Context, userID string) ([]domain.TrackedItem, error) {
	items, err := t.repo.GetActiveTrackedItems(ctx, userID)
	if err != nil {
		return nil, err
	}

	var due []domain.TrackedItem
	for _, item := range items {
		entry, _ := t.repo.GetTodayEntry(ctx, item.ID)
		if entry == nil {
			due = append(due, item)
		}
	}
	return due, nil
}

func progressBar(percentage int) string {
	filled := percentage / 10
	if filled > 10 {
		filled = 10
	}
	empty := 10 - filled
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
}
