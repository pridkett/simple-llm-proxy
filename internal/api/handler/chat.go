package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// ChatCompletions handles POST /v1/chat/completions requests.
func ChatCompletions(r *router.Router, store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		startTime := time.Now()

		var chatReq model.ChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid request body: "+err.Error()))
			return
		}

		if chatReq.Model == "" {
			model.WriteError(w, model.ErrBadRequest("model is required"))
			return
		}

		if len(chatReq.Messages) == 0 {
			model.WriteError(w, model.ErrBadRequest("messages is required"))
			return
		}

		// Try to get a deployment with retries
		tried := make(map[*provider.Deployment]bool)
		var lastErr error

		for attempt := 0; attempt <= r.NumRetries(); attempt++ {
			deployment, err := r.GetDeploymentWithRetry(chatReq.Model, tried)
			if err != nil {
				if attempt == 0 {
					// First attempt - model doesn't exist
					model.WriteError(w, model.ErrModelNotFound(chatReq.Model))
					return
				}
				// All deployments tried
				break
			}
			tried[deployment] = true

			// Create request with actual model name
			providerReq := chatReq
			providerReq.Model = deployment.ActualModel

			if chatReq.Stream {
				err = handleStreamingResponse(ctx, w, deployment, &providerReq, r, store, startTime)
			} else {
				err = handleNonStreamingResponse(ctx, w, deployment, &providerReq, r, store, startTime)
			}

			if err == nil {
				return
			}

			lastErr = err
			r.ReportFailure(deployment)
		}

		// All retries exhausted
		model.WriteError(w, model.ErrProviderError("all providers", lastErr))
	}
}

func handleNonStreamingResponse(
	ctx context.Context,
	w http.ResponseWriter,
	deployment *provider.Deployment,
	req *model.ChatCompletionRequest,
	r *router.Router,
	store storage.Storage,
	startTime time.Time,
) error {
	resp, err := deployment.Provider.ChatCompletion(ctx, req)
	if err != nil {
		return err
	}

	r.ReportSuccess(deployment)

	// Log the request if storage is available
	if store != nil && resp.Usage != nil {
		go logRequest(store, deployment, "/v1/chat/completions", resp.Usage, http.StatusOK, startTime)
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(resp)
}

func handleStreamingResponse(
	ctx context.Context,
	w http.ResponseWriter,
	deployment *provider.Deployment,
	req *model.ChatCompletionRequest,
	r *router.Router,
	store storage.Storage,
	startTime time.Time,
) error {
	stream, err := deployment.Provider.ChatCompletionStream(ctx, req)
	if err != nil {
		return err
	}
	defer stream.Close()

	r.ReportSuccess(deployment)

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			// Send [DONE] marker
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return nil
		}
		if err != nil {
			return err
		}

		data, err := json.Marshal(chunk)
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

func logRequest(store storage.Storage, deployment *provider.Deployment, endpoint string, usage *model.Usage, status int, startTime time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log := &storage.RequestLog{
		RequestID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Model:            deployment.ModelName,
		Provider:         deployment.ProviderName,
		Endpoint:         endpoint,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		StatusCode:       status,
		LatencyMS:        time.Since(startTime).Milliseconds(),
		RequestTime:      startTime,
	}

	store.LogRequest(ctx, log)
}
