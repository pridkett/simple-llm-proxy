package costmap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DefaultURL is the LiteLLM model cost and context window JSON source.
const DefaultURL = "https://raw.githubusercontent.com/BerriAI/litellm/refs/heads/main/model_prices_and_context_window.json"

// ModelSpec holds the cost and context window data for a single model.
type ModelSpec struct {
	MaxTokens                    int     `json:"max_tokens"`
	MaxInputTokens               int     `json:"max_input_tokens"`
	MaxOutputTokens              int     `json:"max_output_tokens"`
	InputCostPerToken            float64 `json:"input_cost_per_token"`
	OutputCostPerToken           float64 `json:"output_cost_per_token"`
	LiteLLMProvider              string  `json:"litellm_provider"`
	Mode                         string  `json:"mode"`
	SupportsFunctionCalling      bool    `json:"supports_function_calling"`
	SupportsParallelFunctionCalling bool `json:"supports_parallel_function_calling"`
	SupportsVision               bool    `json:"supports_vision"`
}

// Status is a snapshot of the Manager's current state, safe to return to callers.
type Status struct {
	Loaded     bool       `json:"loaded"`
	LoadedAt   *time.Time `json:"loaded_at,omitempty"`
	URL        string     `json:"url"`
	ModelCount int        `json:"model_count"`
}

// EffectiveSpecResult is returned by GetEffectiveSpec and describes how cost data was resolved.
type EffectiveSpecResult struct {
	Spec  ModelSpec
	Found bool
	// Source indicates how the spec was resolved:
	// "custom" = user-defined custom spec, "override" = cost map key override,
	// "auto" = matched via deployment actual_model, "" = not found.
	Source string
	// Key is the cost map key that was matched (empty when Source is "custom" or "").
	Key string
}

// Manager downloads and caches the LiteLLM cost/context map.
// All methods are safe for concurrent use.
type Manager struct {
	mu           sync.RWMutex
	sourceURL    string
	models       map[string]ModelSpec
	loadedAt     *time.Time
	httpClient   *http.Client
	overrideKeys map[string]string    // proxyModelName → cost map key override
	customSpecs  map[string]ModelSpec // proxyModelName → custom spec
}

// New creates a Manager with DefaultURL and a 30-second HTTP timeout.
func New() *Manager {
	return &Manager{
		sourceURL:    DefaultURL,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		overrideKeys: make(map[string]string),
		customSpecs:  make(map[string]ModelSpec),
	}
}

// Load fetches the cost map from the current URL and atomically stores it.
// The HTTP request is made outside the write lock to avoid blocking readers.
func (m *Manager) Load(ctx context.Context) error {
	m.mu.RLock()
	u := m.sourceURL
	m.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching cost map: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from cost map URL", resp.StatusCode)
	}

	// Decode loosely into map[string]interface{} so entries with unexpected
	// types (e.g. the "sample_spec" documentation stub) don't fail the entire
	// load. UseNumber preserves numeric precision.
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	var raw map[string]map[string]interface{}
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("decoding cost map JSON: %w", err)
	}

	models := make(map[string]ModelSpec, len(raw))
	for name, entry := range raw {
		models[name] = parseModelSpec(entry)
	}

	now := time.Now()
	m.mu.Lock()
	m.models = models
	m.loadedAt = &now
	m.mu.Unlock()

	return nil
}

// Reload is an alias for Load provided for semantic clarity at call sites.
func (m *Manager) Reload(ctx context.Context) error {
	return m.Load(ctx)
}

// SetURL changes the source URL. Returns an error if the URL is empty or not
// an http/https URL. Does not trigger a reload.
func (m *Manager) SetURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("URL must not be empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got %q", parsed.Scheme)
	}

	m.mu.Lock()
	m.sourceURL = rawURL
	m.mu.Unlock()
	return nil
}

// GetURL returns the current source URL.
func (m *Manager) GetURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sourceURL
}

// Status returns a snapshot of the manager's current state.
func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Status{
		Loaded:     m.models != nil,
		LoadedAt:   m.loadedAt,
		URL:        m.sourceURL,
		ModelCount: len(m.models),
	}
}

