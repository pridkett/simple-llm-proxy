package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
)

// Spec holds the cached OpenAPI specification.
type Spec struct {
	doc  *openapi3.T
	json []byte
}

// New creates a new Spec instance.
func New() *Spec {
	return &Spec{}
}

// Build constructs the OpenAPI specification and caches the JSON bytes.
// Returns an error if the spec fails validation.
func (s *Spec) Build() error {
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:       "Simple LLM Proxy",
			Description: "A lightweight LLM proxy server providing OpenAI-compatible endpoints with multi-provider support (OpenAI, Anthropic).",
			Version:     "1.0.0",
			License: &openapi3.License{
				Name: "MIT",
			},
		},
		Servers: openapi3.Servers{
			&openapi3.Server{
				URL:         "/",
				Description: "Current server",
			},
		},
		Paths: buildPaths(),
		Components: &openapi3.Components{
			Schemas:         buildSchemas(),
			SecuritySchemes: buildSecuritySchemes(),
		},
		Tags: openapi3.Tags{
			&openapi3.Tag{
				Name:        "Health",
				Description: "Health check endpoints",
			},
			&openapi3.Tag{
				Name:        "Chat",
				Description: "Chat completion endpoints",
			},
			&openapi3.Tag{
				Name:        "Embeddings",
				Description: "Embedding endpoints",
			},
			&openapi3.Tag{
				Name:        "Models",
				Description: "Model management endpoints",
			},
			&openapi3.Tag{
				Name:        "Completions",
				Description: "Legacy completion endpoints (deprecated)",
			},
			&openapi3.Tag{
				Name:        "Admin",
				Description: "Admin endpoints for proxy management (require authentication)",
			},
		},
	}

	// Marshal to JSON first
	jsonBytes, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal openapi spec: %w", err)
	}

	// Load the spec back using the loader to resolve refs
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	loadedDoc, err := loader.LoadFromData(jsonBytes)
	if err != nil {
		return fmt.Errorf("failed to load openapi spec: %w", err)
	}

	// Validate the loaded spec (refs are now resolved)
	if err := loadedDoc.Validate(context.Background()); err != nil {
		return fmt.Errorf("openapi spec validation failed: %w", err)
	}

	s.doc = loadedDoc
	s.json = jsonBytes
	return nil
}

// Doc returns the OpenAPI document. Returns nil if Build() hasn't been called.
func (s *Spec) Doc() *openapi3.T {
	return s.doc
}

// JSON returns the cached JSON bytes. Returns nil if Build() hasn't been called.
func (s *Spec) JSON() []byte {
	return s.json
}

// Handler returns an http.HandlerFunc that serves the OpenAPI spec as JSON.
func (s *Spec) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(s.json)
	}
}
