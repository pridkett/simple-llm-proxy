package model

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// systemOneInput preserves message roles and content order as structured state.
// Images become TypeSafe placeholders or numbered Cloudflare image references.
// It never downloads media or substitutes media bytes into text instructions.
func (r *DecisionRequest) systemOneInput(style string) (json.RawMessage, json.RawMessage, error) {
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(r.Fields["input"], &messages); err != nil {
		return nil, nil, fmt.Errorf("input must contain messages")
	}
	state := []map[string]any{}
	assets := map[string]any{}
	images := []string{}
	for _, message := range messages {
		for key := range message {
			if !slices.Contains([]string{"type", "role", "content"}, key) {
				return nil, nil, fmt.Errorf("input item field %q requires native Decisions transport", key)
			}
		}
		var role, kind string
		_ = json.Unmarshal(message["role"], &role)
		_ = json.Unmarshal(message["type"], &kind)
		if role == "" || kind != "" && kind != "message" {
			return nil, nil, fmt.Errorf("only messages can be translated into System One state")
		}
		var text string
		if json.Unmarshal(message["content"], &text) == nil {
			state = append(state, map[string]any{"role": role, "content": text})
			continue
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(message["content"], &parts) != nil {
			return nil, nil, fmt.Errorf("message content must be text or parts")
		}
		content := []any{}
		for _, part := range parts {
			var t string
			_ = json.Unmarshal(part["type"], &t)
			if t == "input_text" {
				for key := range part {
					if key != "type" && key != "text" {
						return nil, nil, fmt.Errorf("text field %q requires native Decisions transport", key)
					}
				}
				if json.Unmarshal(part["text"], &text) != nil {
					return nil, nil, fmt.Errorf("input_text requires text")
				}
				content = append(content, text)
				continue
			}
			if t != "input_image" {
				return nil, nil, fmt.Errorf("%s parts require native transport", t)
			}
			for key := range part {
				if key != "type" && key != "image_url" {
					return nil, nil, fmt.Errorf("image field %q requires native Decisions transport", key)
				}
			}
			var ref string
			if json.Unmarshal(part["image_url"], &ref) != nil {
				return nil, nil, fmt.Errorf("input_image requires an image_url string")
			}
			mediaType, encoded, ok := strings.Cut(ref, ";base64,")
			if !ok || !strings.HasPrefix(mediaType, "data:image/") {
				return nil, nil, fmt.Errorf("image translation requires a base64 data URL; remote media URLs are never fetched")
			}
			format := strings.TrimPrefix(mediaType, "data:image/")
			formats := []string{"png", "jpeg", "jpg"}
			if style == "images" {
				formats = []string{"png", "jpeg", "webp"}
			}
			if !slices.Contains(formats, format) {
				return nil, nil, fmt.Errorf("unsupported image format %q for this deployment", format)
			}
			if data, err := base64.StdEncoding.DecodeString(encoded); err != nil || len(data) == 0 {
				return nil, nil, fmt.Errorf("invalid base64 image")
			}
			images = append(images, ref)
			if style == "images" {
				content = append(content, map[string]any{"image": len(images)})
				continue
			}
			hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", len(images), ref)))
			placeholder := fmt.Sprintf("proxy_image_%x", hash[:12])
			for strings.Contains(string(r.Fields["input"]), placeholder) {
				placeholder += "_"
			}
			assets[placeholder] = map[string]any{"type": "image", "format": format, "data_base64": encoded}
			content = append(content, placeholder)
		}
		state = append(state, map[string]any{"role": role, "content": content})
	}
	s, _ := json.Marshal(state)
	if len(images) == 0 {
		return s, nil, nil
	}
	var media json.RawMessage
	if style == "images" {
		media, _ = json.Marshal(images)
	} else {
		media, _ = json.Marshal(assets)
	}
	return s, media, nil
}