// parseModelSpec converts a loosely-typed map from JSON decoding into a ModelSpec.
// Unknown or non-numeric values are silently ignored (zero value used).
func parseModelSpec(entry map[string]interface{}) ModelSpec {
	return ModelSpec{
		MaxTokens:                    asInt(entry["max_tokens"]),
		MaxInputTokens:               asInt(entry["max_input_tokens"]),
		MaxOutputTokens:              asInt(entry["max_output_tokens"]),
		InputCostPerToken:            asFloat(entry["input_cost_per_token"]),
		OutputCostPerToken:           asFloat(entry["output_cost_per_token"]),
		LiteLLMProvider:              asString(entry["litellm_provider"]),
		Mode:                         asString(entry["mode"]),
		SupportsFunctionCalling:      asBool(entry["supports_function_calling"]),
		SupportsParallelFunctionCalling: asBool(entry["supports_parallel_function_calling"]),
		SupportsVision:               asBool(entry["supports_vision"]),
	}
}

func asInt(v interface{}) int {
	switch n := v.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
		if f, err := n.Float64(); err == nil {
			return int(f)
		}
	}
	return 0
}

func asFloat(v interface{}) float64 {
	switch n := v.(type) {
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f
		}
	}
	return 0
}

func asString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asBool(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// GetModel returns the ModelSpec for a given model name.
// Returns false if the cost map has not been loaded or the model is unknown.
func (m *Manager) GetModel(name string) (ModelSpec, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.models == nil {
		return ModelSpec{}, false
	}
	spec, ok := m.models[name]
	return spec, ok
}

// SetOverrideKey sets a cost map lookup key override for a proxy model name.
// When GetEffectiveSpec is called for this model, it will look up this key in the
// cost map instead of auto-detecting from candidate actual model strings.
// Pass key="" to clear a previous override (treated as no override).
func (m *Manager) SetOverrideKey(proxyModel, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key == "" {
		delete(m.overrideKeys, proxyModel)
	} else {
		m.overrideKeys[proxyModel] = key
	}
}

// SetCustomSpec stores a fully custom ModelSpec for a proxy model name.
// When GetEffectiveSpec is called for this model, this spec is returned directly
// without consulting the cost map.
func (m *Manager) SetCustomSpec(proxyModel string, spec ModelSpec) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customSpecs[proxyModel] = spec
}

// ClearOverride removes any override (key or custom) for the given proxy model name,
// reverting to auto-detection behaviour.
func (m *Manager) ClearOverride(proxyModel string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.overrideKeys, proxyModel)
	delete(m.customSpecs, proxyModel)
}

// GetEffectiveSpec resolves the ModelSpec for a proxy model name using this precedence:
//  1. Custom spec set via SetCustomSpec → Source="custom"
//  2. Override key set via SetOverrideKey, looked up in the cost map → Source="override"
//  3. Auto-detection: first candidateActualModel that matches in the cost map → Source="auto"
//  4. Not found → Found=false
//
// All reads are performed under a single read lock for consistency.
func (m *Manager) GetEffectiveSpec(proxyModel string, candidateActualModels []string) EffectiveSpecResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 1. Custom spec takes highest precedence.
	if spec, ok := m.customSpecs[proxyModel]; ok {
		return EffectiveSpecResult{Spec: spec, Found: true, Source: "custom"}
	}

	// 2. Override key.
	if key, ok := m.overrideKeys[proxyModel]; ok && key != "" {
		if spec, ok := m.models[key]; ok {
			return EffectiveSpecResult{Spec: spec, Found: true, Source: "override", Key: key}
		}
		// Override key is set but not found in the cost map (map not loaded or key typo).
		return EffectiveSpecResult{Found: false, Source: "override", Key: key}
	}

	// 3. Auto-detect from candidate actual model strings.
	for _, candidate := range candidateActualModels {
		if spec, ok := m.models[candidate]; ok {
			return EffectiveSpecResult{Spec: spec, Found: true, Source: "auto", Key: candidate}
		}
	}

	return EffectiveSpecResult{}
}
