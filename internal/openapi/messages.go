package openapi

import (
	"strconv"

	"github.com/getkin/kin-openapi/openapi3"
)

func messagesPath() *openapi3.PathItem {
	responses := openapi3.NewResponses(openapi3.WithStatus(200, &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: strPtr("Anthropic Message, or named SSE message/content_block events. The model is the requested proxy alias. A stream ends with message_stop on success, or error on failure."),
		Content: openapi3.Content{
			"application/json":  {Schema: &openapi3.SchemaRef{Ref: "#/components/schemas/AnthropicMessage"}},
			"text/event-stream": {Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}},
		},
	}}))
	for _, status := range []int{400, 401, 403, 404, 413, 429, 500, 502, 503} {
		responses.Set(strconv.Itoa(status), &openapi3.ResponseRef{Value: &openapi3.Response{Description: strPtr("Anthropic error envelope; Retry-After is preserved when supplied by the provider."), Content: openapi3.Content{"application/json": {Schema: &openapi3.SchemaRef{Ref: "#/components/schemas/AnthropicError"}}}}})
	}
	return &openapi3.PathItem{Post: &openapi3.Operation{
		Tags: []string{"Chat"}, Summary: "Create an Anthropic-compatible message", OperationID: "createMessage",
		Description: "Uses any configured model_name and the shared chat router. Supports text, system prompts, URL/base64 images, client tools and tool results, and streaming. Target model capabilities apply. Cache hints are accepted without a caching guarantee. Thinking, server tools, documents, top_k, and other unsupported options return 400. anthropic-version is accepted for SDK compatibility.",
		Parameters:  openapi3.Parameters{sessionHeaderParameter(), {Value: &openapi3.Parameter{Name: "anthropic-version", In: "header", Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}, Example: "2023-06-01"}}},
		Security:    &openapi3.SecurityRequirements{{"anthropicApiKey": {}}, {bearerAuthName: {}}},
		RequestBody: &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{Required: true, Content: openapi3.Content{"application/json": {Schema: &openapi3.SchemaRef{Ref: "#/components/schemas/AnthropicMessagesRequest"}}}}},
		Responses:   responses,
	}}
}

func messagesRequestSchema() *openapi3.SchemaRef {
	text := openapi3.NewObjectSchema().WithProperty("type", openapi3.NewStringSchema().WithEnum("text")).WithProperty("text", openapi3.NewStringSchema()).WithProperty("cache_control", openapi3.NewObjectSchema())
	text.Required = []string{"type", "text"}
	content := openapi3.NewSchema()
	content.OneOf = openapi3.SchemaRefs{{Value: openapi3.NewStringSchema()}, {Value: openapi3.NewArraySchema().WithItems(messagesBlockSchema())}}
	message := openapi3.NewObjectSchema().WithProperty("role", openapi3.NewStringSchema().WithEnum("user", "assistant"))
	message.Properties["content"] = &openapi3.SchemaRef{Value: content}
	message.Required = []string{"role", "content"}
	system := openapi3.NewSchema()
	system.OneOf = openapi3.SchemaRefs{{Value: openapi3.NewStringSchema()}, {Value: openapi3.NewArraySchema().WithItems(text)}}
	tool := openapi3.NewObjectSchema().WithProperty("type", openapi3.NewStringSchema().WithEnum("custom")).WithProperty("name", openapi3.NewStringSchema()).WithProperty("description", openapi3.NewStringSchema()).WithProperty("input_schema", openapi3.NewObjectSchema()).WithProperty("strict", openapi3.NewBoolSchema()).WithProperty("cache_control", openapi3.NewObjectSchema())
	tool.Required = []string{"name", "input_schema"}
	choice := openapi3.NewObjectSchema().WithProperty("type", openapi3.NewStringSchema().WithEnum("auto", "any", "none", "tool")).WithProperty("name", openapi3.NewStringSchema()).WithProperty("disable_parallel_tool_use", openapi3.NewBoolSchema())
	choice.Required = []string{"type"}
	schema := openapi3.NewObjectSchema().
		WithProperty("model", openapi3.NewStringSchema()).
		WithProperty("max_tokens", openapi3.NewIntegerSchema().WithMin(1)).
		WithProperty("messages", openapi3.NewArraySchema().WithItems(message).WithMinItems(1)).
		WithProperty("stream", openapi3.NewBoolSchema()).
		WithProperty("temperature", openapi3.NewFloat64Schema().WithMin(0).WithMax(1)).
		WithProperty("top_p", openapi3.NewFloat64Schema().WithMin(0).WithMax(1)).
		WithProperty("stop_sequences", openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema())).
		WithProperty("system", system).
		WithProperty("metadata", openapi3.NewObjectSchema().WithProperty("user_id", openapi3.NewStringSchema()).WithProperty("session_id", openapi3.NewStringSchema()).WithProperty("litellm_session_id", openapi3.NewStringSchema())).
		WithProperty("tools", openapi3.NewArraySchema().WithItems(tool)).
		WithProperty("tool_choice", choice).
		WithProperty("cache_control", openapi3.NewObjectSchema())
	schema.Properties["session_id"] = sessionIDSchema()
	schema.Properties["litellm_session_id"] = sessionIDSchema()
	schema.Required = []string{"model", "max_tokens", "messages"}
	additional := false
	schema.AdditionalProperties = openapi3.AdditionalProperties{Has: &additional}
	return &openapi3.SchemaRef{Value: schema}
}

