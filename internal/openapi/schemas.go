package openapi

import (
	"github.com/getkin/kin-openapi/openapi3"
)

// buildSchemas returns all schema definitions for the OpenAPI spec.
func buildSchemas() openapi3.Schemas {
	return openapi3.Schemas{
		// Request types
		"ChatCompletionRequest":  chatCompletionRequestSchema(),
		"EmbeddingsRequest":      embeddingsRequestSchema(),
		"Message":                messageSchema(),
		"ContentPart":            contentPartSchema(),
		"ImageURL":               imageURLSchema(),
		"Tool":                   toolSchema(),
		"FunctionDef":            functionDefSchema(),
		"ResponseFormat":         responseFormatSchema(),

		// Response types
		"ChatCompletionResponse": chatCompletionResponseSchema(),
		"EmbeddingsResponse":     embeddingsResponseSchema(),
		"ModelsResponse":         modelsResponseSchema(),
		"HealthResponse":         healthResponseSchema(),
		"StreamChunk":            streamChunkSchema(),
		"Choice":                 choiceSchema(),
		"Delta":                  deltaSchema(),
		"Usage":                  usageSchema(),
		"EmbeddingData":          embeddingDataSchema(),
		"ModelInfo":              modelInfoSchema(),
		"ToolCall":               toolCallSchema(),
		"FunctionCall":           functionCallSchema(),

		// Error types
		"APIError":               apiErrorSchema(),
		"ErrorDetail":            errorDetailSchema(),

		// Admin types
		"AdminStatusResponse": adminStatusResponseSchema(),
		"AdminConfigResponse": adminConfigResponseSchema(),
		"AdminLogsResponse":   adminLogsResponseSchema(),
		"ReloadResponse":      reloadResponseSchema(),
		"RouterSettings":      routerSettingsSchema(),
		"ModelStatusInfo":     modelStatusInfoSchema(),
		"DeploymentInfo":      deploymentInfoSchema(),
		"ConfigModel":         configModelSchema(),
		"LogEntry":            logEntrySchema(),

		// Cost map types
		"CostMapStatusResponse": costMapStatusResponseSchema(),
		"CostMapReloadResponse": costMapReloadResponseSchema(),
		"CostMapURLRequest":     costMapURLRequestSchema(),
		"CostMapURLResponse":    costMapURLResponseSchema(),
	}
}

func chatCompletionRequestSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"model", "messages"},
			Properties: openapi3.Schemas{
				"model": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "ID of the model to use",
					},
				},
				"messages": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "A list of messages comprising the conversation",
						Items:       &openapi3.SchemaRef{Ref: "#/components/schemas/Message"},
					},
				},
				"temperature": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"number"},
						Description: "Sampling temperature between 0 and 2",
						Min:         ptr(0.0),
						Max:         ptr(2.0),
					},
				},
				"top_p": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"number"},
						Description: "Nucleus sampling parameter",
						Min:         ptr(0.0),
						Max:         ptr(1.0),
					},
				},
				"n": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "Number of completions to generate",
						Min:         ptr(1.0),
					},
				},
				"stream": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"boolean"},
						Description: "Whether to stream partial progress",
						Default:     false,
					},
				},
				"stop": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "Sequences where the API will stop generating",
						Items: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
						},
					},
				},
				"max_tokens": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "Maximum number of tokens to generate",
					},
				},
				"presence_penalty": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"number"},
						Description: "Penalty for new tokens based on presence in text so far",
						Min:         ptr(-2.0),
						Max:         ptr(2.0),
					},
				},
				"frequency_penalty": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"number"},
						Description: "Penalty for new tokens based on frequency in text so far",
						Min:         ptr(-2.0),
						Max:         ptr(2.0),
					},
				},
				"user": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "A unique identifier representing the end-user",
					},
				},
				"tools": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "A list of tools the model may call",
						Items:       &openapi3.SchemaRef{Ref: "#/components/schemas/Tool"},
					},
				},
				"tool_choice": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Description: "Controls which tool is called by the model",
					},
				},
				"response_format": &openapi3.SchemaRef{
					Ref: "#/components/schemas/ResponseFormat",
				},
			},
		},
	}
}

func embeddingsRequestSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"model", "input"},
			Properties: openapi3.Schemas{
				"model": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "ID of the model to use",
					},
				},
				"input": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Description: "Input text to embed, can be a string or array of strings",
					},
				},
				"user": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "A unique identifier representing the end-user",
					},
				},
				"encoding_format": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The format to return the embeddings in",
						Enum:        []any{"float", "base64"},
					},
				},
			},
		},
	}
}

func messageSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"role"},
			Properties: openapi3.Schemas{
				"role": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The role of the message author",
						Enum:        []any{"system", "user", "assistant", "tool"},
					},
				},
				"content": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Description: "The content of the message (string or array of content parts)",
					},
				},
				"name": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "Optional name of the author",
					},
				},
				"tool_calls": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "Tool calls generated by the model",
						Items:       &openapi3.SchemaRef{Ref: "#/components/schemas/ToolCall"},
					},
				},
				"tool_call_id": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The ID of the tool call this message is responding to",
					},
				},
			},
		},
	}
}

func contentPartSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"type"},
			Properties: openapi3.Schemas{
				"type": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The type of content part",
						Enum:        []any{"text", "image_url"},
					},
				},
				"text": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The text content",
					},
				},
				"image_url": &openapi3.SchemaRef{
					Ref: "#/components/schemas/ImageURL",
				},
			},
		},
	}
}

func imageURLSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"url"},
			Properties: openapi3.Schemas{
				"url": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The URL of the image",
					},
				},
				"detail": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The detail level of the image",
						Enum:        []any{"auto", "low", "high"},
					},
				},
			},
		},
	}
}

func toolSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"type", "function"},
			Properties: openapi3.Schemas{
				"type": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The type of tool",
						Enum:        []any{"function"},
					},
				},
				"function": &openapi3.SchemaRef{
					Ref: "#/components/schemas/FunctionDef",
				},
			},
		},
	}
}

func functionDefSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"name"},
			Properties: openapi3.Schemas{
				"name": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The name of the function",
					},
				},
				"description": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "A description of the function",
					},
				},
				"parameters": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"object"},
						Description: "The parameters the function accepts (JSON Schema)",
					},
				},
			},
		},
	}
}

func responseFormatSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"type": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The response format type",
						Enum:        []any{"text", "json_object"},
					},
				},
			},
		},
	}
}

func chatCompletionResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"id", "object", "created", "model", "choices"},
			Properties: openapi3.Schemas{
				"id": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "A unique identifier for the completion",
					},
				},
				"object": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The object type",
						Enum:        []any{"chat.completion"},
					},
				},
				"created": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "Unix timestamp of when the completion was created",
					},
				},
				"model": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The model used for the completion",
					},
				},
				"choices": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "A list of completion choices",
						Items:       &openapi3.SchemaRef{Ref: "#/components/schemas/Choice"},
					},
				},
				"usage": &openapi3.SchemaRef{
					Ref: "#/components/schemas/Usage",
				},
				"system_fingerprint": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "A fingerprint representing the backend configuration",
					},
				},
			},
		},
	}
}

func embeddingsResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"object", "data", "model"},
			Properties: openapi3.Schemas{
				"object": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The object type",
						Enum:        []any{"list"},
					},
				},
				"data": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "A list of embedding objects",
						Items:       &openapi3.SchemaRef{Ref: "#/components/schemas/EmbeddingData"},
					},
				},
				"model": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The model used for the embeddings",
					},
				},
				"usage": &openapi3.SchemaRef{
					Ref: "#/components/schemas/Usage",
				},
			},
		},
	}
}

func modelsResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"object", "data"},
			Properties: openapi3.Schemas{
				"object": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The object type",
						Enum:        []any{"list"},
					},
				},
				"data": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "A list of model objects",
						Items:       &openapi3.SchemaRef{Ref: "#/components/schemas/ModelInfo"},
					},
				},
			},
		},
	}
}

func healthResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"status"},
			Properties: openapi3.Schemas{
				"status": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The health status",
						Enum:        []any{"healthy"},
					},
				},
			},
		},
	}
}

func streamChunkSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:        &openapi3.Types{"object"},
			Description: "A streaming response chunk sent as Server-Sent Events (SSE)",
			Required:    []string{"id", "object", "created", "model", "choices"},
			Properties: openapi3.Schemas{
				"id": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "A unique identifier for the completion",
					},
				},
				"object": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The object type",
						Enum:        []any{"chat.completion.chunk"},
					},
				},
				"created": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "Unix timestamp of when the chunk was created",
					},
				},
				"model": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The model used for the completion",
					},
				},
				"choices": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "A list of completion choices (contains delta instead of message)",
						Items:       &openapi3.SchemaRef{Ref: "#/components/schemas/Choice"},
					},
				},
				"system_fingerprint": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "A fingerprint representing the backend configuration",
					},
				},
			},
		},
	}
}

func choiceSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"index"},
			Properties: openapi3.Schemas{
				"index": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "The index of this choice",
					},
				},
				"message": &openapi3.SchemaRef{
					Ref: "#/components/schemas/Message",
				},
				"delta": &openapi3.SchemaRef{
					Ref: "#/components/schemas/Delta",
				},
				"finish_reason": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The reason the model stopped generating",
						Enum:        []any{"stop", "length", "tool_calls", "content_filter"},
					},
				},
			},
		},
	}
}

func deltaSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:        &openapi3.Types{"object"},
			Description: "A streaming delta containing partial content",
			Properties: openapi3.Schemas{
				"role": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The role of the author (sent in first chunk)",
					},
				},
				"content": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The partial content",
					},
				},
				"tool_calls": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "Partial tool calls",
						Items:       &openapi3.SchemaRef{Ref: "#/components/schemas/ToolCall"},
					},
				},
			},
		},
	}
}

func usageSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"prompt_tokens", "completion_tokens", "total_tokens"},
			Properties: openapi3.Schemas{
				"prompt_tokens": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "Number of tokens in the prompt",
					},
				},
				"completion_tokens": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "Number of tokens in the completion",
					},
				},
				"total_tokens": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "Total number of tokens used",
					},
				},
			},
		},
	}
}

func embeddingDataSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"object", "embedding", "index"},
			Properties: openapi3.Schemas{
				"object": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The object type",
						Enum:        []any{"embedding"},
					},
				},
				"embedding": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"array"},
						Description: "The embedding vector",
						Items: &openapi3.SchemaRef{
							Value: &openapi3.Schema{Type: &openapi3.Types{"number"}},
						},
					},
				},
				"index": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "The index of this embedding",
					},
				},
			},
		},
	}
}

func modelInfoSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"id", "object", "created", "owned_by"},
			Properties: openapi3.Schemas{
				"id": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The model identifier",
					},
				},
				"object": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The object type",
						Enum:        []any{"model"},
					},
				},
				"created": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"integer"},
						Description: "Unix timestamp of when the model was created",
					},
				},
				"owned_by": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The organization that owns the model",
					},
				},
			},
		},
	}
}

func toolCallSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"id", "type", "function"},
			Properties: openapi3.Schemas{
				"id": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The ID of the tool call",
					},
				},
				"type": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The type of tool call",
						Enum:        []any{"function"},
					},
				},
				"function": &openapi3.SchemaRef{
					Ref: "#/components/schemas/FunctionCall",
				},
			},
		},
	}
}

func functionCallSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"name", "arguments"},
			Properties: openapi3.Schemas{
				"name": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The name of the function to call",
					},
				},
				"arguments": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The arguments to pass to the function as JSON",
					},
				},
			},
		},
	}
}

func apiErrorSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"error"},
			Properties: openapi3.Schemas{
				"error": errorDetailSchema(),
			},
		},
	}
}

func errorDetailSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"message", "type"},
			Properties: openapi3.Schemas{
				"message": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "A human-readable error message",
					},
				},
				"type": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The type of error",
						Enum:        []any{"invalid_request_error", "authentication_error", "server_error"},
					},
				},
				"param": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "The parameter that caused the error",
					},
				},
				"code": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"string"},
						Description: "An error code for programmatic handling",
					},
				},
			},
		},
	}
}

func ptr(f float64) *float64 {
	return &f
}

func routerSettingsSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"routing_strategy": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Load balancing strategy"}},
				"num_retries":      {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}, Description: "Number of retries on failure"}},
				"allowed_fails":    {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}, Description: "Failures before cooldown"}},
				"cooldown_time":    {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Cooldown duration (e.g. 30s)"}},
			},
		},
	}
}

func deploymentInfoSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"provider":       {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"actual_model":   {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"api_base":       {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"status":         {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Enum: []any{"healthy", "cooldown"}}},
				"failure_count":  {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"cooldown_until": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Format: "date-time"}},
				"rpm":            {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"tpm":            {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
			},
		},
	}
}

func modelStatusInfoSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"model_name":           {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"total_deployments":    {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"healthy_deployments":  {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"deployments": {Value: &openapi3.Schema{
					Type:  &openapi3.Types{"array"},
					Items: &openapi3.SchemaRef{Ref: "#/components/schemas/DeploymentInfo"},
				}},
			},
		},
	}
}

func adminStatusResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"status", "uptime_seconds", "models", "router_settings"},
			Properties: openapi3.Schemas{
				"status":          {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Enum: []any{"healthy"}}},
				"uptime_seconds":  {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"models":          {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/ModelStatusInfo"}}},
				"router_settings": {Ref: "#/components/schemas/RouterSettings"},
			},
		},
	}
}

func configModelSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"model_name":   {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"provider":     {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"actual_model": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"api_key_set":  {Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}}},
				"api_base":     {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"rpm":          {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"tpm":          {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
			},
		},
	}
}

func adminConfigResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"model_list": {Value: &openapi3.Schema{
					Type:  &openapi3.Types{"array"},
					Items: &openapi3.SchemaRef{Ref: "#/components/schemas/ConfigModel"},
				}},
				"router_settings": {Ref: "#/components/schemas/RouterSettings"},
				"general_settings": {Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"master_key_set": {Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}}},
						"database_url":   {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
						"port":           {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
					},
				}},
			},
		},
	}
}

func logEntrySchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"request_id":         {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"model":              {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"provider":           {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"endpoint":           {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				"prompt_tokens":      {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"completion_tokens":  {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"total_tokens":       {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"total_cost":         {Value: &openapi3.Schema{Type: &openapi3.Types{"number"}}},
				"status_code":        {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"latency_ms":         {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"request_time":       {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Format: "date-time"}},
			},
		},
	}
}

func adminLogsResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"logs", "total", "limit", "offset"},
			Properties: openapi3.Schemas{
				"logs":   {Value: &openapi3.Schema{Type: &openapi3.Types{"array"}, Items: &openapi3.SchemaRef{Ref: "#/components/schemas/LogEntry"}}},
				"total":  {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"limit":  {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
				"offset": {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}}},
			},
		},
	}
}

func reloadResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"status"},
			Properties: openapi3.Schemas{
				"status": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Enum: []any{"ok"}}},
			},
		},
	}
}

func costMapStatusResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:        &openapi3.Types{"object"},
			Description: "Current status of the LiteLLM cost map cache",
			Properties: openapi3.Schemas{
				"loaded":      {Value: &openapi3.Schema{Type: &openapi3.Types{"boolean"}, Description: "Whether the cost map has been successfully loaded"}},
				"loaded_at":   {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Format: "date-time", Description: "Timestamp of the last successful load (omitted if never loaded)"}},
				"url":         {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "Current source URL for the cost map"}},
				"model_count": {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}, Description: "Number of models in the cost map"}},
			},
		},
	}
}

func costMapReloadResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"status", "model_count"},
			Properties: openapi3.Schemas{
				"status":      {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Enum: []any{"ok"}}},
				"model_count": {Value: &openapi3.Schema{Type: &openapi3.Types{"integer"}, Description: "Number of models loaded from the cost map"}},
			},
		},
	}
}

func costMapURLRequestSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"url"},
			Properties: openapi3.Schemas{
				"url": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "New source URL (must be http or https)"}},
			},
		},
	}
}

func costMapURLResponseSchema() *openapi3.SchemaRef {
	return &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"object"},
			Required: []string{"url"},
			Properties: openapi3.Schemas{
				"url": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Description: "The updated source URL"}},
			},
		},
	}
}

