package router

import (
	"context"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapacityReservations(t *testing.T) {
	l := newCapacityLimiter()
	now := time.Now()
	l.now = func() time.Time { return now }
	d := &provider.Deployment{ProviderName: "test", ActualModel: "m", APIKey: "one", RPM: 10, TPM: 10}
	first, err := l.reserve(d, 6)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.reserve(d, 6); err == nil {
		t.Fatal("concurrent TPM reservation exceeded quota")
	}
	first.finish(&model.Usage{TotalTokens: 3})
	first.finish(&model.Usage{TotalTokens: 1})
	second, err := l.reserve(d, 6)
	if err != nil {
		t.Fatal("actual usage did not release excess reservation", err)
	}
	second.finish(nil)
	if _, err = l.reserve(d, 2); err == nil {
		t.Fatal("unknown usage was treated as zero")
	}
	other := *d
	other.APIKey = "two"
	if _, err = l.reserve(&other, 6); err != nil {
		t.Fatal("different credential shares quota", err)
	}
	now = now.Add(time.Minute)
	if _, err = l.reserve(d, 10); err != nil {
		t.Fatal("minute quota not reset", err)
	}
	first.finish(&model.Usage{}) // finishing an old window cannot change current capacity
}
func TestConcurrentRPM(t *testing.T) {
	l := newCapacityLimiter()
	d := &provider.Deployment{ProviderName: "test", RPM: 3}
	var passed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := l.reserve(d, 1); err == nil {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	if passed.Load() != 3 {
		t.Fatalf("RPM admitted %d requests", passed.Load())
	}
}
func TestRouteCapacityAndCooldownErrors(t *testing.T) {
	cfg := makeMockConfig([]string{"test"}, "round-robin")
	cfg.ModelList[0].RPM = 1
	r, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	calls := 0
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		calls++
		return &model.ChatCompletionResponse{Usage: &model.Usage{TotalTokens: 2}}, nil, nil
	}
	if result := r.Route(context.Background(), "test", "", cb); result.Error != nil {
		t.Fatal(result.Error)
	}
	result := r.Route(context.Background(), "test", "", cb)
	rate, ok := result.Error.(*provider.RateLimitError)
	if !ok || rate.RetryAfter <= 0 || calls != 1 {
		t.Fatalf("capacity not enforced: %+v calls=%d", result, calls)
	}
	unknown := r.Route(context.Background(), "missing", "", cb)
	if e, ok := unknown.Error.(*model.ProxyError); !ok || e.StatusCode != 404 {
		t.Fatal(unknown.Error)
	}
	disabled := false
	cfg.ProviderPools = []config.ProviderPool{{Name: "pool", Enabled: &disabled, Members: []config.PoolMember{{ModelName: "test"}}}}
	cfg.ModelList[0].RPM = 0
	cfg.RouterSettings.AllowedFails = 1
	r2, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	d, _ := r2.GetDeployment("test")
	r2.ReportFailure(d)
	result = r2.Route(context.Background(), "test", "", cb)
	if e, ok := result.Error.(*model.ProxyError); !ok || e.StatusCode != 503 {
		t.Fatalf("cooldown disguised as unknown model: %v", result.Error)
	}
}
func TestCooledPoolIsNotSuccessful(t *testing.T) {
	cfg := makeMockConfigWithPools([]string{"one"}, "round-robin", []config.ProviderPool{{Name: "pool", Members: []config.PoolMember{{ModelName: "one"}}}})
	cfg.RouterSettings.AllowedFails = 1
	r, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	d, _ := r.GetDeployment("one")
	r.ReportFailure(d)
	result := r.Route(context.Background(), "one", "", func(*provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		t.Fatal("cooled provider called")
		return nil, nil, nil
	})
	if result.Error == nil {
		t.Fatal("empty pool incorrectly reported success")
	}
}

func TestStickyCapacityRetainsRateLimit(t *testing.T) {
	cfg := makeMockConfigWithPools([]string{"one"}, "round-robin", []config.ProviderPool{{Name: "pool", Members: []config.PoolMember{{ModelName: "one"}}}})
	cfg.ModelList[0].RPM = 1
	r, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cb := func(*provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{}, nil, nil
	}
	if got := r.Route(context.Background(), "one", "app", cb); got.Error != nil {
		t.Fatal(got.Error)
	}
	got := r.Route(context.Background(), "one", "app", cb)
	if _, ok := got.Error.(*provider.RateLimitError); !ok {
		t.Fatalf("sticky capacity became %v", got.Error)
	}
}
