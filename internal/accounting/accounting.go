// Package accounting normalizes request usage and preserves the provenance of cost evidence.
package accounting

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

type Usage struct {
	Source        string        `json:"source"`
	InputTokens   *int64        `json:"input_tokens"`
	OutputTokens  *int64        `json:"output_tokens"`
	TotalTokens   *int64        `json:"total_tokens"`
	InputDetails  InputDetails  `json:"input_details"`
	OutputDetails OutputDetails `json:"output_details"`
}
type InputDetails struct {
	CacheReadTokens  *int64 `json:"cache_read_tokens"`
	CacheWriteTokens *int64 `json:"cache_write_tokens"`
}
type OutputDetails struct {
	ReasoningTokens *int64 `json:"reasoning_tokens"`
}
type Cost struct {
	Currency        string      `json:"currency"`
	Amount          *string     `json:"amount"`
	Source          string      `json:"source"`
	ReportedAmount  *string     `json:"reported_amount"`
	EstimatedAmount *string     `json:"estimated_amount"`
	ReportedVia     string      `json:"reported_via,omitempty"`
	Components      []Component `json:"components,omitempty"`
}
type Component struct {
	Type   string `json:"type"`
	Tokens int64  `json:"tokens"`
	Rate   string `json:"rate_per_token"`
	Amount string `json:"amount"`
}
type Pricing struct {
	Revision   string  `json:"revision"`
	Source     string  `json:"source"`
	Key        string  `json:"key,omitempty"`
	Input      *string `json:"input_per_token"`
	Output     *string `json:"output_per_token"`
	CacheRead  *string `json:"cache_read_per_token"`
	CacheWrite *string `json:"cache_write_per_token"`
}
type Attempt struct {
	nativeCaptured bool
	NativeUsage    json.RawMessage `json:"native_usage,omitempty"`
	Provider       string          `json:"provider"`
	Model          string          `json:"model"`
	DeploymentKey  string          `json:"deployment_key"`
	Pool           string          `json:"pool,omitempty"`
	Policy         string          `json:"policy"`
	Status         string          `json:"status"`
	Usage          Usage           `json:"usage"`
	Cost           Cost            `json:"cost"`
	Pricing        *Pricing        `json:"pricing,omitempty"`
	Warnings       []string        `json:"warnings,omitempty"`
}
type Record struct {
	ID        string    `json:"id"`
	RequestID string    `json:"request_id"`
	APIKeyID  *int64    `json:"-"`
	State     string    `json:"state"`
	Coverage  string    `json:"coverage"`
	Usage     Usage     `json:"usage"`
	Cost      Cost      `json:"cost"`
	Attempts  []Attempt `json:"attempts"`
}

// Collector is scoped to a single logical request. Attempts capture pricing at admission.
type Collector struct {
	mu      sync.Mutex
	record  Record
	current int
}
type collectorKey struct{}
type attemptKey struct{}
type attemptRef struct {
	c     *Collector
	index int
}

