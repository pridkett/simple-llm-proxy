package handler

import (
	"net/http"

	"github.com/pwagstro/simple_llm_proxy/internal/openapi"
)

// OpenAPI returns a handler that serves the OpenAPI specification.
func OpenAPI(spec *openapi.Spec) http.HandlerFunc {
	return spec.Handler()
}
