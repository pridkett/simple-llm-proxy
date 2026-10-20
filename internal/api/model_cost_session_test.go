package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

func TestModelCostWritesWithSession(t *testing.T) {
	cm := costmap.New()
	costServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"openai/wire-openai":{"input_cost_per_token":0.000001},"openai/other":{"input_cost_per_token":0.000002}}`))
	}))
	t.Cleanup(costServer.Close)
	if err := cm.SetURL(costServer.URL); err != nil {
		t.Fatal(err)
	}
	if err := cm.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := newMigrationFixtureWithCostMap(t, cm)
	for _, prefix := range []string{"/admin", "/v1"} {
		t.Run(prefix, func(t *testing.T) {
			// Browser writes use only the session; machine writes still use an API key.
			key, session := "", true
			if prefix == "/v1" {
				key, session = "master-only", false
			}
			for _, op := range []struct {
				method, suffix, body, source string
				inputCost                    float64
			}{
				{"PATCH", "/cost_map_key", `{"cost_map_key":"openai/other"}`, "override", 0.000002},
				{"PATCH", "/costs", `{"input_cost_per_token":0.000003}`, "custom", 0.000003},
				{"DELETE", "/costs", "", "auto", 0.000001},
			} {
				path := prefix + "/models/openai" + op.suffix
				status, _, body := f.request(t, op.method, path, op.body, "", false)
				if status != http.StatusUnauthorized {
					t.Fatalf("unauthenticated %s %s: %d %s", op.method, path, status, body)
				}
				status, _, body = f.request(t, op.method, path, op.body, key, session)
				if status != http.StatusOK {
					t.Fatalf("%s %s: %d %s", op.method, path, status, body)
				}
				// A subsequent read must reflect the saved value, with the session still valid.
				status, _, body = f.request(t, "GET", "/admin/models/openai", "", "", true)
				var detail model.ModelDetailResponse
				if err := json.Unmarshal(body, &detail); err != nil {
					t.Fatal(err)
				}
				if status != http.StatusOK || detail.Costs.Source != op.source || detail.Costs.InputCostPerToken != op.inputCost {
					t.Fatalf("saved costs: %d %s", status, body)
				}
				override, err := f.store.GetCostOverride(context.Background(), "openai")
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
