package otel

// GenAI attribute names follow the development semantic conventions. Keep
// these strings here so a convention update has one review point.
const (
	GenAIOperationName = "gen_ai.operation.name"
	GenAIRequestModel  = "gen_ai.request.model"
	GenAIRequestStream = "gen_ai.request.stream"
	GenAIProviderName  = "gen_ai.provider.name"
	GenAIResponseModel = "gen_ai.response.model"
	GenAIInputTokens   = "gen_ai.usage.input_tokens"
	GenAIOutputTokens  = "gen_ai.usage.output_tokens"
	ServerAddress      = "server.address"
	ServerPort         = "server.port"
	ErrorType          = "error.type"
	Team               = "llmproxy.team"
	Application        = "llmproxy.application"
	APIKeyName         = "llmproxy.api_key_name"
	UserEmail          = "llmproxy.user_email"
	PoolName           = "llmproxy.pool_name"
	CostTotalUSD       = "llmproxy.cost.total_usd"
	RequestID          = "llmproxy.request_id"
)
