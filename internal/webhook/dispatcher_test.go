package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// --- Mock Storage ---

// mockStore records webhook-related storage calls for test assertions.
// It panics for any method not explicitly overridden — this ensures tests
// only use the methods they expect.
type mockStore struct {
	mu sync.Mutex

	// Recorded calls
	notificationEvents []mockNotificationEvent
	deliveries         []mockDelivery
	deliveryUpdates    []mockDeliveryUpdate
	enabledWebhooks    []*storage.WebhookSubscription

	// Control
	nextNotificationID int64
	nextDeliveryID     int64
}

type mockNotificationEvent struct {
	eventType string
	payload   string
}

type mockDelivery struct {
	subscriptionID *int64
	eventID        int64
}

type mockDeliveryUpdate struct {
	id           int64
	status       string
	responseCode int
	attemptCount int
}

func newMockStore() *mockStore {
	return &mockStore{
		nextNotificationID: 1,
		nextDeliveryID:     1,
	}
}

// --- Storage interface methods used by dispatcher ---

func (m *mockStore) GetEnabledWebhooksByEvent(_ context.Context, eventType string) ([]*storage.WebhookSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabledWebhooks, nil
}

func (m *mockStore) InsertNotificationEvent(_ context.Context, eventType string, payload string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notificationEvents = append(m.notificationEvents, mockNotificationEvent{eventType: eventType, payload: payload})
	id := m.nextNotificationID
	m.nextNotificationID++
	return id, nil
}

func (m *mockStore) InsertWebhookDelivery(_ context.Context, subscriptionID *int64, eventID int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliveries = append(m.deliveries, mockDelivery{subscriptionID: subscriptionID, eventID: eventID})
	id := m.nextDeliveryID
	m.nextDeliveryID++
	return id, nil
}

func (m *mockStore) UpdateWebhookDeliveryStatus(_ context.Context, id int64, status string, responseCode int, attemptCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliveryUpdates = append(m.deliveryUpdates, mockDeliveryUpdate{id: id, status: status, responseCode: responseCode, attemptCount: attemptCount})
	return nil
}

func (m *mockStore) DeleteOldNotificationEvents(_ context.Context, olderThan time.Time) (int64, error) {
	return 0, nil
}

// --- Unused Storage interface stubs (panic on call) ---

