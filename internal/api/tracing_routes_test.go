package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestDecisionTraceCapture(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	f := newDecisionFixtureConfigured(t, provider.Tracer("test"), func(cfg *config.Config, appID int64) {
		cfg.OTelSettings.MessageCapture.DecisionApplicationIDs = []int64{appID}
		cfg.OTelSettings.MessageCapture.MaxBytes = 1024
		cfg.OTelSettings.LangfuseCompat = true
	})
	key, _ := f.key(t, "capture", true, nil, nil, nil)
	systemOne := `{"model":"jev","state":{"ticket":"damaged box","password":"secret-value"},` +
		`"assets":{"photo":{"type":"image","format":"png","data_base64":"inline-payload"}},` +
		`"questions":{"damage":{"type":"box","instructions":"Locate damage."}}}`
	for _, tc := range []struct {
		name, path, body, token, input, output, flag string
		status                                       int
	}{
		{"decisions", "/v1/decisions", decisionBody("luna"), key, `"input":"ticket"`, `"probability":0.9`, "", 200},
		{"systemone", "/v1/systemone", systemOne, key, `"questions":{`, `"box":{`, "", 200},
		{"systemone alias", "/systemone", systemOne, key, `"password":"[REDACTED]"`, `"confidence":0.9`, "", 200},
		{"large input", "/v1/decisions", strings.Replace(decisionBody("luna"), "ticket", strings.Repeat("a", 4096), 1), key, `"questions":[`, `"probability":0.9`, "llmproxy.capture.input_truncated", 200},
		{"master key", "/v1/decisions", decisionBody("luna"), "master-only", "", "", "", 200},
		{"upstream error", "/v1/systemone", strings.Replace(systemOne, `"jev"`, `"error422"`, 1), key, `"questions":{`, "", "", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(recorder.Ended())
			if status, _, body := f.request(t, "POST", tc.path, tc.body, tc.token, false); status != tc.status {
				t.Fatalf("HTTP %d: %s", status, body)
			}
			spans := recorder.Ended()[before:]
			if len(spans) != 2 || !strings.HasPrefix(spans[0].Name(), "decision ") {
				t.Fatalf("expected a decision span and its HTTP parent, got %d spans", len(spans))
			}
			for i, span := range spans {
				attrs := make(map[string]string)
				for _, a := range span.Attributes() {
					attrs[string(a.Key)] = a.Value.Emit()
				}
				if i == 0 {
					endpoint := "/v1/" + strings.TrimPrefix(strings.TrimPrefix(tc.path, "/"), "v1/")
					if attrs["llmproxy.decision.endpoint"] != endpoint || (tc.flag != "" && attrs[tc.flag] != "true") {
						t.Fatalf("unexpected metadata: %v", attrs)
					}
				}
				for direction, want := range map[string]string{"input": tc.input, "output": tc.output} {
					got := attrs["langfuse.observation."+direction]
					if want == "" && got != "" || want != "" && !strings.Contains(got, want) || len(got) > 1024 ||
						strings.Contains(got, "secret-value") || strings.Contains(got, "inline-payload") {
						t.Errorf("%s %s = %q, want %q", span.Name(), direction, got, want)
					}
				}
			}
		})
	}

	before := len(recorder.Ended())
	chat := `{"model":"chat","messages":[{"role":"user","content":"chat payload"}]}`
	if status, _, body := f.request(t, "POST", "/v1/chat/completions", chat, key, false); status != http.StatusOK {
		t.Fatalf("HTTP %d: %s", status, body)
	}
	spans := recorder.Ended()[before:]
	if len(spans) != 2 {
		t.Fatalf("expected a chat span and its HTTP parent, got %d spans", len(spans))
	}
	for _, span := range spans {
		for _, attr := range span.Attributes() {
			switch attr.Key {
			case "gen_ai.input.messages", "gen_ai.output.messages", "langfuse.observation.input", "langfuse.observation.output":
				t.Errorf("decision-only allowlist unexpectedly captured %s on %s", attr.Key, span.Name())
			}
		}
	}
}

// Without Langfuse compatibility the decision payload stays on the model span
// only; it is not copied onto the HTTP span as trace-level input/output.
func TestDecisionTraceCaptureWithoutLangfuseCompat(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	f := newDecisionFixtureConfigured(t, provider.Tracer("test"), func(cfg *config.Config, appID int64) {
		cfg.OTelSettings.MessageCapture.DecisionApplicationIDs = []int64{appID}
		cfg.OTelSettings.MessageCapture.MaxBytes = 1024
	})
	key, _ := f.key(t, "capture", true, nil, nil, nil)
	if status, _, body := f.request(t, "POST", "/v1/decisions", decisionBody("luna"), key, false); status != http.StatusOK {
		t.Fatalf("HTTP %d: %s", status, body)
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected a decision span and its HTTP parent, got %d spans", len(spans))
	}
	model, root := map[string]string{}, map[string]string{}
	for _, a := range spans[0].Attributes() {
		model[string(a.Key)] = a.Value.Emit()
	}
	for _, a := range spans[1].Attributes() {
		root[string(a.Key)] = a.Value.Emit()
	}
	if !strings.Contains(model["langfuse.observation.input"], `"questions":[`) || !strings.Contains(model["langfuse.observation.output"], `"probability":0.9`) {
		t.Fatalf("decision span lost its payload: %v", model)
	}
	for key := range root {
		if strings.HasPrefix(key, "langfuse.") {
			t.Errorf("HTTP span got Langfuse-only attribute %s", key)
		}
	}
}

