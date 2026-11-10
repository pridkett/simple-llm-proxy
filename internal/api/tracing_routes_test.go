package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"

	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

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
		if attrs[proxyotel.UserID] != "owui-user-1" || attrs[proxyotel.LangfuseUserID] != "owui-user-1" {
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
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(t.Context())
	f := newDecisionFixtureWithTracer(t, provider.Tracer("test"))

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
		key, err := f.store.CreateAPIKey(t.Context(), tc.appID, "production", prefix, hash, nil, nil, nil, nil, nil, &enabled)
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
				if got := attrs["langfuse.trace.metadata."+name]; got != want {
					t.Errorf("span %q metadata %s = %q, want %q", span.Name(), name, got, want)
				}
			}
		}
	}
	if keyIDs[0] == keyIDs[1] {
		t.Fatal("same-named keys in one application share an ID")
	}
}
