package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestCreateWebhookSubscription(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	sub := &storage.WebhookSubscription{
		URL:     "https://example.com/hook",
		Events:  []string{"provider_failover", "budget_exhausted"},
		Secret:  "s3cret",
		Enabled: true,
	}

	created, err := s.CreateWebhookSubscription(ctx, sub)
	if err != nil {
		t.Fatalf("CreateWebhookSubscription failed: %v", err)
	}
	if created.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if created.URL != "https://example.com/hook" {
		t.Errorf("URL: got %q, want %q", created.URL, "https://example.com/hook")
	}
	if len(created.Events) != 2 {
		t.Errorf("Events length: got %d, want 2", len(created.Events))
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestListWebhookSubscriptions(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create two subscriptions
	sub1 := &storage.WebhookSubscription{
		URL:     "https://example.com/hook1",
		Events:  []string{"provider_failover"},
		Enabled: true,
	}
	sub2 := &storage.WebhookSubscription{
		URL:     "https://example.com/hook2",
		Events:  []string{"budget_exhausted"},
		Enabled: false,
	}

	if _, err := s.CreateWebhookSubscription(ctx, sub1); err != nil {
		t.Fatalf("CreateWebhookSubscription 1 failed: %v", err)
	}
	if _, err := s.CreateWebhookSubscription(ctx, sub2); err != nil {
		t.Fatalf("CreateWebhookSubscription 2 failed: %v", err)
	}

	subs, err := s.ListWebhookSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListWebhookSubscriptions failed: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}
	// Should be ordered by created_at DESC -- most recent first
	if subs[0].URL != "https://example.com/hook2" {
		t.Errorf("first subscription URL: got %q, want %q", subs[0].URL, "https://example.com/hook2")
	}
}

func TestUpdateWebhookSubscription(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	sub := &storage.WebhookSubscription{
		URL:     "https://example.com/hook",
		Events:  []string{"provider_failover"},
		Secret:  "old_secret",
		Enabled: true,
	}

	created, err := s.CreateWebhookSubscription(ctx, sub)
	if err != nil {
		t.Fatalf("CreateWebhookSubscription failed: %v", err)
	}

	// Update fields
	created.URL = "https://example.com/updated"
	created.Events = []string{"budget_exhausted", "cooldown_enter"}
	created.Secret = "new_secret"
	created.Enabled = false

	if err := s.UpdateWebhookSubscription(ctx, created); err != nil {
		t.Fatalf("UpdateWebhookSubscription failed: %v", err)
	}

	// Verify update by listing
	subs, err := s.ListWebhookSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListWebhookSubscriptions failed: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].URL != "https://example.com/updated" {
		t.Errorf("URL: got %q, want %q", subs[0].URL, "https://example.com/updated")
	}
	if len(subs[0].Events) != 2 {
		t.Errorf("Events length: got %d, want 2", len(subs[0].Events))
	}
	if subs[0].Secret != "new_secret" {
		t.Errorf("Secret: got %q, want %q", subs[0].Secret, "new_secret")
	}
	if subs[0].Enabled {
		t.Error("Enabled should be false after update")
	}
}