func New(requestID string, keyID *int64) *Collector {
	return &Collector{record: Record{ID: "acct_" + uuid.NewString(), RequestID: requestID, APIKeyID: keyID, State: "pending", Coverage: "unknown", Cost: Cost{Currency: "USD", Source: "unknown"}, Attempts: []Attempt{}}, current: -1}
}
func WithCollector(ctx context.Context, c *Collector) context.Context {
	return context.WithValue(ctx, collectorKey{}, c)
}
func FromContext(ctx context.Context) *Collector {
	c, _ := ctx.Value(collectorKey{}).(*Collector)
	return c
}
func (c *Collector) Begin(ctx context.Context, provider, proxyModel, actualModel, deploymentKey, pool, policy string, cm *costmap.Manager) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if policy == "" {
		policy = "prefer_upstream"
	}
	a := Attempt{Provider: provider, Model: actualModel, DeploymentKey: deploymentKey, Pool: pool, Policy: policy, Status: "pending", Usage: Usage{Source: "unknown"}, Cost: Cost{Currency: "USD", Source: "unknown"}}
	if cm != nil {
		spec := cm.GetEffectiveSpec(proxyModel, []string{actualModel})
		if spec.Found {
			a.Pricing = &Pricing{Source: spec.Source, Key: spec.Key, Input: pricedRate(spec.Spec, "input_cost_per_token", spec.Spec.InputCostPerToken), Output: pricedRate(spec.Spec, "output_cost_per_token", spec.Spec.OutputCostPerToken), CacheRead: pricedRate(spec.Spec, "cache_read_input_token_cost", spec.Spec.CacheReadInputTokenCost), CacheWrite: pricedRate(spec.Spec, "cache_creation_input_token_cost", spec.Spec.CacheCreationInputTokenCost)}

			data, _ := json.Marshal(a.Pricing)
			a.Pricing.Revision = fmt.Sprintf("%x", sha256.Sum256(data))
		}
	}
	c.record.Attempts = append(c.record.Attempts, a)
	c.current = len(c.record.Attempts) - 1
	return context.WithValue(ctx, attemptKey{}, attemptRef{c, c.current})
}
func rate(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
func ptr(v int64) *int64    { return &v }
func (c *Collector) ObserveUsage(u *model.Usage) {
	if u == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current < 0 {
		return
	}
	observeUsage(&c.record.Attempts[c.current], u)
}
func ObserveUsage(ctx context.Context, u *model.Usage) {
	if ref, ok := ctx.Value(attemptKey{}).(attemptRef); ok && u != nil {
		ref.c.mu.Lock()
		defer ref.c.mu.Unlock()
		observeUsage(&ref.c.record.Attempts[ref.index], u)
	}
}
func observeUsage(a *Attempt, u *model.Usage) {
	data := u.AccountingJSON()
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	if !a.nativeCaptured {
		mergeNative(a, data)
	}
	if a.Provider == "openrouter" {
		if value, ok := raw["cost"]; ok {
			setReported(a, strings.Trim(string(value), "\""), "body:usage.cost")
		}
	}
	in, out := int64(u.PromptTokens), int64(u.CompletionTokens)
	if in < 0 || out < 0 || int64(u.CacheReadTokens) < 0 || int64(u.CacheWriteTokens) < 0 {
		warn(a, "invalid negative token counts")
		return
	}
	read, write := int64(u.CacheReadTokens), int64(u.CacheWriteTokens)
	usage := Usage{Source: "upstream"}
	inputKnown := raw["prompt_tokens"] != nil || raw["input_tokens"] != nil
	outputKnown := raw["completion_tokens"] != nil || raw["output_tokens"] != nil
	if inputKnown {
		usage.InputTokens = ptr(in)
	}
	if outputKnown {
		usage.OutputTokens = ptr(out)
	}
	// Embeddings report prompt == total with no completion field.
	if inputKnown && !outputKnown && raw["total_tokens"] != nil && int64(u.TotalTokens) == in {
		usage.OutputTokens = ptr(0)
		outputKnown = true
	}
	if a.Provider == "anthropic" {
		if in > int64(^uint64(0)>>1)-read || in+read > int64(^uint64(0)>>1)-write {
			warn(a, "token count overflow")
			return
		}
		in += read + write
		if inputKnown {
			usage.InputTokens = ptr(in)
		}
		usage.InputDetails = InputDetails{ptr(read), ptr(write)}
	} else {
		for _, field := range []string{"prompt_tokens_details", "input_tokens_details"} {
			var d struct {
				Cached *int64 `json:"cached_tokens"`
			}
			if json.Unmarshal(raw[field], &d) == nil && d.Cached != nil {
				usage.InputDetails.CacheReadTokens = d.Cached
			}
		}
	}
	for _, field := range []string{"completion_tokens_details", "output_tokens_details"} {
		var d struct {
			Reasoning *int64 `json:"reasoning_tokens"`
		}
		if json.Unmarshal(raw[field], &d) == nil && d.Reasoning != nil {
			usage.OutputDetails.ReasoningTokens = d.Reasoning
		}
	}
	if v := usage.InputDetails.CacheReadTokens; v != nil && (*v < 0 || *v > in) {
		usage.InputDetails.CacheReadTokens = nil
		usage.InputTokens = nil
		warn(a, "invalid cached token count")
	}
	if v := usage.OutputDetails.ReasoningTokens; v != nil && (*v < 0 || *v > out) {
		usage.OutputDetails.ReasoningTokens = nil
		warn(a, "invalid reasoning token count")
	}
	if in > int64(^uint64(0)>>1)-out {
		warn(a, "token count overflow")
		return
	}
	if inputKnown && outputKnown {
		usage.TotalTokens = ptr(in + out)
	} else if raw["total_tokens"] != nil && u.TotalTokens >= 0 {
		usage.TotalTokens = ptr(int64(u.TotalTokens))
	}
	// Header totals cover the entire upstream request, possibly several internal models.
	if a.Usage.Source == "upstream_header" {
		usage.TotalTokens = a.Usage.TotalTokens
		usage.Source = "upstream_header"
		if !inputKnown || !outputKnown || *usage.TotalTokens != in+out {
			usage.InputTokens = nil
			usage.OutputTokens = nil
			usage.InputDetails = InputDetails{}
			usage.OutputDetails = OutputDetails{}
		}
	}
	if usage.TotalTokens == nil && usage.InputTokens == nil && usage.OutputTokens == nil {
		usage.Source = "unknown"
	}
	a.Usage = usage
}

// ObserveHeaders accepts only the supported accounting contract, never arbitrary response headers.
func ObserveHeaders(ctx context.Context, h http.Header) {
	ref, ok := ctx.Value(attemptKey{}).(attemptRef)
	if !ok {
		return
	}
	ref.c.mu.Lock()
	defer ref.c.mu.Unlock()
	a := &ref.c.record.Attempts[ref.index]
	if v := h.Get("x-litellm-response-cost"); v != "" {
		setReported(a, v, "header:x-litellm-response-cost")
	}
	if v := h.Get("x-litellm-total-tokens"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			warn(a, "invalid x-litellm-total-tokens")
		} else {
			a.Usage.TotalTokens = &n
			a.Usage.Source = "upstream_header"
		}
	}
}
func setReported(a *Attempt, s, via string) {
	v, err := Decimal(s)
	if err != nil {
		warn(a, "invalid upstream cost from "+via)
		return
	}
	if a.Cost.ReportedVia == "header:x-litellm-response-cost" && via != "header:x-litellm-response-cost" {
		return
	}
	a.Cost.ReportedAmount = &v
	a.Cost.ReportedVia = via
}

