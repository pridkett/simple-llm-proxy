package config

import (
	"strings"
	"testing"
)

func TestElevenLabsAudioConfig(t *testing.T) {
	t.Setenv("TEST_ELEVENLABS_KEY", "server-key")
	const model = `
model_list:
  - model_name: game-sfx
    audio_cost_per_second: 0.0025
    litellm_params:
      model: elevenlabs/eleven_text_to_sound_v3
      api_key: os.environ/TEST_ELEVENLABS_KEY
`
	cfg, err := Parse([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.ModelList[0]
	if d.LiteLLMParams.APIKey != "server-key" || d.AudioCostPerSecond == nil || *d.AudioCostPerSecond != 0.0025 {
		t.Fatal("ElevenLabs deployment did not load its credential and estimate")
	}
	for _, tc := range []struct{ name, from, to, message string }{
		{"negative rate", "0.0025", "-0.01", "finite and nonnegative"},
		{"unresolved credential", "os.environ/TEST_ELEVENLABS_KEY", "os.environ/UNSET_TEST_ELEVENLABS_KEY", "api_key is required"},
		{"speech model", "eleven_text_to_sound_v3", "eleven_multilingual_v2", "sound effects or music model"},
		{"non-audio provider", "elevenlabs/", "openai/", "requires an ElevenLabs model"},
		{"credential in base URL", "api_key:", "api_base: https://user:secret@example.com/v1\n      api_key:", "without credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(strings.Replace(model, tc.from, tc.to, 1)))
			if err == nil || !strings.Contains(err.Error(), tc.message) || strings.Contains(err.Error(), "user:secret") {
				t.Fatalf("validation error = %v, want %q", err, tc.message)
			}
		})
	}
}
