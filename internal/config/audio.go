package config

import (
	"fmt"
	"math"
	"net/url"
	"strings"
)

const (
	ElevenLabsSoundPath = "/sound-generation"
	ElevenLabsMusicPath = "/music"
)

// ElevenLabsAudioPath maps a deployed model family to its generation endpoint.
func ElevenLabsAudioPath(model string) string {
	switch {
	case strings.HasPrefix(model, "eleven_text_to_sound_") && model != "eleven_text_to_sound_":
		return ElevenLabsSoundPath
	case strings.HasPrefix(model, "music_") && model != "music_":
		return ElevenLabsMusicPath
	default:
		return ""
	}
}

func (mc ModelConfig) ValidateAudio() error {
	parsed := ParseModelString(mc.LiteLLMParams.Model)
	if parsed.Provider != "elevenlabs" {
		if mc.AudioCostPerSecond != nil {
			return fmt.Errorf("audio_cost_per_second requires an ElevenLabs model")
		}
		return nil
	}
	if mc.Decision != nil || mc.TPM != 0 || ElevenLabsAudioPath(parsed.ModelName) == "" {
		return fmt.Errorf("ElevenLabs requires a sound effects or music model without decision or token limits")
	}
	if strings.TrimSpace(mc.LiteLLMParams.APIKey) == "" {
		return fmt.Errorf("ElevenLabs api_key is required")
	}
	if base := mc.LiteLLMParams.APIBase; base != "" {
		u, err := url.Parse(base)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") ||
			u.User != nil || u.ForceQuery || u.RawQuery != "" || strings.Contains(base, "#") {
			return fmt.Errorf("ElevenLabs api_base must be an HTTP(S) URL without credentials, a query, or a fragment")
		}
	}
	if rate := mc.AudioCostPerSecond; rate != nil && (math.IsNaN(*rate) || math.IsInf(*rate, 0) || *rate < 0) {
		return fmt.Errorf("audio_cost_per_second must be finite and nonnegative")
	}
	return nil
}
