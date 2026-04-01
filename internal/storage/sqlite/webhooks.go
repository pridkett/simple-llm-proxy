package sqlite

import (
	"context"
	"fmt"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// ListWebhookSubscriptions returns all webhook subscriptions ordered by created_at DESC.
func (s *Storage) ListWebhookSubscriptions(ctx context.Context) ([]*storage.WebhookSubscription, error) {
	return nil, fmt.Errorf("not implemented: ListWebhookSubscriptions")
}

// CreateWebhookSubscription inserts a new webhook subscription and returns it with assigned ID.
func (s *Storage) CreateWebhookSubscription(ctx context.Context, sub *storage.WebhookSubscription) (*storage.WebhookSubscription, error) {
	return nil, fmt.Errorf("not implemented: CreateWebhookSubscription")
}

// UpdateWebhookSubscription modifies url, events, secret, and enabled fields of an existing subscription.
func (s *Storage) UpdateWebhookSubscription(ctx context.Context, sub *storage.WebhookSubscription) error {
	return fmt.Errorf("not implemented: UpdateWebhookSubscription")
}

// DeleteWebhookSubscription removes a webhook subscription by ID.
func (s *Storage) DeleteWebhookSubscription(ctx context.Context, id int64) error {
	return fmt.Errorf("not implemented: DeleteWebhookSubscription")
}

// ListNotificationEvents returns paginated notification events with total count.
func (s *Storage) ListNotificationEvents(ctx context.Context, limit, offset int, eventType string) ([]*storage.NotificationEvent, int, error) {
	return nil, 0, fmt.Errorf("not implemented: ListNotificationEvents")
}
