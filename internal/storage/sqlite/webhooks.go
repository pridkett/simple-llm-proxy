package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// ListWebhookSubscriptions returns all webhook subscriptions ordered by created_at DESC.
func (s *Storage) ListWebhookSubscriptions(ctx context.Context) ([]*storage.WebhookSubscription, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, url, events, secret, enabled, created_at
		 FROM webhook_subscriptions
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing webhook subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []*storage.WebhookSubscription
	for rows.Next() {
		sub := &storage.WebhookSubscription{}
		var eventsStr string
		var secret *string
		if err := rows.Scan(&sub.ID, &sub.URL, &eventsStr, &secret, &sub.Enabled, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning webhook subscription: %w", err)
		}
		if err := json.Unmarshal([]byte(eventsStr), &sub.Events); err != nil {
			return nil, fmt.Errorf("unmarshaling events for subscription %d: %w", sub.ID, err)
		}
		if secret != nil {
			sub.Secret = *secret
		}
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating webhook subscriptions: %w", err)
	}
	return subs, nil
}

// CreateWebhookSubscription inserts a new webhook subscription and returns it with assigned ID and created_at.
func (s *Storage) CreateWebhookSubscription(ctx context.Context, sub *storage.WebhookSubscription) (*storage.WebhookSubscription, error) {
	eventsJSON, err := json.Marshal(sub.Events)
	if err != nil {
		return nil, fmt.Errorf("marshaling events: %w", err)
	}

	var secretParam interface{}
	if sub.Secret != "" {
		secretParam = sub.Secret
	}

	var id int64
	var createdAt time.Time
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO webhook_subscriptions (url, events, secret, enabled)
		 VALUES (?, ?, ?, ?)
		 RETURNING id, created_at`,
		sub.URL, string(eventsJSON), secretParam, sub.Enabled,
	).Scan(&id, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("creating webhook subscription: %w", err)
	}

	result := *sub
	result.ID = id
	result.CreatedAt = createdAt
	return &result, nil
}

// UpdateWebhookSubscription modifies url, events, secret, and enabled fields of an existing subscription.
func (s *Storage) UpdateWebhookSubscription(ctx context.Context, sub *storage.WebhookSubscription) error {
	eventsJSON, err := json.Marshal(sub.Events)
	if err != nil {
		return fmt.Errorf("marshaling events: %w", err)
	}

	var secretParam interface{}
	if sub.Secret != "" {
		secretParam = sub.Secret
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE webhook_subscriptions SET url = ?, events = ?, secret = ?, enabled = ? WHERE id = ?`,
		sub.URL, string(eventsJSON), secretParam, sub.Enabled, sub.ID,
	)
	if err != nil {
		return fmt.Errorf("updating webhook subscription %d: %w", sub.ID, err)
	}
	return nil
}

// DeleteWebhookSubscription removes a webhook subscription by ID. No error for non-existent ID.
func (s *Storage) DeleteWebhookSubscription(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM webhook_subscriptions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting webhook subscription %d: %w", id, err)
	}
	return nil
}

// GetEnabledWebhooksByEvent returns only enabled subscriptions whose events JSON array contains the given event type.
// Filtering is done in Go because modernc.org/sqlite does not support JSON array containment functions.
func (s *Storage) GetEnabledWebhooksByEvent(ctx context.Context, eventType string) ([]*storage.WebhookSubscription, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, url, events, secret, enabled, created_at
		 FROM webhook_subscriptions
		 WHERE enabled = TRUE`)
	if err != nil {
		return nil, fmt.Errorf("querying enabled webhook subscriptions: %w", err)
	}
	defer rows.Close()

	var result []*storage.WebhookSubscription
	for rows.Next() {
		sub := &storage.WebhookSubscription{}
		var eventsStr string
		var secret *string
		if err := rows.Scan(&sub.ID, &sub.URL, &eventsStr, &secret, &sub.Enabled, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning webhook subscription: %w", err)
		}
		if err := json.Unmarshal([]byte(eventsStr), &sub.Events); err != nil {
			return nil, fmt.Errorf("unmarshaling events for subscription %d: %w", sub.ID, err)
		}
		if secret != nil {
			sub.Secret = *secret
		}

		// Filter in Go: check if events contains the queried event type
		for _, evt := range sub.Events {
			if evt == eventType {
				result = append(result, sub)
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating webhook subscriptions: %w", err)
	}
	return result, nil
}

// InsertNotificationEvent inserts a routing event and returns its new row ID.
func (s *Storage) InsertNotificationEvent(ctx context.Context, eventType string, payload string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO notification_events (event_type, payload) VALUES (?, ?) RETURNING id`,
		eventType, payload,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("inserting notification event: %w", err)
	}
	return id, nil
}

// ListNotificationEvents returns paginated notification events with total count.
// When eventType is non-empty, results are filtered to that event type.
func (s *Storage) ListNotificationEvents(ctx context.Context, limit, offset int, eventType string) ([]*storage.NotificationEvent, int, error) {
	var total int
	var err error

	if eventType != "" {
		err = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM notification_events WHERE event_type = ?`, eventType,
		).Scan(&total)
	} else {
		err = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM notification_events`,
		).Scan(&total)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("counting notification events: %w", err)
	}

	var rows interface {
		Next() bool
		Scan(dest ...interface{}) error
		Close() error
		Err() error
	}

	if eventType != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, event_type, payload, created_at
			 FROM notification_events
			 WHERE event_type = ?
			 ORDER BY created_at DESC
			 LIMIT ? OFFSET ?`,
			eventType, limit, offset,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, event_type, payload, created_at
			 FROM notification_events
			 ORDER BY created_at DESC
			 LIMIT ? OFFSET ?`,
			limit, offset,
		)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("querying notification events: %w", err)
	}
	defer rows.Close()

	var events []*storage.NotificationEvent
	for rows.Next() {
		evt := &storage.NotificationEvent{}
		if err := rows.Scan(&evt.ID, &evt.EventType, &evt.Payload, &evt.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scanning notification event: %w", err)
		}
		events = append(events, evt)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating notification events: %w", err)
	}
	return events, total, nil
}

// DeleteOldNotificationEvents removes events older than the given cutoff and returns the count deleted.
// ON DELETE CASCADE on webhook_deliveries.event_id will also delete associated delivery rows.
func (s *Storage) DeleteOldNotificationEvents(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM notification_events WHERE created_at < ?`,
		olderThan.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("deleting old notification events: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}
	return count, nil
}

// InsertWebhookDelivery creates a delivery record. subscriptionID is nil for YAML webhook deliveries.
func (s *Storage) InsertWebhookDelivery(ctx context.Context, subscriptionID *int64, eventID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO webhook_deliveries (subscription_id, event_id) VALUES (?, ?) RETURNING id`,
		subscriptionID, eventID,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("inserting webhook delivery: %w", err)
	}
	return id, nil
}

// UpdateWebhookDeliveryStatus updates status, response_code, attempt_count, and sets last_attempt_at to now.
func (s *Storage) UpdateWebhookDeliveryStatus(ctx context.Context, id int64, status string, responseCode int, attemptCount int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, response_code = ?, attempt_count = ?, last_attempt_at = datetime('now') WHERE id = ?`,
		status, responseCode, attemptCount, id,
	)
	if err != nil {
		return fmt.Errorf("updating webhook delivery status %d: %w", id, err)
	}
	return nil
}