func (m *mockStore) Initialize(context.Context) error                               { panic("not used") }
func (m *mockStore) Close() error                                                    { panic("not used") }
func (m *mockStore) LogRequest(context.Context, *storage.RequestLog) error           { panic("not used") }
func (m *mockStore) GetLogs(context.Context, int, int) ([]*storage.RequestLog, int, error) {
	panic("not used")
}
func (m *mockStore) UpsertCostMapKey(context.Context, string, string) error          { panic("not used") }
func (m *mockStore) UpsertCustomCostSpec(context.Context, string, string) error      { panic("not used") }
func (m *mockStore) GetCostOverride(context.Context, string) (*storage.CostOverride, error) {
	panic("not used")
}
func (m *mockStore) DeleteCostOverride(context.Context, string) error                { panic("not used") }
func (m *mockStore) ListCostOverrides(context.Context) ([]*storage.CostOverride, error) {
	panic("not used")
}
func (m *mockStore) UpsertUser(context.Context, *storage.User) error                 { panic("not used") }
func (m *mockStore) GetUser(context.Context, string) (*storage.User, error)          { panic("not used") }
func (m *mockStore) ListUsers(context.Context) ([]*storage.User, error)              { panic("not used") }
func (m *mockStore) CreateTeam(context.Context, string) (*storage.Team, error)       { panic("not used") }
func (m *mockStore) DeleteTeam(context.Context, int64) error                         { panic("not used") }
func (m *mockStore) ListTeams(context.Context) ([]*storage.Team, error)              { panic("not used") }
func (m *mockStore) AddTeamMember(context.Context, int64, string, string) error      { panic("not used") }
func (m *mockStore) RemoveTeamMember(context.Context, int64, string) error           { panic("not used") }
func (m *mockStore) UpdateTeamMemberRole(context.Context, int64, string, string) error {
	panic("not used")
}
func (m *mockStore) ListTeamMembers(context.Context, int64) ([]*storage.TeamMember, error) {
	panic("not used")
}
func (m *mockStore) ListMyTeams(context.Context, string) ([]*storage.TeamMember, error) {
	panic("not used")
}
func (m *mockStore) CreateApplication(context.Context, int64, string) (*storage.Application, error) {
	panic("not used")
}
func (m *mockStore) DeleteApplication(context.Context, int64) error                   { panic("not used") }
func (m *mockStore) ListApplications(context.Context, int64) ([]*storage.Application, error) {
	panic("not used")
}
func (m *mockStore) CleanExpiredSessions(context.Context) error                       { panic("not used") }
func (m *mockStore) CreateAPIKey(context.Context, int64, string, string, string, *int, *int, *float64, *float64, []string) (*storage.APIKey, error) {
	panic("not used")
}
func (m *mockStore) GetAPIKeyByHash(context.Context, string) (*storage.APIKey, error) {
	panic("not used")
}
func (m *mockStore) ListAPIKeys(context.Context, int64) ([]*storage.APIKey, error) {
	panic("not used")
}
func (m *mockStore) RevokeAPIKey(context.Context, int64) error                        { panic("not used") }
func (m *mockStore) GetAPIKeyByID(context.Context, int64) (*storage.APIKey, error)    { panic("not used") }
func (m *mockStore) ListUserAccessibleKeys(context.Context, string) ([]*storage.AccessibleKey, error) {
	panic("not used")
}
func (m *mockStore) GetKeyAllowedModels(context.Context, int64) ([]string, error) {
	panic("not used")
}
func (m *mockStore) UpdateKeyAllowedModels(context.Context, int64, []string) error {
	panic("not used")
}
func (m *mockStore) UpdateAPIKey(context.Context, int64, string, *int, *int, *float64, *float64, []string) error {
	panic("not used")
}
func (m *mockStore) RecordKeySpend(context.Context, int64, float64) error             { panic("not used") }
func (m *mockStore) GetKeySpendTotals(context.Context) (map[int64]float64, error) {
	panic("not used")
}
func (m *mockStore) FlushKeySpend(context.Context, int64, float64) error              { panic("not used") }
func (m *mockStore) GetSpendSummary(context.Context, time.Time, time.Time, storage.SpendFilters) ([]storage.SpendRow, error) {
	panic("not used")
}
func (m *mockStore) GetModelSpend(context.Context, time.Time, time.Time, storage.SpendFilters) ([]storage.ModelSpendRow, error) {
	panic("not used")
}
func (m *mockStore) GetDailySpend(context.Context, time.Time, time.Time, storage.SpendFilters) ([]storage.DailySpendRow, error) {
	panic("not used")
}
func (m *mockStore) GetStickySession(context.Context, string, string) (string, error) {
	panic("not used")
}
func (m *mockStore) UpsertStickySession(context.Context, string, string, string) error {
	panic("not used")
}
func (m *mockStore) DeleteExpiredStickySessions(context.Context, time.Time) (int64, error) {
	panic("not used")
}
func (m *mockStore) BulkUpsertStickySessions(context.Context, []storage.StickySession) error {
	panic("not used")
}
func (m *mockStore) GetPoolBudgetState(context.Context) ([]storage.PoolBudgetRow, error) {
	panic("not used")
}
func (m *mockStore) UpsertPoolBudgetState(context.Context, string, float64, string) error {
	panic("not used")
}
func (m *mockStore) ListWebhookSubscriptions(context.Context) ([]*storage.WebhookSubscription, error) {
	panic("not used")
}
func (m *mockStore) CreateWebhookSubscription(context.Context, *storage.WebhookSubscription) (*storage.WebhookSubscription, error) {
	panic("not used")
}
func (m *mockStore) UpdateWebhookSubscription(context.Context, *storage.WebhookSubscription) error {
	panic("not used")
}
func (m *mockStore) DeleteWebhookSubscription(context.Context, int64) error           { panic("not used") }
func (m *mockStore) ListNotificationEvents(context.Context, int, int, string) ([]*storage.NotificationEvent, int, error) {
	panic("not used")
}