func messagesBlockSchema() *openapi3.Schema {
	source := openapi3.NewObjectSchema().WithProperty("type", openapi3.NewStringSchema().WithEnum("url", "base64")).WithProperty("url", openapi3.NewStringSchema()).WithProperty("media_type", openapi3.NewStringSchema()).WithProperty("data", openapi3.NewStringSchema())
	toolResultContent := openapi3.NewSchema()
	toolResultContent.OneOf = openapi3.SchemaRefs{{Value: openapi3.NewStringSchema()}, {Value: openapi3.NewArraySchema().WithItems(openapi3.NewObjectSchema().WithProperty("type", openapi3.NewStringSchema().WithEnum("text")).WithProperty("text", openapi3.NewStringSchema()))}}
	block := openapi3.NewObjectSchema().WithProperty("type", openapi3.NewStringSchema().WithEnum("text", "image", "tool_use", "tool_result")).WithProperty("text", openapi3.NewStringSchema()).WithProperty("source", source).
		WithProperty("id", openapi3.NewStringSchema()).WithProperty("name", openapi3.NewStringSchema()).WithProperty("input", openapi3.NewObjectSchema()).WithProperty("tool_use_id", openapi3.NewStringSchema()).WithProperty("content", toolResultContent).WithProperty("is_error", openapi3.NewBoolSchema()).WithProperty("cache_control", openapi3.NewObjectSchema())
	block.Required = []string{"type"}
	return block
}

func messagesResponseSchema() *openapi3.SchemaRef {
	usage := openapi3.NewObjectSchema().WithProperty("input_tokens", openapi3.NewIntegerSchema()).WithProperty("output_tokens", openapi3.NewIntegerSchema()).WithProperty("cache_read_input_tokens", openapi3.NewIntegerSchema()).WithProperty("cache_creation_input_tokens", openapi3.NewIntegerSchema())
	usage.Required = []string{"input_tokens", "output_tokens"}
	schema := openapi3.NewObjectSchema().WithProperty("id", openapi3.NewStringSchema()).WithProperty("type", openapi3.NewStringSchema().WithEnum("message")).WithProperty("role", openapi3.NewStringSchema().WithEnum("assistant")).WithProperty("model", openapi3.NewStringSchema()).WithProperty("content", openapi3.NewArraySchema().WithItems(messagesBlockSchema())).WithProperty("stop_reason", openapi3.NewStringSchema().WithNullable()).WithProperty("stop_sequence", openapi3.NewStringSchema().WithNullable()).WithProperty("usage", usage)
	schema.Required = []string{"id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage"}
	return &openapi3.SchemaRef{Value: schema}
}

func messagesErrorSchema() *openapi3.SchemaRef {
	schema := openapi3.NewObjectSchema().WithProperty("type", openapi3.NewStringSchema().WithEnum("error")).WithProperty("request_id", openapi3.NewStringSchema())
	schema.Properties["error"] = &openapi3.SchemaRef{Ref: "#/components/schemas/ErrorDetail"}
	schema.Required = []string{"type", "error"}
	return &openapi3.SchemaRef{Value: schema}
}
