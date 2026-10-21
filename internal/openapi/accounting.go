package openapi

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
)

func accountingSchema() *openapi3.SchemaRef {
	ref, err := openapi3gen.NewSchemaRefForValue(accounting.Record{}, nil)
	if err != nil {
		panic(err)
	} // The static record contains only JSON-compatible fields.
	ref.Value.Description = "Durable accounting for one logical LLM request. Money is a decimal USD string; null means unknown. coverage describes whether selected costs cover every upstream attempt. Token detail counts are subsets of input/output totals."
	return ref
}
func accountingPath() *openapi3.PathItem {
	return &openapi3.PathItem{Get: &openapi3.Operation{
		Tags: []string{"Accounting"}, Summary: "Get request accounting", OperationID: "getAccounting",
		Description: "Returns pending or final cost and token evidence, including retries. App keys can read only their own records; master keys can read all records. Missing and inaccessible records both return 404.",
		Security:    &openapi3.SecurityRequirements{{bearerAuthName: []string{}}},
		Parameters:  openapi3.Parameters{&openapi3.ParameterRef{Value: openapi3.NewPathParameter("id").WithSchema(openapi3.NewStringSchema())}},
		Responses:   openapi3.NewResponses(openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{Value: &openapi3.Response{Description: strPtr("Accounting record"), Content: openapi3.Content{"application/json": &openapi3.MediaType{Schema: &openapi3.SchemaRef{Ref: "#/components/schemas/AccountingRecord"}}}}}), openapi3.WithStatus(http.StatusNotFound, errorResponseRef("Accounting unavailable or inaccessible")), openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Authentication required"))),
	}}
}
func addAccountingContract(paths *openapi3.Paths) {
	paths.Set("/v1/accounting/{id}", accountingPath())
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/embeddings", "/v1/responses"} {
		op := paths.Value(path).Post
		op.Parameters = append(op.Parameters, &openapi3.ParameterRef{Value: openapi3.NewHeaderParameter("X-LLM-Include-Accounting").WithSchema(openapi3.NewBoolSchema()).WithDescription("Opt in to proxy_accounting in JSON responses or a final accounting SSE chunk/event. Default false; never forwarded upstream.")})
		op.Description += " Every call returns X-LLM-Accounting-ID, X-LLM-Accounting-State, and X-LLM-Accounting-Coverage. Final non-streaming calls include X-LLM-Cost-USD and X-LLM-Cost-Source when known, plus LiteLLM cost/token aliases. Streaming headers remain pending; fetch /v1/accounting/{id} after completion or opt in to inline accounting. Chat emits a final choices:[] chunk with proxy_accounting before [DONE]; Responses emits event: proxy.accounting."
		for _, response := range op.Responses.Map() {
			if response.Value == nil {
				continue
			}
			if response.Value.Headers == nil {
				response.Value.Headers = openapi3.Headers{}
			}
			for _, name := range []string{"X-LLM-Accounting-ID", "X-LLM-Accounting-State", "X-LLM-Accounting-Coverage", "X-LLM-Cost-USD", "X-LLM-Cost-Source", "x-litellm-response-cost", "x-litellm-total-tokens"} {
				response.Value.Headers[name] = &openapi3.HeaderRef{Value: &openapi3.Header{Parameter: openapi3.Parameter{Schema: &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}}}}
			}
		}
	}
}