// --- Helper to get delivery updates ---

func (m *mockStore) getDeliveryUpdates() []mockDeliveryUpdate {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mockDeliveryUpdate, len(m.deliveryUpdates))
	copy(result, m.deliveryUpdates)
	return result
}

func (m *mockStore) getNotificationEvents() []mockNotificationEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mockNotificationEvent, len(m.notificationEvents))
	copy(result, m.notificationEvents)
	return result
}

func (m *mockStore) getDeliveries() []mockDelivery {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mockDelivery, len(m.deliveries))
	copy(result, m.deliveries)
	return result
}

// --- Tests ---

func TestDispatcherDeliverySuccess(t *testing.T) {
	var received atomic.Int32
	var receivedBody []byte
	var receivedContentType string
	var bodyMu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyMu.Lock()
		receivedBody, _ = io.ReadAll(r.Body)
		receivedContentType = r.Header.Get("Content-Type")
		bodyMu.Unlock()
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newMockStore()
	yamlHooks := []config.WebhookConfig{
		{URL: srv.URL, Events: []string{"provider_failover"}, Enabled: true},
	}

	d := New(store, yamlHooks)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Emit(Event{
		Type:      EventProviderFailover,
		Timestamp: time.Now().UTC(),
		Value1:    "gpt-4",
		Value2:    "openai/gpt-4 -> anthropic/claude-3",
		Value3:    "provider_error",
	})

	// Wait for delivery
	deadline := time.After(5 * time.Second)
	for received.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for webhook delivery")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	d.Close()

	bodyMu.Lock()
	defer bodyMu.Unlock()
	if receivedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", receivedContentType, "application/json")
	}
	if len(receivedBody) == 0 {
		t.Error("expected non-empty body")
	}

	// Verify delivery was recorded as "delivered"
	updates := store.getDeliveryUpdates()
	found := false
	for _, u := range updates {
		if u.status == "delivered" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected delivery status update with 'delivered'")
	}
}

