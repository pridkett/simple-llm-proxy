package provider

import (
	"errors"
	"fmt"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpstreamErrorsReachClient(t *testing.T) {
	for _, status := range []int{400, 401, 422, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := []byte(`{"error":{"message":"upstream rejection","type":"provider_error","code":123}}`)
			err := NewHTTPError("test", status, http.Header{"Retry-After": []string{"7"}}, body)
			w := httptest.NewRecorder()
			model.WriteError(w, model.ErrProviderError("test", fmt.Errorf("route: %w", err)))
			if w.Code != status || w.Header().Get("Retry-After") != "7" || w.Body.String() != string(body) {
				t.Fatalf("lost error metadata: %d %s %s", w.Code, w.Header(), w.Body.String())
			}
			if IsInvalidRequest(err) != (status == 400 || status == 422) {
				t.Fatal("invalid retry classification")
			}
			if status == 429 {
				var rate *RateLimitError
				if !errors.As(err, &rate) || rate.RetryAfter != 7*time.Second {
					t.Fatal("rate limit classification lost")
				}
			}
		})
	}
	err := NewHTTPError("test", 502, nil, []byte("bad gateway"), errors.New("custom parser"))
	if err.Error() != "custom parser" {
		t.Fatal(err)
	}
	fallback := model.ErrProviderError("test", err)
	w := httptest.NewRecorder()
	model.WriteError(w, fallback)
	if w.Code != 502 || w.Body.String() == "bad gateway" {
		t.Fatal("non-JSON error was not normalized")
	}
	if IsInvalidRequest(errors.New("transport")) {
		t.Fatal("transport error classified as input")
	}
	if NewHTTPError("test", 500, nil, []byte(`{"error":{"message":"broken"}}`)).Error() != "test error: broken" {
		t.Fatal("message parsing failed")
	}
	if NewHTTPError("test", 500, nil, []byte("oops")).Error() != "test error (status 500): oops" {
		t.Fatal("raw message failed")
	}
}
