package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
)

func TestChatCompletionsAnthropicPromptCaching(t *testing.T) {
	cachingConfig, err := config.Parse([]byte(`
model_list:
  - model_name: anthropic-cached
    litellm_params:
      model: anthropic/wire-anthropic
      api_key: upstream-only
      extra_params:
        anthropic_prompt_caching: true
`))
	if err != nil {
		t.Fatal(err)
	}
	f := newConfiguredMigrationFixture(t, nil, func(cfg *config.Config) {
		for _, deployment := range cfg.ModelList {
			if deployment.ModelName == "anthropic" {
				cached := cachingConfig.ModelList[0]
				cached.LiteLLMParams.APIBase = deployment.LiteLLMParams.APIBase
				cfg.ModelList = append(cfg.ModelList, cached)
				return
			}
		}
		t.Fatal("missing Anthropic fixture deployment")
	})

	for _, stream := range []bool{false, true} {
		for _, alias := range []string{"anthropic", "anthropic-cached"} {
			t.Run(fmt.Sprintf("%s/stream=%t", alias, stream), func(t *testing.T) {
				body := fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":"hello"}]}`, alias, stream)
				status, _, response := f.request(t, http.MethodPost, "/v1/chat/completions", body, "master-only", false)
				if status != http.StatusOK {
					t.Fatalf("chat returned %d: %s", status, response)
				}
				value, present := (<-f.captured)["cache_control"]
				if alias == "anthropic" {
					if present {
						t.Fatalf("cache_control should be omitted by default, got %v", value)
					}
					return
				}
				control, _ := value.(map[string]any)
				if len(control) != 1 || control["type"] != "ephemeral" {
					t.Fatalf("expected an ephemeral cache_control directive, got %v", value)
				}
			})
		}
	}
}
