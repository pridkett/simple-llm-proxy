package openapi

import (
	"math"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
)

const keyIssuerAuthName = "keyIssuerAuth"

func addKeyIssuancePaths(paths *openapi3.Paths) {
	const path = "/v1/key-issuance/keys"
	security := &openapi3.SecurityRequirements{{keyIssuerAuthName: []string{}}}
	deadline := openapi3.NewStringSchema().WithFormat("date-time")
	allowlist := openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema()).WithMinItems(1)
	settings := openapi3.NewObjectSchema().
		WithProperty("name", openapi3.NewStringSchema()).
		WithProperty("expires_at", deadline).
		WithProperty("allowed_models", allowlist).
		WithProperty("max_rpm", openapi3.NewIntegerSchema().WithMin(1).WithMax(math.MaxInt32)).
		WithProperty("max_rpd", openapi3.NewIntegerSchema().WithMin(1).WithMax(math.MaxInt32)).
		WithProperty("max_budget", openapi3.NewFloat64Schema()).
		WithProperty("soft_budget", openapi3.NewFloat64Schema()).
		WithProperty("allow_decisions", openapi3.NewBoolSchema()).
		WithRequired([]string{"name", "expires_at", "allowed_models"})
	created := openapi3.NewObjectSchema().
		WithProperty("id", openapi3.NewInt64Schema()).
		WithProperty("key", openapi3.NewStringSchema()).
		WithProperty("issuer_id", openapi3.NewStringSchema()).
		WithProperty("application_id", openapi3.NewInt64Schema()).
		WithProperty("allowed_models", allowlist).
		WithProperty("expires_at", deadline).
		WithRequired([]string{"id", "key", "issuer_id", "application_id", "allowed_models", "expires_at"})

	paths.Set(path, &openapi3.PathItem{Post: &openapi3.Operation{
		Tags: []string{"Key issuance"}, Summary: "Issue an expiring app key", OperationID: "issueAppKey",
		Description: "Creates a key for the issuer's configured application. Requested permissions must fit the issuer policy. Omitted hard limits inherit configured limits. The plaintext key appears only in the creation response.",
		Security:    security,
		RequestBody: &openapi3.RequestBodyRef{Value: openapi3.NewRequestBody().WithRequired(true).WithJSONSchema(settings)},
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(http.StatusCreated, &openapi3.ResponseRef{Value: openapi3.NewResponse().WithDescription("Issued app key").WithJSONSchema(created)}),
			openapi3.WithStatus(http.StatusBadRequest, errorResponseRef("Invalid key settings")),
			openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Issuer credential required")),
			openapi3.WithStatus(http.StatusForbidden, errorResponseRef("Requested key exceeds the issuer's scope")),
			openapi3.WithStatus(http.StatusRequestEntityTooLarge, errorResponseRef("Request exceeds the size limit")),
		),
	}})
	paths.Set(path+"/{id}", &openapi3.PathItem{Delete: &openapi3.Operation{
		Tags: []string{"Key issuance"}, Summary: "Revoke an issued app key", OperationID: "revokeIssuedAppKey",
		Description: "Revokes only a key created by this issuer for its configured application. Repeating the request for the same key is safe.",
		Security:    security,
		Parameters:  openapi3.Parameters{&openapi3.ParameterRef{Value: openapi3.NewPathParameter("id").WithSchema(openapi3.NewInt64Schema().WithMin(1))}},
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(http.StatusNoContent, &openapi3.ResponseRef{Value: openapi3.NewResponse().WithDescription("Key revoked")}),
			openapi3.WithStatus(http.StatusBadRequest, errorResponseRef("Invalid key ID")),
			openapi3.WithStatus(http.StatusUnauthorized, errorResponseRef("Issuer credential required")),
			openapi3.WithStatus(http.StatusNotFound, errorResponseRef("Issued key unavailable or outside the issuer's scope")),
		),
	}})
}
