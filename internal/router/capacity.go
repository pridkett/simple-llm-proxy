package router

import (
	"crypto/sha256"
	"io"
	"sync"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// RouteOptions supplies a token estimate for admission before an upstream call.
// TPM reservations include input and maximum output. Actual usage reconciles
// the reservation when available; unknown usage retains the estimate.
type RouteOptions struct {
	// UsageSnapshot supplies authoritative usage from the current upstream attempt.
	UsageSnapshot func() *model.Usage
	// DeferReconcile transfers reconciliation to streaming completion/cancellation.
	DeferReconcile  func(func(*model.Usage))
	EstimatedTokens int
	BeforeAttempt   func(*provider.Deployment, string)
	AfterAttempt    func(*model.ChatCompletionResponse, error)
	Eligible        func(*provider.Deployment) bool
	// StopOnUpstreamError prevents replay of non-idempotent requests after dispatch.
	StopOnUpstreamError bool
}

func eligibleDeployment(d *provider.Deployment, opts []RouteOptions) bool {
	if len(opts) > 0 && opts[0].Eligible != nil {
		return opts[0].Eligible(d)
	}
	// Decision and audio-only deployments require an explicit endpoint filter.
	_, audioOnly := d.Provider.(provider.AudioGenerationProvider)
	return d.Decision == nil && !audioOnly
}

type capacityWindow struct {
	start            time.Time
	requests, tokens int
}

type capacityKey struct {
	deployment string
	credential [sha256.Size]byte
}

func deploymentCapacityKey(d *provider.Deployment) capacityKey {
	// Aliases for one credential share a quota and traffic counts.
	return capacityKey{d.DeploymentKey(), sha256.Sum256([]byte(d.APIKey))}
}

const trafficWindowSeconds = int64(time.Minute / time.Second)

type trafficCounts struct {
	requests, tokens int
}

type trafficBucket struct {
	second int64
	counts map[capacityKey]trafficCounts
}

type capacityLimiter struct {
	mu      sync.Mutex
	windows map[capacityKey]*capacityWindow
	traffic [trafficWindowSeconds]trafficBucket
	now     func() time.Time
}
type capacityReservation struct {
	once    sync.Once
	limiter *capacityLimiter
	key     capacityKey
	start   time.Time
	tokens  int
}

func newCapacityLimiter() *capacityLimiter {
	return &capacityLimiter{windows: make(map[capacityKey]*capacityWindow), now: time.Now}
}

// recordTraffic adds to the current second with the limiter lock held.
func (l *capacityLimiter) recordTraffic(key capacityKey, second int64, requests, tokens int) {
	index := (second%trafficWindowSeconds + trafficWindowSeconds) % trafficWindowSeconds
	bucket := &l.traffic[index]
	if bucket.counts == nil || bucket.second != second {
		*bucket = trafficBucket{second: second, counts: make(map[capacityKey]trafficCounts)}
	}
	counts := bucket.counts[key]
	counts.requests += requests
	counts.tokens += tokens
	bucket.counts[key] = counts
}

func (l *capacityLimiter) recentTraffic() map[capacityKey]trafficCounts {
	l.mu.Lock()
	defer l.mu.Unlock()
	second := l.now().Unix()
	totals := make(map[capacityKey]trafficCounts)
	for _, bucket := range l.traffic {
		if bucket.second <= second-trafficWindowSeconds || bucket.second > second {
			continue
		}
		for key, counts := range bucket.counts {
			total := totals[key]
			total.requests += counts.requests
			total.tokens += counts.tokens
			totals[key] = total
		}
	}
	return totals
}

func (l *capacityLimiter) reserve(d *provider.Deployment, tokens int) (*capacityReservation, error) {
	key := deploymentCapacityKey(d)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	reservation := capacityReservation{limiter: l, key: key}
	if d.RPM > 0 || d.TPM > 0 {
		tokens = max(tokens, 1)
		window := l.windows[key]
		if window == nil || now.Sub(window.start) >= time.Minute {
			window = &capacityWindow{start: now}
			l.windows[key] = window
		}
		if (d.RPM > 0 && window.requests >= d.RPM) || (d.TPM > 0 && window.tokens+tokens > d.TPM) {
			return nil, &provider.RateLimitError{Local: true, Provider: d.ProviderName, RetryAfter: time.Minute - now.Sub(window.start)}
		}
		window.requests++
		if d.TPM > 0 {
			window.tokens += tokens
			reservation.start = window.start
			reservation.tokens = tokens
		}
	}
	l.recordTraffic(key, now.Unix(), 1, 0)
	return &reservation, nil
}

func (r *capacityReservation) finish(usage *model.Usage) {
	if usage == nil {
		return
	}
	r.once.Do(func() {
		actual := usage.TotalTokens
		if actual == 0 {
			actual = usage.PromptTokens + usage.CompletionTokens
		}
		r.limiter.mu.Lock()
		defer r.limiter.mu.Unlock()
		if r.tokens > 0 {
			if w := r.limiter.windows[r.key]; w != nil && w.start.Equal(r.start) {
				w.tokens += actual - r.tokens
			}
		}
		if actual > 0 {
			r.limiter.recordTraffic(r.key, r.limiter.now().Unix(), 0, actual)
		}
	})
}

type capacityStream struct {
	provider.Stream
	reservation *capacityReservation
	usage       *model.Usage
}

func (s *capacityStream) Recv() (*model.StreamChunk, error) {
	chunk, err := s.Stream.Recv()
	if chunk != nil && chunk.Usage != nil {
		s.usage = chunk.Usage
	}
	if err == io.EOF {
		s.reservation.finish(s.usage)
	}
	return chunk, err
}
func (s *capacityStream) Close() error { s.reservation.finish(s.usage); return s.Stream.Close() }
func (r *Router) invoke(d *provider.Deployment, pool string, cb RouteCallback, opts ...RouteOptions) (*model.ChatCompletionResponse, provider.Stream, error) {
	estimate := 1
	if len(opts) > 0 {
		estimate = opts[0].EstimatedTokens
	}
	reservation, err := r.capacity.reserve(d, estimate)
	if err != nil {
		return nil, nil, err
	}
	if len(opts) > 0 && opts[0].BeforeAttempt != nil {
		opts[0].BeforeAttempt(d, pool)
	}
	response, stream, err := cb(d)
	if len(opts) > 0 && opts[0].AfterAttempt != nil {
		opts[0].AfterAttempt(response, err)
	}
	if err != nil {
		// HTTP rejections did not generate tokens. Transport failures may have.
		var usage *model.Usage
		if _, ok := err.(interface{ HTTPStatus() int }); ok {
			usage = &model.Usage{}
		}
		if len(opts) > 0 && opts[0].UsageSnapshot != nil {
			if reported := opts[0].UsageSnapshot(); reported != nil {
				usage = reported
			}
		}
		reservation.finish(usage)
		return nil, nil, err
	}
	if len(opts) > 0 && opts[0].DeferReconcile != nil {
		opts[0].DeferReconcile(reservation.finish)
		return response, stream, nil
	}
	if len(opts) > 0 && opts[0].UsageSnapshot != nil {
		if usage := opts[0].UsageSnapshot(); usage != nil {
			reservation.finish(usage)
			return response, stream, nil
		}
	}
	if stream != nil {
		return response, &capacityStream{Stream: stream, reservation: reservation}, nil
	}
	// Synchronous callbacks can return a lightweight response containing usage.
	if response != nil {
		reservation.finish(response.Usage)
	} else {
		reservation.finish(nil)
	}
	return response, stream, nil
}
