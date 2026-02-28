package openapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestSpec_Build(t *testing.T) {
	spec := New()
	err := spec.Build()
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	if spec.Doc() == nil {
		t.Error("Doc() returned nil after Build()")
	}

	if spec.JSON() == nil {
		t.Error("JSON() returned nil after Build()")
	}
}

func TestSpec_Validates(t *testing.T) {
	spec := New()
	err := spec.Build()
	if err != nil {
		t.Fatalf("Spec validation failed: %v", err)
	}
}

func TestSpec_HasExpectedPaths(t *testing.T) {
	spec := New()
	if err := spec.Build(); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	expectedPaths := []string{
		"/health",
		"/v1/chat/completions",
		"/v1/embeddings",
		"/v1/models",
		"/v1/completions",
	}

	doc := spec.Doc()
	for _, path := range expectedPaths {
		if doc.Paths.Find(path) == nil {
			t.Errorf("Expected path %s not found in spec", path)
		}
	}
}

func TestSpec_HasExpectedSchemas(t *testing.T) {
	spec := New()
	if err := spec.Build(); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	expectedSchemas := []string{
		"ChatCompletionRequest",
		"ChatCompletionResponse",
		"EmbeddingsRequest",
		"EmbeddingsResponse",
		"ModelsResponse",
		"HealthResponse",
		"APIError",
		"Message",
		"Choice",
		"Usage",
		"StreamChunk",
	}

	doc := spec.Doc()
	for _, schema := range expectedSchemas {
		if doc.Components.Schemas[schema] == nil {
			t.Errorf("Expected schema %s not found in spec", schema)
		}
	}
}

func TestSpec_HasSecurityScheme(t *testing.T) {
	spec := New()
	if err := spec.Build(); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	doc := spec.Doc()
	if doc.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Error("Expected bearerAuth security scheme not found")
	}
}

func TestSpec_Handler_ContentType(t *testing.T) {
	spec := New()
	if err := spec.Build(); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()

	handler := spec.Handler()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}
}

func TestSpec_Handler_ValidJSON(t *testing.T) {
	spec := New()
	if err := spec.Build(); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()

	handler := spec.Handler()
	handler(rec, req)

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Errorf("Response is not valid JSON: %v", err)
	}

	if result["openapi"] != "3.0.3" {
		t.Errorf("Expected openapi version 3.0.3, got %v", result["openapi"])
	}
}

func TestSpec_OpenAPIVersion(t *testing.T) {
	spec := New()
	if err := spec.Build(); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	doc := spec.Doc()
	if doc.OpenAPI != "3.0.3" {
		t.Errorf("Expected OpenAPI version 3.0.3, got %s", doc.OpenAPI)
	}
}

func TestSpec_ProtectedRoutesHaveSecurity(t *testing.T) {
	spec := New()
	if err := spec.Build(); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	doc := spec.Doc()
	protectedPaths := map[string]string{
		"/v1/chat/completions": "POST",
		"/v1/embeddings":       "POST",
		"/v1/models":           "GET",
		"/v1/completions":      "POST",
	}

	for path, method := range protectedPaths {
		pathItem := doc.Paths.Find(path)
		if pathItem == nil {
			t.Errorf("Path %s not found", path)
			continue
		}

		var security *openapi3.SecurityRequirements
		switch method {
		case "POST":
			if pathItem.Post != nil {
				security = pathItem.Post.Security
			}
		case "GET":
			if pathItem.Get != nil {
				security = pathItem.Get.Security
			}
		}

		if security == nil || len(*security) == 0 {
			t.Errorf("Path %s has no security requirements", path)
		}
	}
}

func TestSpec_HealthRouteIsPublic(t *testing.T) {
	spec := New()
	if err := spec.Build(); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	doc := spec.Doc()
	pathItem := doc.Paths.Find("/health")
	if pathItem == nil {
		t.Fatal("Path /health not found")
	}

	if pathItem.Get.Security != nil && len(*pathItem.Get.Security) > 0 {
		t.Error("Health endpoint should be public (no security requirements)")
	}
}