// Only model calls reach the trace backend; housekeeping routes stay out.
func TestOnlyModelRoutesAreTraced(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(t.Context())
	f := newDecisionFixtureWithTracer(t, provider.Tracer("test"))

	for _, path := range []string{"/health", "/v1/models", "/v1/models/luna"} {
		if status, _, body := f.request(t, "GET", path, "", "master-only", false); status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, status, body)
		}
	}
	if spans := recorder.Ended(); len(spans) != 0 {
		names := make([]string, 0, len(spans))
		for _, span := range spans {
			names = append(names, span.Name())
		}
		t.Fatalf("housekeeping routes produced spans: %v", names)
	}

	req, err := http.NewRequest("POST", f.url+"/v1/decisions", bytes.NewBufferString(decisionBody("luna")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer master-only")
	req.Header.Set("X-User-ID", "owui-user-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("decision status %d", resp.StatusCode)
	}

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want HTTP and model spans", len(spans))
	}
	for _, span := range spans {
		attrs := map[string]any{}
		for _, a := range span.Attributes() {
			attrs[string(a.Key)] = a.Value.AsInterface()
		}
		if attrs[proxyotel.UserID] != "owui-user-1" || attrs["langfuse.user.id"] != nil {
			t.Fatalf("span %q missing user attributes: %+v", span.Name(), attrs)
		}
		for name := range attrs {
			if strings.HasPrefix(name, "langfuse.trace.metadata.") {
				t.Fatalf("master-key request unexpectedly has application key metadata %q", name)
			}
		}
	}
}

func TestModelTracesDistinguishSameNamedApplicationsAndKeys(t *testing.T) {
	for _, langfuse := range []bool{false, true} {
		t.Run("langfuse_compat="+strconv.FormatBool(langfuse), func(t *testing.T) {
			testModelTracesDistinguishSameNamedApplicationsAndKeys(t, langfuse)
		})
	}
}

func testModelTracesDistinguishSameNamedApplicationsAndKeys(t *testing.T, langfuse bool) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(t.Context())
	f := newDecisionFixtureConfigured(t, provider.Tracer("test"), func(cfg *config.Config, _ int64) {
		cfg.OTelSettings.LangfuseCompat = langfuse
	})

	createApp := func(teamName string) (teamID, appID int64) {
		t.Helper()
		team, err := f.store.CreateTeam(t.Context(), teamName)
		if err != nil {
			t.Fatal(err)
		}
		app, err := f.store.CreateApplication(t.Context(), team.ID, "canyon")
		if err != nil {
			t.Fatal(err)
		}
		return team.ID, app.ID
	}
	playTeam, playApp := createApp("play")
	testTeam, testApp := createApp("test")
	var keyIDs []int64
	for _, tc := range []struct {
		team          string
		teamID, appID int64
		token         string
	}{
		{"play", playTeam, playApp, "sk-app-play-first"},
		{"play", playTeam, playApp, "sk-app-play-second"},
		{"test", testTeam, testApp, "sk-app-test-first"},
	} {
		digest := sha256.Sum256([]byte(tc.token))
		hash := hex.EncodeToString(digest[:])
		const prefix = "key-prefix-sentinel"
		enabled := true
		key, err := f.store.CreateAPIKey(t.Context(), tc.appID, "production", prefix, hash, nil, nil, nil, nil, nil, storage.APIKeyOptions{AllowDecisions: &enabled})
		if err != nil {
			t.Fatal(err)
		}
		keyIDs = append(keyIDs, key.ID)
		before := len(recorder.Ended())
		status, _, body := f.request(t, "POST", "/v1/decisions", decisionBody("luna"), tc.token, false)
		if status != http.StatusOK {
			t.Fatalf("HTTP %d: %s", status, body)
		}
		spans := recorder.Ended()[before:]
		if len(spans) != 2 {
			t.Fatalf("got %d spans, want HTTP and model spans", len(spans))
		}
		for _, span := range spans {
			attrs := make(map[string]string)
			for _, a := range span.Attributes() {
				value := a.Value.Emit()
				attrs[string(a.Key)] = value
				if strings.Contains(value, tc.token) || strings.Contains(value, hash) || strings.Contains(value, prefix) {
					t.Fatalf("span %q leaked credential material in %s", span.Name(), a.Key)
				}
			}
			for name, want := range map[string]string{
				"team_name":        tc.team,
				"team_id":          strconv.FormatInt(tc.teamID, 10),
				"application_name": "canyon",
				"application_id":   strconv.FormatInt(tc.appID, 10),
				"api_key_name":     "production",
				"api_key_id":       strconv.FormatInt(key.ID, 10),
			} {
				proxyKey := map[string]string{"team_name": "llmproxy.team", "application_name": "llmproxy.application"}[name]
				if proxyKey == "" {
					proxyKey = "llmproxy." + name
				}
				if got := attrs[proxyKey]; got != want {
					t.Errorf("span %q %s = %q, want %q", span.Name(), proxyKey, got, want)
				}
				metadata, want := attrs["langfuse.trace.metadata."+name], map[bool]string{true: want}[langfuse]
				if metadata != want {
					t.Errorf("span %q Langfuse metadata %s = %q, want %q", span.Name(), name, metadata, want)
				}
			}
		}
	}
	if keyIDs[0] == keyIDs[1] {
		t.Fatal("same-named keys in one application share an ID")
	}
}