// ObserveJSON captures error-body usage and provider-specific cost fields, before decoding discards them.
func ObserveJSON(ctx context.Context, body []byte) {
	ref, ok := ctx.Value(attemptKey{}).(attemptRef)
	if !ok {
		return
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return
	}
	native := root["usage"]
	if len(native) == 0 {
		var message map[string]json.RawMessage
		json.Unmarshal(root["message"], &message)
		native = message["usage"]
	}
	if len(native) == 0 {
		if metadata := root["usageMetadata"]; len(metadata) > 0 {
			ref.c.mu.Lock()
			mergeNative(&ref.c.record.Attempts[ref.index], metadata)
			ref.c.record.Attempts[ref.index].nativeCaptured = true
			ref.c.mu.Unlock()
		}
		return
	}
	if string(native) == "null" {
		return
	}
	ref.c.mu.Lock()
	mergeNative(&ref.c.record.Attempts[ref.index], native)
	ref.c.record.Attempts[ref.index].nativeCaptured = true
	ref.c.mu.Unlock()
	obj := struct{ Usage json.RawMessage }{native}
	var fields map[string]json.RawMessage
	if json.Unmarshal(obj.Usage, &fields) != nil {
		return
	}
	var u model.Usage
	if json.Unmarshal(obj.Usage, &u) != nil {
		return
	}
	if ref.c.recordProvider(ref.index) == "anthropic" {
		json.Unmarshal(fields["cache_read_input_tokens"], &u.CacheReadTokens)
		json.Unmarshal(fields["cache_creation_input_tokens"], &u.CacheWriteTokens)
	}
	ObserveUsage(ctx, &u)
}
func (c *Collector) recordProvider(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.record.Attempts[i].Provider
}
func EndAttempt(ctx context.Context, err error) {
	ref, ok := ctx.Value(attemptKey{}).(attemptRef)
	if !ok {
		return
	}
	ref.c.mu.Lock()
	defer ref.c.mu.Unlock()
	a := &ref.c.record.Attempts[ref.index]
	if err != nil {
		a.Status = "failed"
	} else {
		a.Status = "completed"
	}
}
func (c *Collector) Snapshot(final bool) *Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.record
	if final {
		r.State = "final"
	}
	r.Attempts = make([]Attempt, len(c.record.Attempts))
	copy(r.Attempts, c.record.Attempts)
	r.Cost = Cost{Currency: "USD", Source: "unknown"}
	r.Usage = Usage{Source: "unknown"}
	r.Coverage = "unknown"
	known := 0
	partial := false
	var amounts, reported, estimated []string
	for i := range r.Attempts {
		a := &r.Attempts[i]
		price(a)
		if a.Cost.Source == "estimate" && (a.Status == "failed" || a.Status == "interrupted" || a.Status == "pending" && final) {
			partial = true
		}
		if a.Status == "pending" && final {
			a.Status = "interrupted"
		}
		if a.Cost.Amount != nil {
			known++
			amounts = append(amounts, *a.Cost.Amount)
			if r.Cost.Source == "unknown" {
				r.Cost.Source = a.Cost.Source
			} else if r.Cost.Source != a.Cost.Source {
				r.Cost.Source = "mixed"
			}
		}
		if a.Cost.ReportedAmount != nil {
			reported = append(reported, *a.Cost.ReportedAmount)
		}
		if a.Cost.EstimatedAmount != nil {
			estimated = append(estimated, *a.Cost.EstimatedAmount)
		}
	}
	if known > 0 {
		v := sum(amounts...)
		r.Cost.Amount = &v
		r.Coverage = "partial"
		if known == len(r.Attempts) && !partial {
			r.Coverage = "complete"
		}
	}
	if len(reported) > 0 && len(reported) == len(r.Attempts) {
		v := sum(reported...)
		r.Cost.ReportedAmount = &v
	}
	if len(estimated) > 0 && len(estimated) == len(r.Attempts) {
		v := sum(estimated...)
		r.Cost.EstimatedAmount = &v
	}
	aggregateUsage(&r)
	if len(r.Attempts) == 1 {
		r.Cost.ReportedVia = r.Attempts[0].Cost.ReportedVia
		r.Cost.Components = r.Attempts[0].Cost.Components
	}
	// Deep copy slices and pointers so callers cannot mutate evidence.
	b, _ := json.Marshal(r)
	var result Record
	json.Unmarshal(b, &result)
	result.APIKeyID = c.record.APIKeyID
	return &result
}
func aggregateUsage(r *Record) {
	if len(r.Attempts) == 0 {
		return
	}
	fields := []func(*Usage) **int64{func(u *Usage) **int64 { return &u.InputTokens }, func(u *Usage) **int64 { return &u.OutputTokens }, func(u *Usage) **int64 { return &u.TotalTokens }, func(u *Usage) **int64 { return &u.InputDetails.CacheReadTokens }, func(u *Usage) **int64 { return &u.InputDetails.CacheWriteTokens }, func(u *Usage) **int64 { return &u.OutputDetails.ReasoningTokens }}
	for _, field := range fields {
		var n int64
		all := true
		for i := range r.Attempts {
			v := *field(&r.Attempts[i].Usage)
			if v == nil {
				all = false
				break
			}
			if *v > int64(^uint64(0)>>1)-n {
				all = false
				break
			}
			n += *v
		}
		if all {
			*field(&r.Usage) = ptr(n)
		}
	}
	source := r.Attempts[0].Usage.Source
	for i := range r.Attempts {
		if r.Attempts[i].Usage.Source != source {
			source = "mixed"
		}
	}
	r.Usage.Source = source
	if r.Usage.TotalTokens == nil && r.Usage.InputTokens == nil && r.Usage.OutputTokens == nil {
		r.Usage.Source = "unknown"
	}
}
func price(a *Attempt) {
	a.Cost.Components = nil
	a.Cost.EstimatedAmount = nil
	a.Cost.Amount = nil
	a.Cost.Source = "unknown"
	if p := a.Pricing; p != nil && a.Usage.InputTokens != nil && a.Usage.OutputTokens != nil {
		input := *a.Usage.InputTokens
		cached, write := int64(0), int64(0)
		if a.Usage.InputDetails.CacheReadTokens != nil {
			cached = *a.Usage.InputDetails.CacheReadTokens
		}
		if a.Usage.InputDetails.CacheWriteTokens != nil {
			write = *a.Usage.InputDetails.CacheWriteTokens
		}
		if cached <= input && write <= input-cached && input >= 0 && *a.Usage.OutputTokens >= 0 && cached >= 0 && write >= 0 {
			specs := []struct {
				name   string
				tokens int64
				rate   *string
			}{{"input", input - cached - write, p.Input}, {"output", *a.Usage.OutputTokens, p.Output}, {"cache_read", cached, p.CacheRead}, {"cache_write", write, p.CacheWrite}}
			valid := true
			var amounts []string
			for _, s := range specs {
				if s.rate == nil {
					if s.tokens == 0 {
						continue
					}
					valid = false
					break
				}
				rate, err := Decimal(*s.rate)
				if err != nil {
					valid = false
					break
				}
				amount := multiply(rate, s.tokens)
				a.Cost.Components = append(a.Cost.Components, Component{s.name, s.tokens, rate, amount})
				amounts = append(amounts, amount)
			}
			if valid {
				v := sum(amounts...)
				a.Cost.EstimatedAmount = &v
			}
		}
	}
	selectCost(a)
}
func selectCost(a *Attempt) {
	if a.Policy != "estimate_only" && a.Cost.ReportedAmount != nil {
		a.Cost.Amount = a.Cost.ReportedAmount
		a.Cost.Source = "upstream"
	} else if a.Policy != "require_upstream" && a.Cost.EstimatedAmount != nil {
		a.Cost.Amount = a.Cost.EstimatedAmount
		a.Cost.Source = "estimate"
	}
}

