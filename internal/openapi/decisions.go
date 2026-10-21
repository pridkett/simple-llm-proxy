package openapi

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
)

func decisionsPath(systemOne bool, operationID string) *openapi3.PathItem {
	input, questions, answers := "input", openapi3.NewArraySchema().WithItems(openapi3.NewObjectSchema()), openapi3.NewArraySchema().WithItems(openapi3.NewObjectSchema())
	description := "Answer typed predicate, choice, score, or box questions using a declared decision deployment. Text or multimodal input is preserved. Requires allow_decisions=true on app keys AND the existing model allowlist. Model-specific capabilities and USD prices appear in decision_deployments on model discovery. Streaming and background calls are unsupported. Translations reject extensions that cannot be preserved. X-Request-Cost reports the USD request cost when known."
	if systemOne {
		input = "state"
		questions = openapi3.NewObjectSchema()
		answers = openapi3.NewObjectSchema()
		description += " Uses native System One named questions (noul/choice/score/box); supports provider media extensions assets/images/audio/videos."
	}
	request := openapi3.NewObjectSchema().WithProperty("model", openapi3.NewStringSchema()).WithProperty(input, &openapi3.Schema{}).WithProperty("questions", questions)
	request.Required = []string{"model", input, "questions"}
	response := openapi3.NewObjectSchema().WithProperty("model", openapi3.NewStringSchema()).WithProperty("answers", answers).WithProperty("usage", openapi3.NewObjectSchema())
	response.Required = []string{"answers"}
	return &openapi3.PathItem{Post: &openapi3.Operation{
		Tags: []string{"Decisions"}, Summary: "Create a decision", Description: description, OperationID: operationID,
		Parameters: openapi3.Parameters{sessionHeaderParameter()}, Security: &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
		RequestBody: &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{Required: true, Content: openapi3.NewContentWithJSONSchema(request)}},
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{Value: &openapi3.Response{Description: strPtr("Typed answers, preserving upstream extensions and usage"), Content: openapi3.NewContentWithJSONSchema(response)}}),
			openapi3.WithStatus(http.StatusBadRequest, errorResponseRef("Invalid request, unsupported modality, question type, or translation")),
			openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Invalid API key")),
			openapi3.WithStatus(http.StatusForbidden, errorResponseRef("Decision access disabled or model not allowed")),
			openapi3.WithStatus(http.StatusNotFound, errorResponseRef("Unknown model")),
			openapi3.WithStatus(http.StatusRequestEntityTooLarge, errorResponseRef("Request exceeds 32 MiB")),
			openapi3.WithStatus(http.StatusTooManyRequests, errorResponseRef("Key or provider rate limit exceeded")),
			openapi3.WithStatus(http.StatusBadGateway, errorResponseRef("Upstream failure")),
			openapi3.WithStatus(http.StatusServiceUnavailable, errorResponseRef("No healthy compatible deployment")),
		),
	}}
}
