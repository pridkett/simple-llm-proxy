package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode"
)

// decodeSessionRequest consumes proxy-only session attribution before decoding
// the provider request. No dependency on body capture, routing affinity, or user.
// Precedence: header, top-level session_id/litellm_session_id, then metadata.
func decodeSessionRequest(req *http.Request, dst any) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(req.Body).Decode(&fields); err != nil {
		return "", err
	}
	if fields == nil {
		return "", fmt.Errorf("request must be a JSON object")
	}
	candidates := []string{req.Header.Get("X-Session-ID")}
	consume := func(obj map[string]json.RawMessage) error {
		for _, key := range []string{"session_id", "litellm_session_id"} {
			if raw, ok := obj[key]; ok {
				var value string
				if err := json.Unmarshal(raw, &value); err != nil {
					return fmt.Errorf("%s must be a string", key)
				}
				candidates = append(candidates, value)
				delete(obj, key)
			}
		}
		return nil
	}
	if err := consume(fields); err != nil {
		return "", err
	}
	var metadata map[string]json.RawMessage
	if raw, ok := fields["metadata"]; ok && json.Unmarshal(raw, &metadata) == nil && metadata != nil {
		before := len(metadata)
		if err := consume(metadata); err != nil {
			return "", err
		}
		if len(metadata) != before {
			if len(metadata) == 0 {
				delete(fields, "metadata")
			} else {
				fields["metadata"], _ = json.Marshal(metadata)
			}
		}
	}
	var sessionID string
	for _, value := range candidates {
		if strings.ContainsFunc(value, unicode.IsControl) {
			return "", fmt.Errorf("session ID must contain no control characters")
		}
		value = strings.TrimSpace(value)
		if len(value) > 256 {
			return "", fmt.Errorf("session ID must be at most 256 bytes and contain no control characters")
		}
		if sessionID == "" {
			sessionID = value
		}
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	return sessionID, json.Unmarshal(body, dst)
}
