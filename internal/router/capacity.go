package router

import (
	"crypto/sha256"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// RouteOptions supplies a token estimate for admission before an upstream call.
// TPM reservations include input and maximum output. Actual usage reconciles
// the reservation when available; unknown usage retains the estimate.
type RouteOptions struct{ EstimatedTokens int }
type capacityWindow struct {
	start            time.Time
	requests, tokens int
}
type capacityLimiter struct {
	mu      sync.Mutex
	windows map[string]*capacityWindow
	now     func() time.Time
}
type capacityReservation struct {
	once    sync.Once
	limiter *capacityLimiter
	key     string
	start   time.Time
	tokens  int
}

func newCapacityLimiter() *capacityLimiter {
	return &capacityLimiter{windows: make(map[string]*capacityWindow), now: time.Now}
}
func (l *capacityLimiter) reserve(d *provider.Deployment, tokens int) (*capacityReservation, error) {
	if d.RPM <= 0 && d.TPM <= 0 {
		return nil, nil
	}
	if tokens < 1 {
		tokens = 1
	}
	// Aliases for one credential share a quota; distinct provider keys do not.
	key := fmt.Sprintf("%s:%x", d.DeploymentKey(), sha256.Sum256([]byte(d.APIKey)))
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
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
	} else {
		tokens = 0
	}
	return &capacityReservation{limiter: l, key: key, start: window.start, tokens: tokens}, nil
}
func (r *capacityReservation) finish(usage *model.Usage) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if usage == nil || r.tokens == 0 {
			return
		}
		r.limiter.mu.Lock()
		defer r.limiter.mu.Unlock()
		if w := r.limiter.windows[r.key]; w != nil && w.start.Equal(r.start) {
			actual := usage.TotalTokens
			if actual == 0 {
				actual = usage.PromptTokens + usage.CompletionTokens
			}
			w.tokens += actual - r.tokens
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
func (r *Router) invoke(d *provider.Deployment, cb RouteCallback, opts ...RouteOptions) (*model.ChatCompletionResponse, provider.Stream, error) {
	estimate := 1
	if len(opts) > 0 {
		estimate = opts[0].EstimatedTokens
	}
	reservation, err := r.capacity.reserve(d, estimate)
	if err != nil {
		return nil, nil, err
	}
	response, stream, err := cb(d)
	if err != nil {
		// HTTP rejections did not generate tokens. Transport failures may have.
		var usage *model.Usage
		if _, ok := err.(interface{ HTTPStatus() int }); ok {
			usage = &model.Usage{}
		}
		reservation.finish(usage)
		return nil, nil, err
	}
	if stream != nil && reservation != nil {
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
