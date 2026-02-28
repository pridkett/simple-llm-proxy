package openapi

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
)

// buildPaths returns all path definitions for the OpenAPI spec.
func buildPaths() *openapi3.Paths {
	paths := openapi3.NewPaths()

	paths.Set("/health", healthPath())
	paths.Set("/v1/chat/completions", chatCompletionsPath())
	paths.Set("/v1/embeddings", embeddingsPath())
	paths.Set("/v1/models", modelsPath())
	paths.Set("/v1/completions", completionsPath())

	return paths
}

func healthPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Health"},
			Summary:     "Health check",
			Description: "Returns the health status of the proxy server",
			OperationID: "getHealth",
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Healthy"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/HealthResponse",
								},
							},
						},
					},
				}),
			),
		},
	}
}

func chatCompletionsPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Chat"},
			Summary:     "Create chat completion",
			Description: "Creates a model response for the given chat conversation. Supports streaming via Server-Sent Events when stream=true.",
			OperationID: "createChatCompletion",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			RequestBody: &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Required: true,
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Ref: "#/components/schemas/ChatCompletionRequest",
							},
						},
					},
				},
			},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Successful response. When stream=false, returns ChatCompletionResponse. When stream=true, returns Server-Sent Events with StreamChunk objects in format: `data: {json}\\n\\n`, terminated by `data: [DONE]\\n\\n`"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/ChatCompletionResponse",
								},
							},
							"text/event-stream": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/StreamChunk",
								},
							},
						},
					},
				}),
				openapi3.WithStatus(http.StatusBadRequest, errorResponseRef("Invalid request")),
				openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication failed")),
				openapi3.WithStatus(http.StatusNotFound, errorResponseRef("Model not found")),
				openapi3.WithStatus(http.StatusBadGateway, errorResponseRef("Provider error")),
				openapi3.WithStatus(http.StatusServiceUnavailable, errorResponseRef("No healthy deployment available")),
			),
		},
	}
}

func embeddingsPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Embeddings"},
			Summary:     "Create embeddings",
			Description: "Creates an embedding vector representing the input text",
			OperationID: "createEmbeddings",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			RequestBody: &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Required: true,
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Ref: "#/components/schemas/EmbeddingsRequest",
							},
						},
					},
				},
			},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Successful response"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/EmbeddingsResponse",
								},
							},
						},
					},
				}),
				openapi3.WithStatus(http.StatusBadRequest, errorResponseRef("Invalid request")),
				openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication failed")),
				openapi3.WithStatus(http.StatusNotFound, errorResponseRef("Model not found")),
				openapi3.WithStatus(http.StatusBadGateway, errorResponseRef("Provider error")),
				openapi3.WithStatus(http.StatusServiceUnavailable, errorResponseRef("No healthy deployment available")),
			),
		},
	}
}

func modelsPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Models"},
			Summary:     "List models",
			Description: "Lists the currently available models",
			OperationID: "listModels",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Successful response"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/ModelsResponse",
								},
							},
						},
					},
				}),
				openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication failed")),
			),
		},
	}
}

func completionsPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Completions"},
			Summary:     "Create completion (deprecated)",
			Description: "Creates a completion for the provided prompt. This is a legacy endpoint; use /v1/chat/completions instead.",
			OperationID: "createCompletion",
			Deprecated:  true,
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			RequestBody: &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Required: true,
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{
								Ref: "#/components/schemas/CompletionRequest",
							},
						},
					},
				},
			},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Successful response"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/ChatCompletionResponse",
								},
							},
						},
					},
				}),
				openapi3.WithStatus(http.StatusBadRequest, errorResponseRef("Invalid request")),
				openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication failed")),
				openapi3.WithStatus(http.StatusNotImplemented, errorResponseRef("Not implemented")),
			),
		},
	}
}

func errorResponseRef(description string) *openapi3.ResponseRef {
	return &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: strPtr(description),
			Content: openapi3.Content{
				"application/json": &openapi3.MediaType{
					Schema: &openapi3.SchemaRef{
						Ref: "#/components/schemas/APIError",
					},
				},
			},
		},
	}
}

func strPtr(s string) *string {
	return &s
}

const bearerAuthName = "bearerAuth"

// buildSecuritySchemes returns the security scheme definitions.
func buildSecuritySchemes() openapi3.SecuritySchemes {
	return openapi3.SecuritySchemes{
		bearerAuthName: &openapi3.SecuritySchemeRef{
			Value: &openapi3.SecurityScheme{
				Type:        "http",
				Scheme:      "bearer",
				Description: "API key authentication. Pass your API key as a Bearer token in the Authorization header.",
			},
		},
	}
}