func TestDeleteWebhookSubscription(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	sub := &storage.WebhookSubscription{
		URL:     "https://example.com/hook",
		Events:  []string{"provider_failover"},
		Enabled: true,
	}

	created, err := s.CreateWebhookSubscription(ctx, sub)
	if err != nil {
		t.Fatalf("CreateWebhookSubscription failed: %v", err)
	}

	if err := s.DeleteWebhookSubscription(ctx, created.ID); err != nil {
		t.Fatalf("DeleteWebhookSubscription failed: %v", err)
	}

	subs, err := s.ListWebhookSubscriptions(ctx)
	if err != nil {
		t.Fatalf("ListWebhookSubscriptions failed: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("expected 0 subscriptions after delete, got %d", len(subs))
	}

	// Delete non-existent ID should not error
	if err := s.DeleteWebhookSubscription(ctx, 99999); err != nil {
		t.Errorf("DeleteWebhookSubscription for non-existent ID should not error: %v", err)
	}
}

func TestGetEnabledWebhooksByEvent(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create subscriptions with different events and enabled states
	subs := []*storage.WebhookSubscription{
		{URL: "https://a.com", Events: []string{"provider_failover", "budget_exhausted"}, Enabled: true},
		{URL: "https://b.com", Events: []string{"provider_failover"}, Enabled: true},
		{URL: "https://c.com", Events: []string{"budget_exhausted"}, Enabled: false}, // disabled
		{URL: "https://d.com", Events: []string{"cooldown_enter"}, Enabled: true},
	}
	for _, sub := range subs {
		if _, err := s.CreateWebhookSubscription(ctx, sub); err != nil {
			t.Fatalf("CreateWebhookSubscription failed: %v", err)
		}
	}

	// Query for provider_failover -- should return a.com and b.com (not c.com disabled, not d.com wrong event)
	result, err := s.GetEnabledWebhooksByEvent(ctx, "provider_failover")
	if err != nil {
		t.Fatalf("GetEnabledWebhooksByEvent failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 enabled subscriptions for provider_failover, got %d", len(result))
	}

	// Query for budget_exhausted -- should return only a.com (c.com is disabled)
	result, err = s.GetEnabledWebhooksByEvent(ctx, "budget_exhausted")
	if err != nil {
		t.Fatalf("GetEnabledWebhooksByEvent failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 enabled subscription for budget_exhausted, got %d", len(result))
	}
	if result[0].URL != "https://a.com" {
		t.Errorf("URL: got %q, want %q", result[0].URL, "https://a.com")
	}

	// Query for nonexistent event -- should return empty
	result, err = s.GetEnabledWebhooksByEvent(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetEnabledWebhooksByEvent failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 subscriptions for nonexistent event, got %d", len(result))
	}
}

func TestInsertNotificationEvent(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	id, err := s.InsertNotificationEvent(ctx, "provider_failover", `{"pool":"main","from":"openai","to":"anthropic"}`)
	if err != nil {
		t.Fatalf("InsertNotificationEvent failed: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestListNotificationEvents(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert multiple events
	for i := 0; i < 5; i++ {
		eventType := "provider_failover"
		if i%2 == 0 {
			eventType = "budget_exhausted"
		}
		if _, err := s.InsertNotificationEvent(ctx, eventType, `{"i":`+string(rune('0'+i))+`}`); err != nil {
			t.Fatalf("InsertNotificationEvent %d failed: %v", i, err)
		}
	}

	// List all with pagination
	events, total, err := s.ListNotificationEvents(ctx, 3, 0, "")
	if err != nil {
		t.Fatalf("ListNotificationEvents failed: %v", err)
	}
	if total != 5 {
		t.Errorf("total: got %d, want 5", total)
	}
	if len(events) != 3 {
		t.Errorf("events length: got %d, want 3", len(events))
	}

	// List with offset
	events, total, err = s.ListNotificationEvents(ctx, 10, 3, "")
	if err != nil {
		t.Fatalf("ListNotificationEvents offset failed: %v", err)
	}
	if total != 5 {
		t.Errorf("total: got %d, want 5", total)
	}
	if len(events) != 2 {
		t.Errorf("events length with offset 3: got %d, want 2", len(events))
	}

	// Filter by event type
	events, total, err = s.ListNotificationEvents(ctx, 10, 0, "budget_exhausted")
	if err != nil {
		t.Fatalf("ListNotificationEvents filtered failed: %v", err)
	}
	if total != 3 {
		t.Errorf("total for budget_exhausted: got %d, want 3", total)
	}
	if len(events) != 3 {
		t.Errorf("events length for budget_exhausted: got %d, want 3", len(events))
	}
}

func TestDeleteOldNotificationEvents(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert events
	for i := 0; i < 3; i++ {
		if _, err := s.InsertNotificationEvent(ctx, "test_event", `{}`); err != nil {
			t.Fatalf("InsertNotificationEvent %d failed: %v", i, err)
		}
	}

	// Delete events older than 1 hour from now (should delete none -- events just created)
	cutoff := time.Now().Add(-1 * time.Hour)
	count, err := s.DeleteOldNotificationEvents(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldNotificationEvents failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 deleted, got %d", count)
	}

	// Delete events older than 1 hour in the future (should delete all)
	cutoff = time.Now().Add(1 * time.Hour)
	count, err = s.DeleteOldNotificationEvents(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldNotificationEvents failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 deleted, got %d", count)
	}

	// Verify they're gone
	_, total, err := s.ListNotificationEvents(ctx, 10, 0, "")
	if err != nil {
		t.Fatalf("ListNotificationEvents failed: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 events after delete, got %d", total)
	}
}

func TestInsertWebhookDeliveryWithSubscriptionID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create a subscription and event first
	sub, err := s.CreateWebhookSubscription(ctx, &storage.WebhookSubscription{
		URL:     "https://example.com/hook",
		Events:  []string{"provider_failover"},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWebhookSubscription failed: %v", err)
	}

	eventID, err := s.InsertNotificationEvent(ctx, "provider_failover", `{"test":true}`)
	if err != nil {
		t.Fatalf("InsertNotificationEvent failed: %v", err)
	}

	// Insert delivery with subscription_id
	subID := sub.ID
	deliveryID, err := s.InsertWebhookDelivery(ctx, &subID, eventID)
	if err != nil {
		t.Fatalf("InsertWebhookDelivery failed: %v", err)
	}
	if deliveryID == 0 {
		t.Error("expected non-zero delivery ID")
	}
}

func TestInsertWebhookDeliveryWithNilSubscriptionID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create an event
	eventID, err := s.InsertNotificationEvent(ctx, "provider_failover", `{"test":true}`)
	if err != nil {
		t.Fatalf("InsertNotificationEvent failed: %v", err)
	}

	// Insert delivery with nil subscription_id (YAML webhook)
	deliveryID, err := s.InsertWebhookDelivery(ctx, nil, eventID)
	if err != nil {
		t.Fatalf("InsertWebhookDelivery with nil subscription_id failed: %v", err)
	}
	if deliveryID == 0 {
		t.Error("expected non-zero delivery ID")
	}
}

func TestUpdateWebhookDeliveryStatus(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create event and delivery
	eventID, err := s.InsertNotificationEvent(ctx, "provider_failover", `{}`)
	if err != nil {
		t.Fatalf("InsertNotificationEvent failed: %v", err)
	}

	deliveryID, err := s.InsertWebhookDelivery(ctx, nil, eventID)
	if err != nil {
		t.Fatalf("InsertWebhookDelivery failed: %v", err)
	}

	// Update status
	if err := s.UpdateWebhookDeliveryStatus(ctx, deliveryID, "delivered", 200, 1); err != nil {
		t.Fatalf("UpdateWebhookDeliveryStatus failed: %v", err)
	}

	// Verify the update by querying raw SQL
	var status string
	var responseCode, attemptCount int
	var lastAttemptAt *string
	err = s.db.QueryRowContext(ctx,
		"SELECT status, response_code, attempt_count, last_attempt_at FROM webhook_deliveries WHERE id = ?",
		deliveryID,
	).Scan(&status, &responseCode, &attemptCount, &lastAttemptAt)
	if err != nil {
		t.Fatalf("SELECT from webhook_deliveries failed: %v", err)
	}
	if status != "delivered" {
		t.Errorf("status: got %q, want %q", status, "delivered")
	}
	if responseCode != 200 {
		t.Errorf("response_code: got %d, want 200", responseCode)
	}
	if attemptCount != 1 {
		t.Errorf("attempt_count: got %d, want 1", attemptCount)
	}
	if lastAttemptAt == nil {
		t.Error("last_attempt_at should not be nil after update")
	}
}

func TestCascadeDeleteNotificationEventToDeliveries(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create subscription, event, and delivery
	sub, err := s.CreateWebhookSubscription(ctx, &storage.WebhookSubscription{
		URL:     "https://example.com/hook",
		Events:  []string{"provider_failover"},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWebhookSubscription failed: %v", err)
	}

	eventID, err := s.InsertNotificationEvent(ctx, "provider_failover", `{}`)
	if err != nil {
		t.Fatalf("InsertNotificationEvent failed: %v", err)
	}

	subID := sub.ID
	_, err = s.InsertWebhookDelivery(ctx, &subID, eventID)
	if err != nil {
		t.Fatalf("InsertWebhookDelivery failed: %v", err)
	}

	// Verify delivery exists
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM webhook_deliveries").Scan(&count); err != nil {
		t.Fatalf("counting deliveries: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 delivery, got %d", count)
	}

	// Delete the notification event -- should cascade to webhook_deliveries
	cutoff := time.Now().Add(1 * time.Hour)
	deleted, err := s.DeleteOldNotificationEvents(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldNotificationEvents failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 event deleted, got %d", deleted)
	}

	// Verify delivery was cascade-deleted
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM webhook_deliveries").Scan(&count); err != nil {
		t.Fatalf("counting deliveries after cascade: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 deliveries after cascade delete, got %d", count)
	}
}

func TestCascadeDeleteSubscriptionToDeliveries(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create subscription, event, and delivery
	sub, err := s.CreateWebhookSubscription(ctx, &storage.WebhookSubscription{
		URL:     "https://example.com/hook",
		Events:  []string{"provider_failover"},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWebhookSubscription failed: %v", err)
	}

	eventID, err := s.InsertNotificationEvent(ctx, "provider_failover", `{}`)
	if err != nil {
		t.Fatalf("InsertNotificationEvent failed: %v", err)
	}

	subID := sub.ID
	_, err = s.InsertWebhookDelivery(ctx, &subID, eventID)
	if err != nil {
		t.Fatalf("InsertWebhookDelivery failed: %v", err)
	}

	// Delete the subscription -- should cascade to webhook_deliveries
	if err := s.DeleteWebhookSubscription(ctx, sub.ID); err != nil {
		t.Fatalf("DeleteWebhookSubscription failed: %v", err)
	}

	// Verify delivery was cascade-deleted
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM webhook_deliveries").Scan(&count); err != nil {
		t.Fatalf("counting deliveries after cascade: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 deliveries after subscription cascade delete, got %d", count)
	}
}