var decimalPattern = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// Decimal accepts finite, nonnegative decimals. Bound input before arbitrary-precision parsing.
func Decimal(s string) (string, error) {
	if len(s) > 128 || !decimalPattern.MatchString(s) {
		return "", fmt.Errorf("invalid decimal")
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(s[i+1:])
		if err != nil || exponent < -36 || exponent > 36 {
			return "", fmt.Errorf("decimal exponent out of range")
		}
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() < 0 || r.Num().BitLen() > 256 || r.Denom().BitLen() > 256 {
		return "", fmt.Errorf("decimal out of range")
	}
	normalized := format(r)
	exact, _ := new(big.Rat).SetString(normalized)
	if exact.Cmp(r) != 0 {
		return "", fmt.Errorf("decimal precision exceeds 36 places")
	}
	return normalized, nil
}
func format(r *big.Rat) string {
	s := r.FloatString(36)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}
func sum(values ...string) string {
	r := new(big.Rat)
	for _, v := range values {
		n, _ := new(big.Rat).SetString(v)
		if n != nil {
			r.Add(r, n)
		}
	}
	return format(r)
}
func multiply(rate string, n int64) string {
	r, _ := new(big.Rat).SetString(rate)
	return format(r.Mul(r, new(big.Rat).SetInt64(n)))
}
func Float(s *string) float64 {
	if s == nil {
		return 0
	}
	v, _ := strconv.ParseFloat(*s, 64)
	return v
}

// Restore resumes a durable background request without treating each poll as a new model call.
func Restore(r *Record) *Collector {
	for i := range r.Attempts {
		r.Attempts[i].nativeCaptured = len(r.Attempts[i].NativeUsage) > 0
	}
	return &Collector{record: *r, current: len(r.Attempts) - 1}
}
func (c *Collector) CurrentContext(ctx context.Context) context.Context {
	return context.WithValue(WithCollector(ctx, c), attemptKey{}, attemptRef{c, c.current})
}

// CurrentUsage reconciles deployment TPM from the current attempt, including authoritative header totals.
func (c *Collector) CurrentUsage() *model.Usage {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current < 0 {
		return nil
	}
	u := c.record.Attempts[c.current].Usage
	if u.TotalTokens == nil {
		return nil
	}
	return &model.Usage{TotalTokens: int(*u.TotalTokens)}
}

func pricedRate(spec costmap.ModelSpec, key string, value float64) *string {
	if spec.KnownRates != nil && !spec.KnownRates[key] {
		return nil
	}
	v := rate(value)
	return &v
}

func warn(a *Attempt, message string) {
	for _, existing := range a.Warnings {
		if existing == message {
			return
		}
	}
	a.Warnings = append(a.Warnings, message)
}

func mergeNative(a *Attempt, data json.RawMessage) {
	var old, latest map[string]json.RawMessage
	json.Unmarshal(a.NativeUsage, &old)
	if json.Unmarshal(data, &latest) != nil || latest == nil {
		return
	}
	if old == nil {
		old = make(map[string]json.RawMessage)
	}
	for key, value := range latest {
		old[key] = value
	}
	a.NativeUsage, _ = json.Marshal(old)
}
