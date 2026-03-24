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
	paths.Set("/admin/status", adminStatusPath())
	paths.Set("/admin/config", adminConfigPath())
	paths.Set("/admin/logs", adminLogsPath())
	paths.Set("/admin/reload", adminReloadPath())
	paths.Set("/admin/costmap", adminCostMapStatusPath())
	paths.Set("/admin/costmap/reload", adminCostMapReloadPath())
	paths.Set("/admin/costmap/url", adminCostMapSetURLPath())

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

func adminStatusPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Admin"},
			Summary:     "Proxy status",
			Description: "Returns proxy health, uptime, model deployment statuses, and router settings",
			OperationID: "getAdminStatus",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Proxy status"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/AdminStatusResponse",
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

func adminConfigPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Admin"},
			Summary:     "Current config",
			Description: "Returns the current proxy configuration. Secrets (API keys, master key) are redacted.",
			OperationID: "getAdminConfig",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Sanitized proxy configuration"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/AdminConfigResponse",
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

func adminLogsPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Admin"},
			Summary:     "Request logs",
			Description: "Returns paginated request logs from the database",
			OperationID: "getAdminLogs",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			Parameters: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:        "limit",
						In:          "query",
						Description: "Maximum number of log entries to return (1–500, default 50)",
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type:    &openapi3.Types{"integer"},
								Min:     ptr(1),
								Max:     ptr(500),
								Default: 50,
							},
						},
					},
				},
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						Name:        "offset",
						In:          "query",
						Description: "Number of log entries to skip",
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type:    &openapi3.Types{"integer"},
								Min:     ptr(0),
								Default: 0,
							},
						},
					},
				},
			},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Paginated request logs"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/AdminLogsResponse",
								},
							},
						},
					},
				}),
				openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication failed")),
				openapi3.WithStatus(http.StatusInternalServerError, errorResponseRef("Failed to fetch logs")),
			),
		},
	}
}

func adminReloadPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Admin"},
			Summary:     "Reload config",
			Description: "Re-reads the config file from disk and applies changes to model deployments and router settings. Note: changes to master_key, port, and database_url require a server restart.",
			OperationID: "postAdminReload",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Config reloaded successfully"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{
									Ref: "#/components/schemas/ReloadResponse",
								},
							},
						},
					},
				}),
				openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication failed")),
				openapi3.WithStatus(http.StatusInternalServerError, errorResponseRef("Failed to reload config")),
			),
		},
	}
}

func adminCostMapStatusPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Get: &openapi3.Operation{
			Tags:        []string{"Admin"},
			Summary:     "Cost map status",
			Description: "Returns the current status of the LiteLLM cost/context map cache.",
			OperationID: "getAdminCostMap",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Cost map status"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{Ref: "#/components/schemas/CostMapStatusResponse"},
							},
						},
					},
				}),
				openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication failed")),
			),
		},
	}
}

func adminCostMapReloadPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Post: &openapi3.Operation{
			Tags:        []string{"Admin"},
			Summary:     "Reload cost map",
			Description: "Fetches the cost map from the configured source URL and refreshes the local cache.",
			OperationID: "postAdminCostMapReload",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("Cost map reloaded"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{Ref: "#/components/schemas/CostMapReloadResponse"},
							},
						},
					},
				}),
				openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication failed")),
				openapi3.WithStatus(http.StatusInternalServerError, errorResponseRef("Failed to reload cost map")),
			),
		},
	}
}

func adminCostMapSetURLPath() *openapi3.PathItem {
	return &openapi3.PathItem{
		Put: &openapi3.Operation{
			Tags:        []string{"Admin"},
			Summary:     "Update cost map URL",
			Description: "Changes the source URL used for future cost map reloads. URL changes are in-memory and reset to default on server restart.",
			OperationID: "putAdminCostMapURL",
			Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
			RequestBody: &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Required: true,
					Content: openapi3.Content{
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{Ref: "#/components/schemas/CostMapURLRequest"},
						},
					},
				},
			},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: strPtr("URL updated"),
						Content: openapi3.Content{
							"application/json": &openapi3.MediaType{
								Schema: &openapi3.SchemaRef{Ref: "#/components/schemas/CostMapURLResponse"},
							},
						},
					},
				}),
				openapi3.WithStatus(http.StatusBadRequest, errorResponseRef("Invalid URL")),
				openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication failed")),
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
