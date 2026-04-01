package webhook

import (
	"bytes"
	"context"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/logger"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

const (
	// channelBufferSize is the capacity of the async event channel.
	// Events beyond this capacity are dropped (non-blocking Emit).
	channelBufferSize = 256

	// httpTimeout is the deadline for a single webhook delivery HTTP call.
	httpTimeout = 15 * time.Second

	// maxDeliveryAttempts is the retry ceiling per target per event.
	maxDeliveryAttempts = 5

	// backoffBaseDelay is the base for full-jitter exponential backoff.
	backoffBaseDelay = 1 * time.Second

	// backoffMaxDelay caps the computed backoff delay.
	backoffMaxDelay = 60 * time.Second

	// cleanupInterval is how often the background cleanup loop runs.
	cleanupInterval = 24 * time.Hour

	// retentionDays is how many days of notification events to keep.
	retentionDays = 30

	// drainTimeout is the max time Close() waits to drain remaining events.
	drainTimeout = 5 * time.Second
)

// WebhookTarget unifies YAML and DB webhook sources for delivery.
type WebhookTarget struct {
	SubscriptionID *int64 // nil for YAML webhooks; non-nil for DB webhooks
	URL            string
	Secret         string
	Events         []string
}

// WebhookDispatcher is the central async delivery engine. It receives
// routing events via a buffered channel, merges YAML and DB webhook
// subscriptions at dispatch time, delivers signed HTTP POSTs, and retries
// failed deliveries with full-jitter exponential backoff.
//
// The dispatcher never blocks the LLM request path: Emit() uses a
// select/default pattern that drops events when the channel is full.
type WebhookDispatcher struct {
	eventCh  chan Event
	store    storage.Storage
	yamlHooks []config.WebhookConfig
	httpClient *http.Client
	stopCh   chan struct{}
	wg       sync.WaitGroup
	logger   zerolog.Logger

	// testBackoffScale overrides backoffBaseDelay in tests for fast retries.
	// Zero means use the production constant.
	testBackoffScale time.Duration
}

// New creates a WebhookDispatcher. store may be nil for tests that don't
// need persistence. yamlHooks are the YAML-configured webhooks.
func New(store storage.Storage, yamlHooks []config.WebhookConfig) *WebhookDispatcher {
	return &WebhookDispatcher{
		eventCh:    make(chan Event, channelBufferSize),
		store:      store,
		yamlHooks:  yamlHooks,
		httpClient: &http.Client{Timeout: httpTimeout},
		stopCh:     make(chan struct{}),
		logger:     logger.Component("webhook-dispatcher"),
	}
}

// Emit sends an event to the delivery channel. If the channel is full,
// the event is dropped with a warning log. This method never blocks.
func (d *WebhookDispatcher) Emit(event Event) {
	select {
	case d.eventCh <- event:
	default:
		d.logger.Warn().
			Str("event_type", string(event.Type)).
			Msg("webhook dispatch channel full, dropping event")
	}
}

// Start launches the delivery and cleanup goroutines. Both goroutines
// respect context cancellation and the Close() signal.
func (d *WebhookDispatcher) Start(ctx context.Context) {
	d.wg.Add(2)
	go d.deliveryLoop(ctx)
	go d.cleanupLoop(ctx)
}

// Close signals both goroutines to stop, drains remaining events with a
// timeout, and waits for all goroutines to finish.
func (d *WebhookDispatcher) Close() {
	close(d.stopCh)

	// Drain remaining events with a timeout.
	drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()

	for {
		select {
		case event, ok := <-d.eventCh:
			if !ok {
				goto done
			}
			d.processEvent(drainCtx, event)
		case <-drainCtx.Done():
			goto done
		default:
			goto done
		}
	}
done:

	d.wg.Wait()
}

// deliveryLoop reads events from the channel and processes each one.
func (d *WebhookDispatcher) deliveryLoop(ctx context.Context) {
	defer d.wg.Done()

	for {
		select {
		case event := <-d.eventCh:
			d.processEvent(ctx, event)
		case <-d.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// cleanupLoop periodically deletes notification events older than retentionDays.
func (d *WebhookDispatcher) cleanupLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if d.store == nil {
				continue
			}
			cutoff := time.Now().AddDate(0, 0, -retentionDays)
			deleted, err := d.store.DeleteOldNotificationEvents(ctx, cutoff)
			if err != nil {
				d.logger.Error().Err(err).Msg("failed to delete old notification events")
			} else if deleted > 0 {
				d.logger.Info().Int64("deleted", deleted).Msg("cleaned up old notification events")
			}
		case <-d.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// processEvent marshals the event, records it in the DB, collects matching
// targets, and delivers to each with retries.
func (d *WebhookDispatcher) processEvent(ctx context.Context, event Event) {
	payload, err := event.JSON()
	if err != nil {
		d.logger.Error().Err(err).Str("event_type", string(event.Type)).Msg("failed to marshal event")
		return
	}

	// Record the notification event in the database.
	var eventID int64
	if d.store != nil {
		eventID, err = d.store.InsertNotificationEvent(ctx, string(event.Type), string(payload))
		if err != nil {
			d.logger.Error().Err(err).Str("event_type", string(event.Type)).Msg("failed to insert notification event")
			// Continue: delivery should still be attempted even if DB logging fails.
		}
	}

	// Collect matching targets from both YAML and DB sources.
	targets := d.collectTargets(ctx, event.Type)

	// Deliver to each target.
	for _, target := range targets {
		d.deliverWithRetry(ctx, target, payload, eventID)
	}
}

// collectTargets merges YAML and DB webhook sources, filtering by event type.
func (d *WebhookDispatcher) collectTargets(ctx context.Context, eventType EventType) []WebhookTarget {
	var targets []WebhookTarget

	// YAML webhook sources.
	for _, hook := range d.yamlHooks {
		if !hook.Enabled {
			continue
		}
		if !containsEventType(hook.Events, string(eventType)) {
			continue
		}
		targets = append(targets, WebhookTarget{
			SubscriptionID: nil,
			URL:            hook.URL,
			Secret:         hook.Secret,
			Events:         hook.Events,
		})
	}

	// DB webhook sources.
	if d.store != nil {
		dbHooks, err := d.store.GetEnabledWebhooksByEvent(ctx, string(eventType))
		if err != nil {
			d.logger.Error().Err(err).
				Str("event_type", string(eventType)).
				Msg("failed to query DB webhooks")
		} else {
			for _, hook := range dbHooks {
				id := hook.ID
				targets = append(targets, WebhookTarget{
					SubscriptionID: &id,
					URL:            hook.URL,
					Secret:         hook.Secret,
					Events:         hook.Events,
				})
			}
		}
	}

	return targets
}

// deliverWithRetry attempts to deliver the payload to a target up to
// maxDeliveryAttempts times with full-jitter exponential backoff.
func (d *WebhookDispatcher) deliverWithRetry(ctx context.Context, target WebhookTarget, payload []byte, eventID int64) {
	for attempt := 1; attempt <= maxDeliveryAttempts; attempt++ {
		// Record delivery attempt.
		var deliveryID int64
		if d.store != nil {
			var err error
			deliveryID, err = d.store.InsertWebhookDelivery(ctx, target.SubscriptionID, eventID)
			if err != nil {
				d.logger.Error().Err(err).
					Str("url", target.URL).
					Int("attempt", attempt).
					Msg("failed to insert webhook delivery record")
			}
		}

		// Build HTTP request.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(payload))
		if err != nil {
			d.logger.Error().Err(err).Str("url", target.URL).Msg("failed to build webhook request")
			d.recordDeliveryStatus(ctx, deliveryID, "failed", 0, attempt)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		// Set HMAC signature header when secret is non-empty.
		if target.Secret != "" {
			req.Header.Set("X-Webhook-Signature", Sign(payload, target.Secret))
		}

		// Execute HTTP request.
		resp, err := d.httpClient.Do(req)
		if err != nil {
			d.logger.Warn().Err(err).
				Str("url", target.URL).
				Int("attempt", attempt).
				Msg("webhook delivery failed")
			d.recordDeliveryStatus(ctx, deliveryID, statusForAttempt(attempt), 0, attempt)
		} else {
			resp.Body.Close()

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				// Success.
				d.recordDeliveryStatus(ctx, deliveryID, "delivered", resp.StatusCode, attempt)
				d.logger.Debug().
					Str("url", target.URL).
					Int("status", resp.StatusCode).
					Int("attempt", attempt).
					Msg("webhook delivered")
				return
			}

			// Non-2xx: retry or fail.
			d.logger.Warn().
				Str("url", target.URL).
				Int("status", resp.StatusCode).
				Int("attempt", attempt).
				Msg("webhook delivery non-2xx response")
			d.recordDeliveryStatus(ctx, deliveryID, statusForAttempt(attempt), resp.StatusCode, attempt)
		}

		// If this was the final attempt, stop.
		if attempt >= maxDeliveryAttempts {
			d.logger.Error().
				Str("url", target.URL).
				Int("attempts", attempt).
				Msg("webhook delivery failed after max retries")
			return
		}

		// Wait with backoff before next attempt.
		delay := d.computeBackoff(attempt)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		}
	}
}

// computeBackoff calculates a full-jitter exponential backoff delay.
// Formula: delay = rand(0, min(maxDelay, base * 2^attempt))
func (d *WebhookDispatcher) computeBackoff(attempt int) time.Duration {
	base := backoffBaseDelay
	if d.testBackoffScale > 0 {
		base = d.testBackoffScale
	}

	maxD := backoffMaxDelay
	if d.testBackoffScale > 0 {
		// Scale max delay proportionally for tests.
		maxD = d.testBackoffScale * 60
	}

	cap := base * (1 << attempt)
	if cap > maxD || cap <= 0 {
		cap = maxD
	}

	return time.Duration(rand.Int63n(int64(cap)))
}

// recordDeliveryStatus is a nil-safe wrapper around store.UpdateWebhookDeliveryStatus.
func (d *WebhookDispatcher) recordDeliveryStatus(ctx context.Context, deliveryID int64, status string, responseCode int, attemptCount int) {
	if d.store == nil || deliveryID == 0 {
		return
	}
	if err := d.store.UpdateWebhookDeliveryStatus(ctx, deliveryID, status, responseCode, attemptCount); err != nil {
		d.logger.Error().Err(err).
			Int64("delivery_id", deliveryID).
			Str("status", status).
			Msg("failed to update delivery status")
	}
}

// statusForAttempt returns "failed" for the last attempt, "pending" otherwise.
func statusForAttempt(attempt int) string {
	if attempt >= maxDeliveryAttempts {
		return "failed"
	}
	return "pending"
}

// containsEventType checks if the events list contains the given event type.
func containsEventType(events []string, eventType string) bool {
	for _, e := range events {
		if e == eventType {
			return true
		}
	}
	return false
}