func TestDispatcherHMACSignature(t *testing.T) {
	var receivedSig string
	var receivedBodyBytes []byte
	var sigMu sync.Mutex
	secret := "test-secret-key"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigMu.Lock()
		receivedSig = r.Header.Get("X-Webhook-Signature")
		receivedBodyBytes, _ = io.ReadAll(r.Body)
		sigMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newMockStore()
	yamlHooks := []config.WebhookConfig{
		{URL: srv.URL, Events: []string{"provider_failover"}, Secret: secret, Enabled: true},
	}

	d := New(store, yamlHooks)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Emit(Event{
		Type:      EventProviderFailover,
		Timestamp: time.Now().UTC(),
		Value1:    "gpt-4",
	})

	// Wait for delivery
	deadline := time.After(5 * time.Second)
	for {
		sigMu.Lock()
		sig := receivedSig
		body := receivedBodyBytes
		sigMu.Unlock()
		if sig != "" {
			expected := Sign(body, secret)
			if sig != expected {
				t.Errorf("signature = %q, want %q", sig, expected)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for webhook delivery")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	d.Close()
}

func TestDispatcherNoSignatureWhenNoSecret(t *testing.T) {
	var headerPresent atomic.Int32
	var done atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Webhook-Signature") != "" {
			headerPresent.Add(1)
		}
		done.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newMockStore()
	yamlHooks := []config.WebhookConfig{
		{URL: srv.URL, Events: []string{"provider_failover"}, Secret: "", Enabled: true},
	}

	d := New(store, yamlHooks)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Emit(Event{
		Type:      EventProviderFailover,
		Timestamp: time.Now().UTC(),
		Value1:    "gpt-4",
	})

	deadline := time.After(5 * time.Second)
	for done.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for delivery")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	d.Close()

	if headerPresent.Load() > 0 {
		t.Error("X-Webhook-Signature header should NOT be present when secret is empty")
	}
}

func TestDispatcherMergeYAMLAndDB(t *testing.T) {
	var yamlReceived atomic.Int32
	var dbReceived atomic.Int32

	yamlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		yamlReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer yamlSrv.Close()

	dbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dbReceived.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer dbSrv.Close()

	subID := int64(42)
	store := newMockStore()
	store.enabledWebhooks = []*storage.WebhookSubscription{
		{ID: subID, URL: dbSrv.URL, Events: []string{"provider_failover"}, Enabled: true},
	}

	yamlHooks := []config.WebhookConfig{
		{URL: yamlSrv.URL, Events: []string{"provider_failover"}, Enabled: true},
	}

	d := New(store, yamlHooks)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Emit(Event{
		Type:      EventProviderFailover,
		Timestamp: time.Now().UTC(),
		Value1:    "gpt-4",
	})

	deadline := time.After(5 * time.Second)
	for yamlReceived.Load() == 0 || dbReceived.Load() == 0 {
		select {
		case <-deadline:
			t.Fatalf("timed out: yaml=%d db=%d", yamlReceived.Load(), dbReceived.Load())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	d.Close()

	if yamlReceived.Load() != 1 {
		t.Errorf("YAML webhook received %d calls, want 1", yamlReceived.Load())
	}
	if dbReceived.Load() != 1 {
		t.Errorf("DB webhook received %d calls, want 1", dbReceived.Load())
	}
}

func TestDispatcherEventTypeFiltering(t *testing.T) {
	var received atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newMockStore()
	yamlHooks := []config.WebhookConfig{
		{URL: srv.URL, Events: []string{"budget_exhausted"}, Enabled: true},
	}

	d := New(store, yamlHooks)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	// Emit provider_failover -- should NOT match the budget_exhausted webhook
	d.Emit(Event{
		Type:      EventProviderFailover,
		Timestamp: time.Now().UTC(),
		Value1:    "gpt-4",
	})

	// Give time for processing
	time.Sleep(500 * time.Millisecond)
	d.Close()

	if received.Load() != 0 {
		t.Errorf("webhook received %d calls, want 0 (event type mismatch)", received.Load())
	}
}

func TestDispatcherRetryOnFailure(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newMockStore()
	yamlHooks := []config.WebhookConfig{
		{URL: srv.URL, Events: []string{"provider_failover"}, Enabled: true},
	}

	d := New(store, yamlHooks)
	// Use a very short backoff for testing
	d.testBackoffScale = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Emit(Event{
		Type:      EventProviderFailover,
		Timestamp: time.Now().UTC(),
		Value1:    "gpt-4",
	})

	deadline := time.After(10 * time.Second)
	for {
		updates := store.getDeliveryUpdates()
		delivered := false
		for _, u := range updates {
			if u.status == "delivered" {
				delivered = true
				break
			}
		}
		if delivered {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for successful retry")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	d.Close()

	if attempts.Load() != 3 {
		t.Errorf("attempts = %d, want 3 (2 failures + 1 success)", attempts.Load())
	}
}

func TestDispatcherMaxRetries(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := newMockStore()
	yamlHooks := []config.WebhookConfig{
		{URL: srv.URL, Events: []string{"provider_failover"}, Enabled: true},
	}

	d := New(store, yamlHooks)
	d.testBackoffScale = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Emit(Event{
		Type:      EventProviderFailover,
		Timestamp: time.Now().UTC(),
		Value1:    "gpt-4",
	})

	deadline := time.After(10 * time.Second)
	for {
		updates := store.getDeliveryUpdates()
		failed := false
		for _, u := range updates {
			if u.status == "failed" {
				failed = true
				break
			}
		}
		if failed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for final 'failed' status")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	d.Close()

	if attempts.Load() != 5 {
		t.Errorf("attempts = %d, want 5", attempts.Load())
	}

	// Verify final status update is "failed"
	updates := store.getDeliveryUpdates()
	lastStatus := updates[len(updates)-1]
	if lastStatus.status != "failed" {
		t.Errorf("final status = %q, want %q", lastStatus.status, "failed")
	}
}

func TestDispatcherEmitNonBlocking(t *testing.T) {
	store := newMockStore()
	d := New(store, nil)
	// Don't start the dispatcher -- channel will fill up

	// Fill channel to capacity
	for i := 0; i < 256; i++ {
		d.Emit(Event{
			Type:      EventProviderFailover,
			Timestamp: time.Now().UTC(),
			Value1:    "gpt-4",
		})
	}

	// This call should not block or panic (channel full, drops event)
	done := make(chan struct{})
	go func() {
		d.Emit(Event{
			Type:      EventProviderFailover,
			Timestamp: time.Now().UTC(),
			Value1:    "overflow",
		})
		close(done)
	}()

	select {
	case <-done:
		// Success: Emit returned immediately
	case <-time.After(1 * time.Second):
		t.Fatal("Emit blocked when channel was full")
	}
}

func TestDispatcherNotificationEventRecorded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := newMockStore()
	yamlHooks := []config.WebhookConfig{
		{URL: srv.URL, Events: []string{"budget_exhausted"}, Enabled: true},
	}

	d := New(store, yamlHooks)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Emit(Event{
		Type:      EventBudgetExhausted,
		Timestamp: time.Now().UTC(),
		Value1:    "gpt-4",
		Value2:    "default",
		Value3:    "daily budget exhausted",
	})

	deadline := time.After(5 * time.Second)
	for {
		events := store.getNotificationEvents()
		if len(events) > 0 {
			if events[0].eventType != "budget_exhausted" {
				t.Errorf("event type = %q, want %q", events[0].eventType, "budget_exhausted")
			}
			if events[0].payload == "" {
				t.Error("expected non-empty payload JSON")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for notification event insertion")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	d.Close()
}

func TestDispatcherClose(t *testing.T) {
	store := newMockStore()
	d := New(store, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	// Emit an event (it will be processed or drained)
	d.Emit(Event{
		Type:      EventProviderFailover,
		Timestamp: time.Now().UTC(),
		Value1:    "gpt-4",
	})

	// Close should return without hanging (goroutines shut down)
	done := make(chan struct{})
	go func() {
		d.Close()
		close(done)
	}()

	select {
	case <-done:
		// Success: Close returned, no goroutine leak
	case <-time.After(10 * time.Second):
		t.Fatal("Close() did not return within timeout -- possible goroutine leak")
	}
}

func TestDispatcherBackoffJitter(t *testing.T) {
	// Verify that backoff uses full-jitter (delay >= 0 and < cap).
	// We call computeBackoff multiple times and check the distribution is varied.
	d := New(nil, nil)
	d.testBackoffScale = time.Millisecond // speed up test

	seen := make(map[time.Duration]bool)
	for i := 0; i < 50; i++ {
		delay := d.computeBackoff(1)
		if delay < 0 {
			t.Errorf("backoff delay = %v, want >= 0", delay)
		}
		// At attempt 1: cap = base*2^1 = 2ms (with testBackoffScale=1ms)
		// Actual cap in production would be 2s. With test scale, max is 2ms.
		cap := d.testBackoffScale * (1 << 1)
		if delay > cap {
			t.Errorf("backoff delay = %v, exceeds cap %v", delay, cap)
		}
		seen[delay] = true
	}
	// With jitter, we should see multiple distinct values
	if len(seen) < 2 {
		t.Errorf("expected jitter to produce varied delays, got %d distinct values", len(seen))
	}
}
