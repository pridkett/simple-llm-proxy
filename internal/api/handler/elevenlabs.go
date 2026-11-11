package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// ElevenLabs proxies a single audio generation and returns its binary output.
func (h Media) ElevenLabs(path string) http.HandlerFunc {
	metadataHeader := "song-id"
	if path == config.ElevenLabsSoundPath {
		metadataHeader = "character-cost"
	}
	route := mediaRoute{
		providerName:        "elevenlabs",
		operation:           "elevenlabs_audio",
		stopOnUpstreamError: true,
		eligible: func(d *provider.Deployment) bool {
			_, supported := d.Provider.(provider.AudioGenerationProvider)
			return supported && config.ElevenLabsAudioPath(d.ActualModel) == path
		},
	}
	return func(w http.ResponseWriter, req *http.Request) {
		if header := req.Header.Get("Content-Type"); header != "" {
			if contentType, _, err := mime.ParseMediaType(header); err != nil || contentType != "application/json" {
				model.WriteError(w, model.ErrBadRequest("expected application/json"))
				return
			}
		}
		query, err := url.ParseQuery(req.URL.RawQuery)
		format := query.Get("output_format")
		if err != nil || len(query) > 1 || (len(query) == 1 && (len(query["output_format"]) != 1 || format == "" || len(format) > 64)) {
			model.WriteError(w, model.ErrBadRequest("only a single output_format query parameter is supported"))
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, 64<<10)
		var fields map[string]json.RawMessage
		sessionID, err := decodeSessionRequest(req, &fields, true)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				model.WriteError(w, &model.ProxyError{StatusCode: http.StatusRequestEntityTooLarge, Message: "audio request exceeds size limit", Type: "invalid_request_error"})
			} else {
				model.WriteError(w, model.ErrBadRequest(err.Error()))
			}
			return
		}
		alias, err := mediaModel(fields["model"])
		if err != nil {
			model.WriteError(w, model.ErrBadRequest(err.Error()))
			return
		}
		duration, err := prepareElevenLabsBody(path, fields)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest(err.Error()))
			return
		}

		var response *http.Response
		h.route(w, req, alias, sessionID, route, func(ctx context.Context, d *provider.Deployment) error {
			fields["model_id"], _ = json.Marshal(d.ActualModel)
			body, err := json.Marshal(fields)
			if err != nil {
				return err
			}
			audio := d.Provider.(provider.AudioGenerationProvider)
			// A reader without GetBody prevents transport replay of this POST.
			response, err = audio.GenerateAudio(ctx, d.ActualModel, format, io.NopCloser(bytes.NewReader(body)))
			usage := accounting.AudioGenerationUsage{RequestedDurationMS: duration}
			if err == nil {
				if path == config.ElevenLabsSoundPath {
					if cost, err := strconv.ParseInt(response.Header.Get("character-cost"), 10, 64); err == nil && cost >= 0 {
						usage.CharacterCost = &cost
					}
				} else {
					usage.SongID = response.Header.Get("song-id")
				}
				if rate := d.AudioCostPerSecond; rate != nil {
					value := strconv.FormatFloat(*rate, 'f', -1, 64)
					usage.USDPerSecond = &value
				}
			}
			accounting.ObserveAudioGeneration(ctx, usage)
			return err
		}, func(ctx context.Context, _ string) (int, bool, error) {
			return deliverMediaHTTP(ctx, w, response, metadataHeader)
		})
	}
}

func prepareElevenLabsBody(path string, fields map[string]json.RawMessage) (int64, error) {
	delete(fields, "model")
	promptField := "prompt"
	sound := path == config.ElevenLabsSoundPath
	if sound {
		promptField = "text"
	}
	for field := range fields {
		allowed := field == "prompt" || field == "music_length_ms" || field == "force_instrumental"
		if sound {
			allowed = field == "text" || field == "duration_seconds" || field == "loop" || field == "prompt_influence"
		}
		if !allowed {
			return 0, fmt.Errorf("unsupported audio generation field %q", field)
		}
	}
	var prompt string
	if json.Unmarshal(fields[promptField], &prompt) != nil || strings.TrimSpace(prompt) == "" {
		return 0, fmt.Errorf("%s is required", promptField)
	}

	if sound {
		var seconds float64
		if json.Unmarshal(fields["duration_seconds"], &seconds) != nil || seconds < 0.5 || seconds > 30 {
			return 0, fmt.Errorf("duration_seconds must be between 0.5 and 30")
		}
		millis := int64(math.Round(seconds * 1000))
		fields["duration_seconds"], _ = json.Marshal(float64(millis) / 1000)
		return millis, nil
	}
	var millis int64
	if json.Unmarshal(fields["music_length_ms"], &millis) != nil || millis < 3000 || millis > 600000 {
		return 0, fmt.Errorf("music_length_ms must be an integer between 3000 and 600000")
	}
	return millis, nil
}
