package openapi

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
)

func addMediaPaths(paths *openapi3.Paths) {
	for _, route := range []struct {
		path, id, summary   string
		requests, responses []string
		status              int
	}{
		{"/v1/images/generations", "generateImage", "Generate images", []string{"application/json"}, []string{"application/json", "text/event-stream"}, http.StatusOK},
		{"/v1/images/edits", "editImage", "Edit images", []string{"application/json", "multipart/form-data"}, []string{"application/json", "text/event-stream"}, http.StatusOK},
		{"/v1/audio/speech", "createSpeech", "Generate speech", []string{"application/json"}, []string{"application/octet-stream", "text/event-stream"}, http.StatusOK},
		{"/v1/audio/transcriptions", "transcribeAudio", "Transcribe a file", []string{"multipart/form-data"}, []string{"application/json", "text/plain", "text/event-stream"}, http.StatusOK},
		{"/v1/audio/translations", "translateAudio", "Translate a file", []string{"multipart/form-data"}, []string{"application/json", "text/plain"}, http.StatusOK},
		{"/v1/realtime/calls", "createRealtimeCall", "Create a WebRTC call (master key)", []string{"multipart/form-data"}, []string{"application/sdp"}, http.StatusCreated},
		{"/v1/realtime/client_secrets", "createRealtimeClientSecret", "Create a WebRTC client secret (master key)", []string{"application/json"}, []string{"application/json"}, http.StatusOK},
	} {
		op := mediaOperation(route.id, route.summary, route.status, route.responses)
		op.Description = "Uses the native OpenAI request and response formats. Set the model to a configured OpenAI alias."
		body := openapi3.NewObjectSchema().WithProperty("model", openapi3.NewStringSchema()).WithRequired([]string{"model"})
		if route.id == "createRealtimeCall" || route.id == "createRealtimeClientSecret" {
			op.Description = "Requires the proxy master key and a configured OpenAI alias in session.model."
			body = openapi3.NewObjectSchema().WithProperty("session", body).WithRequired([]string{"session"})
		}
		if route.id == "createRealtimeCall" {
			body.WithProperty("sdp", openapi3.NewStringSchema()).WithRequired([]string{"session", "sdp"})
		}
		content := openapi3.Content{}
		for _, contentType := range route.requests {
			content[contentType] = &openapi3.MediaType{Schema: &openapi3.SchemaRef{Value: body}}
		}
		op.RequestBody = &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{Required: true, Content: content}}
		paths.Set(route.path, &openapi3.PathItem{Post: op})
	}

	for _, route := range []struct{ path, id, summary string }{
		{"/v1/realtime", "connectRealtime", "Connect to a Realtime conversation or transcription session"},
		{"/v1/realtime/translations", "connectRealtimeTranslation", "Connect to a Realtime translation session"},
	} {
		op := mediaOperation(route.id, route.summary, http.StatusSwitchingProtocols, nil)
		op.Description = "Requires an authenticated WebSocket upgrade. Exchange native OpenAI Realtime events over the upgraded connection."
		op.Parameters = openapi3.Parameters{
			&openapi3.ParameterRef{Value: openapi3.NewQueryParameter("model").WithRequired(true).WithSchema(openapi3.NewStringSchema()).WithDescription("Configured OpenAI model alias")},
		}
		if route.id == "connectRealtime" {
			op.Parameters = append(op.Parameters, &openapi3.ParameterRef{Value: openapi3.NewQueryParameter("intent").WithSchema(openapi3.NewStringSchema().WithEnum("transcription"))})
		}
		paths.Set(route.path, &openapi3.PathItem{Get: op})
	}
	addElevenLabsPaths(paths)
}

func addElevenLabsPaths(paths *openapi3.Paths) {
	sound := openapi3.NewObjectSchema().
		WithProperty("text", openapi3.NewStringSchema()).
		WithProperty("duration_seconds", openapi3.NewFloat64Schema().WithMin(0.5).WithMax(30)).
		WithProperty("loop", openapi3.NewBoolSchema()).
		WithProperty("prompt_influence", openapi3.NewFloat64Schema().WithMin(0).WithMax(1)).
		WithRequired([]string{"model", "text", "duration_seconds"})
	music := openapi3.NewObjectSchema().
		WithProperty("prompt", openapi3.NewStringSchema()).
		WithProperty("music_length_ms", openapi3.NewIntegerSchema().WithMin(3000).WithMax(600000)).
		WithProperty("force_instrumental", openapi3.NewBoolSchema()).
		WithRequired([]string{"model", "prompt", "music_length_ms"})
	for _, route := range []struct {
		path, id, summary, metadataHeader string
		body                              *openapi3.Schema
	}{
		{config.ElevenLabsSoundPath, "generateSoundEffect", "Generate an ElevenLabs sound effect", "character-cost", sound},
		{config.ElevenLabsMusicPath, "generateMusic", "Generate an ElevenLabs music file", "song-id", music},
	} {
		op := mediaOperation(route.id, route.summary, http.StatusOK, []string{"audio/mpeg", "audio/wav", "application/octet-stream"})
		op.Description = "Set model to a configured ElevenLabs alias for this operation. The response is binary audio. Use X-LLM-Accounting-ID to retrieve the final usage record."
		route.body.WithProperty("model", openapi3.NewStringSchema())
		op.RequestBody = &openapi3.RequestBodyRef{Value: openapi3.NewRequestBody().WithRequired(true).WithJSONSchema(route.body)}
		op.Parameters = openapi3.Parameters{
			&openapi3.ParameterRef{Value: openapi3.NewQueryParameter("output_format").WithSchema(openapi3.NewStringSchema())},
		}
		ok := op.Responses.Status(http.StatusOK).Value
		ok.Headers = openapi3.Headers{}
		for _, name := range []string{"X-LLM-Accounting-ID", route.metadataHeader} {
			ok.Headers[name] = &openapi3.HeaderRef{Value: &openapi3.Header{Parameter: openapi3.Parameter{
				Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
			}}}
		}
		paths.Set("/v1"+route.path, &openapi3.PathItem{Post: op})
	}
}

func mediaOperation(id, summary string, status int, formats []string) *openapi3.Operation {
	content := openapi3.Content{}
	for _, format := range formats {
		schema := openapi3.NewStringSchema()
		if format == "application/json" {
			schema = openapi3.NewObjectSchema()
		} else if format == "application/octet-stream" || format == "audio/mpeg" || format == "audio/wav" {
			schema = schema.WithFormat("binary")
		}
		content[format] = &openapi3.MediaType{Schema: &openapi3.SchemaRef{Value: schema}}
	}
	return &openapi3.Operation{
		Tags: []string{"Media"}, Summary: summary, OperationID: id,
		Security: &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(status, &openapi3.ResponseRef{Value: &openapi3.Response{Description: strPtr(http.StatusText(status)), Content: content}}),
			openapi3.WithStatus(http.StatusBadRequest, errorResponseRef("Invalid request")),
			openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication required")),
			openapi3.WithStatus(http.StatusForbidden, errorResponseRef("Key or model is not allowed")),
			openapi3.WithStatus(http.StatusNotFound, errorResponseRef("Model not found")),
			openapi3.WithStatus(http.StatusTooManyRequests, errorResponseRef("Rate or budget limit reached")),
		),
	}
}
