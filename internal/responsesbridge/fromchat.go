package responsesbridge

import (
	"context"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// chatBacked serves the Responses API from a provider that only speaks chat.
type chatBacked struct {
	chat provider.Provider
}

// FromChat lets a chat provider answer Responses requests. It is stateless:
// background jobs, retrieval and server-side conversation state are rejected.
func FromChat(p provider.Provider) provider.ResponsesProvider {
	return &chatBacked{chat: p}
}

func (p *chatBacked) CreateResponse(ctx context.Context, req *model.ResponsesRequest) (*model.ResponsesResponse, error) {
	chat, echo, err := chatRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.chat.ChatCompletion(ctx, chat)
	if err != nil {
		return nil, err
	}
	return fromChatResponse(resp, newEnvelope(req.Model, echo))
}

func (p *chatBacked) CreateResponseStream(ctx context.Context, req *model.ResponsesRequest) (provider.ResponsesStream, error) {
	chat, echo, err := chatRequest(req)
	if err != nil {
		return nil, err
	}
	chat.Stream = true
	// Responses always reports usage on the terminal event.
	chat.StreamOptions = &model.StreamOptions{IncludeUsage: true}
	stream, err := p.chat.ChatCompletionStream(ctx, chat)
	if err != nil {
		return nil, err
	}
	return newChatStream(stream, newEnvelope(req.Model, echo)), nil
}

func (p *chatBacked) GetResponse(context.Context, string) (*model.ResponsesResponse, error) {
	return nil, invalid("responses from this model are not stored")
}

func (p *chatBacked) CancelResponse(context.Context, string) (*model.ResponsesResponse, error) {
	return nil, invalid("responses from this model are not stored")
}
