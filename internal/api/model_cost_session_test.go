package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

func TestModelCostWritesWithSession(t *testing.T) {
	for _, name := range []string{"openai", "openai/gpt-5.4-mini", "vendor/model%2Fv1 + preview", "literal%2Fv1", "alias % preview"} {
		t.Run(name, func(t *testing.T) {
			testModelCostWritesWithSession(t, name)
		})
	}
}

func testModelCostWritesWithSession(t *testing.T, modelName string) {
	t.Helper()
	cm := costmap.New()
	costServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"wire-openai":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002},"openai/other":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000004}}`))
	}))
	t.Cleanup(costServer.Close)
	if err := cm.SetURL(costServer.URL); err != nil {
		t.Fatal(err)
	}
	if err := cm.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := newConfiguredMigrationFixture(t, cm, func(cfg *config.Config) {
		cfg.ModelList[0].ModelName = modelName
	})
	for _, prefix := range []string{"/admin", "/v1"} {
		t.Run(prefix, func(t *testing.T) {
			// Browser writes use only the session; machine writes still use an API key.
			key, session := "", true
			if prefix == "/v1" {
				key, session = "master-only", false
			}
			for _, op := range []struct {
				method, suffix, body, source, totalCost string
				inputCost                               float64
			}{
				{"PATCH", "/cost_map_key", `{"cost_map_key":"openai/other"}`, "override", "0.000032", 0.000002},
				{"PATCH", "/costs", `{"input_cost_per_token":0.000003,"output_cost_per_token":0.000006}`, "custom", "0.000048", 0.000003},
				// Switching back from custom costs must immediately use the selected mapping.
				{"PATCH", "/cost_map_key", `{"cost_map_key":"openai/other"}`, "override", "0.000032", 0.000002},
				{"DELETE", "/costs", "", "auto", "0.000016", 0.000001},
			} {
				path := prefix + "/models/" + url.PathEscape(modelName) + op.suffix
				status, _, body := f.request(t, op.method, path, op.body, "", false)
				if status != http.StatusUnauthorized {
					t.Fatalf("unauthenticated %s %s: %d %s", op.method, path, status, body)
				}
				status, _, body = f.request(t, op.method, path, op.body, key, session)
				if status != http.StatusOK {
					t.Fatalf("%s %s: %d %s", op.method, path, status, body)
				}
				// A subsequent read must reflect the saved value, with the session still valid.
				status, _, body = f.request(t, "GET", "/admin/models/"+url.PathEscape(modelName), "", "", true)
				var detail model.ModelDetailResponse
				if err := json.Unmarshal(body, &detail); err != nil {
					t.Fatal(err)
				}
				if status != http.StatusOK || detail.Costs.Source != op.source || detail.Costs.InputCostPerToken != op.inputCost {
					t.Fatalf("saved costs: %d %s", status, body)
				}
				// The proxy must price a subsequent inference using the saved rates.
				status, headers, body := f.request(t, "POST", "/v1/chat/completions", fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, modelName), "master-only", false)
				if status != http.StatusOK || headers.Get("X-LLM-Cost-USD") != op.totalCost {
					t.Fatalf("inference costs after %s: status=%d headers=%v body=%s", op.source, status, headers, body)
				}
				record := getAccounting(t, f, headers.Get("X-LLM-Accounting-ID"), "master-only")
				if len(record.Attempts) != 1 || record.Attempts[0].Pricing == nil || record.Attempts[0].Pricing.Source != op.source {
					t.Fatalf("inference pricing: %+v", record)
				}
				override, err := f.store.GetCostOverride(context.Background(), modelName)
				if err != nil {
					t.Fatal(err)
				}
				switch op.source {
				case "override":
					if override == nil || override.CostMapKey == nil || *override.CostMapKey != "openai/other" {
						t.Fatalf("mapping not persisted: %+v", override)
					}
				case "custom":
					var spec costmap.ModelSpec
					if override == nil || override.CustomSpec == nil {
						t.Fatalf("custom costs not persisted: %+v", override)
					}
					if err := json.Unmarshal([]byte(*override.CustomSpec), &spec); err != nil {
						t.Fatal(err)
					}
					if spec.InputCostPerToken != op.inputCost {
						t.Fatalf("persisted custom costs: %+v", spec)
					}
				case "auto":
					if override != nil {
						t.Fatalf("override not deleted: %+v", override)
					}
				}
			}
		})
	}
}
