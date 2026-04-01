// Package minimax implements the MiniMax provider as a wrapper around
// openaicompat.BaseProvider. MiniMax is a China-based LLM provider with
// an OpenAI-compatible API that sometimes returns tool calls as XML
// blocks embedded in the content string rather than in the tool_calls
// array. The provider includes a TransformResponse hook that parses
// these XML blocks and promotes them to proper tool_calls entries.
package minimax

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/provider/openaicompat"
)

const defaultBaseURL = "https://api.minimax.io/v1"

// XML parsing types for MiniMax's tool call format.
type minimaxToolCallBlock struct {
	Invokes []minimaxInvoke `xml:"invoke"`
}

type minimaxInvoke struct {
	Name       string             `xml:"name,attr"`
	Parameters []minimaxParameter `xml:"parameter"`
}

type minimaxParameter struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

// toolCallBlockRe matches <minimax:tool_call>...</minimax:tool_call> blocks.
var toolCallBlockRe = regexp.MustCompile(`(?s)<minimax:tool_call>(.*?)</minimax:tool_call>`)

// parseMinimaxToolCalls extracts tool calls from XML blocks embedded in content.
// Returns the extracted tool calls, the cleaned content (XML blocks removed and
// whitespace trimmed), and any error. If no XML blocks are found, returns nil
// tool calls and the original content unchanged.
func parseMinimaxToolCalls(content string) ([]model.ToolCall, string, error) {
	matches := toolCallBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil, content, nil
	}

	var allToolCalls []model.ToolCall
	cleanedContent := content
	globalIndex := 0

	for _, match := range matches {
		fullMatch := match[0] // The entire <minimax:tool_call>...</minimax:tool_call>
		innerXML := match[1]  // Content between the tags

		// Remove the full XML block from content
		cleanedContent = strings.Replace(cleanedContent, fullMatch, "", 1)

		// Wrap in root element for XML parsing
		wrappedXML := "<root>" + innerXML + "</root>"

		var block minimaxToolCallBlock
		if err := xml.Unmarshal([]byte(wrappedXML), &block); err != nil {
			return nil, content, fmt.Errorf("parsing minimax tool call XML: %w", err)
		}

		for _, invoke := range block.Invokes {
			// Build arguments map from parameters
			args := make(map[string]interface{})
			for _, param := range invoke.Parameters {
				args[param.Name] = param.Value
			}

			argsJSON, err := json.Marshal(args)
			if err != nil {
				return nil, content, fmt.Errorf("marshaling tool call arguments: %w", err)
			}

			allToolCalls = append(allToolCalls, model.ToolCall{
				ID:   fmt.Sprintf("call_%d", globalIndex),
				Type: "function",
				Function: model.FunctionCall{
					Name:      invoke.Name,
					Arguments: string(argsJSON),
				},
			})
			globalIndex++
		}
	}

	return allToolCalls, strings.TrimSpace(cleanedContent), nil
}

// newTransformResponse creates a TransformResponseFunc that parses XML tool calls
// from MiniMax response content. When xmlEnabled is false, returns nil (no transform).
func newTransformResponse(xmlEnabled bool) openaicompat.TransformResponseFunc {
	if !xmlEnabled {
		return nil
	}

	return func(resp *model.ChatCompletionResponse) *model.ChatCompletionResponse {
		for i := range resp.Choices {
			choice := &resp.Choices[i]
			if choice.Message == nil {
				continue
			}

			// Skip if tool_calls are already populated
			if len(choice.Message.ToolCalls) > 0 {
				continue
			}

			// Extract content as string
			content, ok := choice.Message.Content.(string)
			if !ok || content == "" {
				continue
			}

			// Check for XML tool call markers
			if !strings.Contains(content, "<minimax:tool_call>") {
				continue
			}

			toolCalls, cleanedContent, err := parseMinimaxToolCalls(content)
			if err != nil || len(toolCalls) == 0 {
				continue
			}

			choice.Message.ToolCalls = toolCalls
			if cleanedContent == "" {
				choice.Message.Content = ""
			} else {
				choice.Message.Content = cleanedContent
			}
			choice.FinishReason = "tool_calls"
		}

		return resp
	}
}

// New creates a new MiniMax provider.
// XML tool-call parsing is enabled by default (D-17). Set opts.XMLToolCalls
// to false to disable it.
func New(opts provider.ProviderOptions) provider.Provider {
	baseURL := defaultBaseURL
	if opts.APIBase != "" {
		baseURL = strings.TrimSuffix(opts.APIBase, "/")
	}

	// XML tool call parsing enabled by default (D-17)
	xmlEnabled := true
	if opts.XMLToolCalls != nil {
		xmlEnabled = *opts.XMLToolCalls
	}

	return &openaicompat.BaseProvider{
		ProviderName:      "minimax",
		BaseURL:           baseURL,
		Client:            &http.Client{},
		Auth:              func(req *http.Request) { req.Header.Set("Authorization", "Bearer "+opts.APIKey) },
		ExtraHeaders:      opts.ExtraHeaders,
		DoneSentinel:      "[DONE]",
		TransformResponse: newTransformResponse(xmlEnabled),
	}
}

func init() {
	provider.Register("minimax", func(opts provider.ProviderOptions) provider.Provider {
		return New(opts)
	})
}
