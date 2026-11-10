package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/elevenlabs"
)

func TestElevenLabsAudioGeneration(t *testing.T) {
	const audio = "\x00\xfftest-audio"
	var calls atomic.Int32
	f := newConfiguredMigrationFixture(t, nil, func(cfg *config.Config) {
		rate := 0.0025
		sound := elevenLabsDeployment(cfg, "game-sfx", "eleven_text_to_sound_v3")
		sound.AudioCostPerSecond = &rate
		cfg.ModelList = append(cfg.ModelList, sound, elevenLabsDeployment(cfg, "game-music", "music_v2"))
	}, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Xi-Api-Key") != "elevenlabs-server-key" ||
			r.Header.Get("Authorization") != "" || r.Header.Get("X-Private") != "" {
			t.Error("upstream request used unexpected credentials or client headers")
		}
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if fields["model"] != nil || fields["session_id"] != nil {
			t.Errorf("proxy fields forwarded: %v", fields)
		}
		switch r.URL.Path {
		case "/v1/sound-generation":
			if fields["model_id"] != "eleven_text_to_sound_v3" || fields["text"] != "looping rain" ||
				fields["duration_seconds"] != 2.5 || fields["loop"] != true || r.URL.RawQuery != "output_format=mp3_44100_128" {
				t.Errorf("sound request: %s %v", r.URL, fields)
			}
			w.Header().Set("character-cost", "100")
		case "/v1/music":
			if fields["model_id"] != "music_v2" || fields["prompt"] != "quiet strings" ||
				fields["music_length_ms"] != float64(5000) || fields["force_instrumental"] != true {
				t.Errorf("music request: %v", fields)
			}
			w.Header().Set("song-id", "song_fixture")
		default:
			t.Errorf("unexpected upstream route %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Set-Cookie", "private=hidden")
		fmt.Fprint(w, audio)
	})

	team := f.create(t, "/admin/teams", `{"name":"game-audio"}`)
	app := f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"game-audio","team_id":%.0f}`, team["id"]))
	keyPath := fmt.Sprintf("/admin/applications/%.0f/keys", app["id"])
	soundKey := f.create(t, keyPath, `{"name":"sound-run","allowed_models":["game-sfx"],"max_budget":0.006}`)
	musicKey := f.create(t, keyPath, `{"name":"music-run","allowed_models":["game-music"]}`)
	soundToken, musicToken := soundKey["key"].(string), musicKey["key"].(string)
	const soundRequest = `{"model":"game-sfx","text":"looping rain","duration_seconds":2.5,"loop":true,"session_id":"agent-run"}`
	const musicRequest = `{"model":"game-music","prompt":"quiet strings","music_length_ms":5000,"force_instrumental":true}`

	for _, tc := range []struct {
		path, input, key string
		status           int
	}{
		{"/v1/music", musicRequest, "", 401},
		{"/v1/music", musicRequest, soundToken, 403},
		{"/v1/music", strings.Replace(musicRequest, "game-music", "game-sfx", 1), soundToken, 503},
		{"/v1/music", strings.Replace(musicRequest, "game-music", "openai", 1), "master-only", 503},
		{"/v1/chat/completions", `{"model":"game-sfx","messages":[{"role":"user","content":"hi"}]}`, soundToken, 503},
		{"/v1/sound-generation", `{"model":"game-sfx","text":"rain"}`, soundToken, 400},
		{"/v1/sound-generation", `{"model":"game-sfx","text":"rain","duration_seconds":2,"model_id":"other"}`, soundToken, 400},
		{"/v1/music", `{"model":"game-music","prompt":"strings","music_length_ms":600001}`, musicToken, 400},
		{"/v1/music?token=bad", musicRequest, musicToken, 400},
	} {
		status, _, body := f.request(t, http.MethodPost, tc.path, tc.input, tc.key, false)
		if status != tc.status {
			t.Errorf("%s status = %d, want %d: %s", tc.path, status, tc.status, body)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected requests reached upstream")
	}

	for _, tc := range []struct {
		path, input, token, header, headerValue, estimate string
		duration                                          int64
		keyID                                             int64
	}{
		{"/v1/sound-generation?output_format=mp3_44100_128", soundRequest, soundToken, "character-cost", "100", "0.00625", 2500, int64(soundKey["id"].(float64))},
		{"/v1/music", musicRequest, musicToken, "song-id", "song_fixture", "", 5000, int64(musicKey["id"].(float64))},
	} {
		resp, body := requestMedia(t, f, tc.path, tc.input, "application/json", tc.token)
		if resp.StatusCode != 200 || string(body) != audio || resp.Header.Get("Content-Type") != "audio/mpeg" ||
			resp.Header.Get(tc.header) != tc.headerValue || resp.Header.Get("Set-Cookie") != "" {
			t.Fatalf("%s: status=%d headers=%v body=%q", tc.path, resp.StatusCode, resp.Header, body)
		}
		accountingID := resp.Header.Get("X-LLM-Accounting-ID")
		record := getAccounting(t, f, accountingID, tc.token)
		if record.State != "final" || len(record.Attempts) != 1 || record.Usage.TotalTokens != nil {
			t.Fatalf("unexpected accounting: %+v", record)
		}
		usage := record.Attempts[0].AudioGeneration
		if usage == nil || usage.RequestedDurationMS != tc.duration {
			t.Fatalf("unexpected audio evidence: %+v", usage)
		}
		if tc.estimate != "" {
			if usage.CharacterCost == nil || *usage.CharacterCost != 100 || record.Cost.Source != "estimate" ||
				record.Cost.Amount == nil || *record.Cost.Amount != tc.estimate || record.Cost.ReportedAmount != nil {
				t.Fatalf("unexpected sound accounting: %+v / %+v", record.Cost, usage)
			}
		} else if usage.SongID != tc.headerValue || record.Cost.Amount != nil || record.Coverage != "unknown" {
			t.Fatalf("unexpected music accounting: %+v / %+v", record.Cost, usage)
		}
		log := f.log(t, resp.Header.Get("X-Request-ID"))
		if log.APIKeyID == nil || *log.APIKeyID != tc.keyID || log.AccountingID != accountingID ||
			(tc.estimate != "" && log.SessionID != "agent-run") {
			t.Fatalf("wrong usage attribution: %+v", log)
		}
		if status, _, _ := f.request(t, http.MethodGet, "/v1/accounting/"+accountingID, "", otherAudioKey(tc.token, soundToken, musicToken), false); status != 404 {
			t.Fatalf("other key could read accounting: HTTP %d", status)
		}
	}
	status, _, _ := f.request(t, http.MethodPost, "/v1/sound-generation", soundRequest, soundToken, false)
	if status != 429 || calls.Load() != 2 {
		t.Fatalf("estimated spend should block the next request: status=%d calls=%d", status, calls.Load())
	}
}

func otherAudioKey(current, soundKey, musicKey string) string {
	if current == soundKey {
		return musicKey
	}
	return soundKey
}

func TestElevenLabsGenerationDoesNotReplay(t *testing.T) {
	for _, tc := range []struct {
		name           string
		pooled         bool
		budgetFailover bool
		upstreamStatus int
		status         int
	}{
		{"legacy upstream error", false, false, 503, 503},
		{"pooled upstream error", true, false, 503, 503},
		{"alternate pool upstream error", true, true, 503, 503},
		{"redirect", true, false, 307, 502},
		{"rate limit", true, false, 429, 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			f := newConfiguredMigrationFixture(t, nil, func(cfg *config.Config) {
				cfg.RouterSettings.NumRetries = 3
				cfg.RouterSettings.AllowedFails = 10
				rate := 0.01
				for _, model := range []string{"eleven_text_to_sound_v3", "eleven_text_to_sound_v2"} {
					deployment := elevenLabsDeployment(cfg, "game-sfx", model)
					deployment.AudioCostPerSecond = &rate
					cfg.ModelList = append(cfg.ModelList, deployment)
				}
				members := []config.PoolMember{{ModelName: "game-sfx"}}
				if tc.pooled {
					cfg.ProviderPools = []config.ProviderPool{{Name: "audio", Members: members}}
				}
				if tc.budgetFailover {
					cfg.ProviderPools = append(cfg.ProviderPools,
						config.ProviderPool{Name: "secondary", Members: members},
						config.ProviderPool{Name: "primary", Members: members, BudgetCapDaily: 0.005})
				}
			}, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 && tc.budgetFailover {
					w.Header().Set("Content-Type", "audio/mpeg")
					fmt.Fprint(w, "audio")
					return
				}
				w.Header().Set("Location", "/v1/redirect-target")
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(tc.upstreamStatus)
				fmt.Fprint(w, `{"error":{"message":"private upstream message"}}`)
			})
			const request = `{"model":"game-sfx","text":"rain","duration_seconds":1}`
			wantCalls := int32(1)
			if tc.budgetFailover {
				if status, _, body := f.request(t, http.MethodPost, "/v1/sound-generation", request, "master-only", false); status != 200 {
					t.Fatalf("could not prime primary pool spend: HTTP %d %s", status, body)
				}
				wantCalls++
			}
			status, headers, body := f.request(t, http.MethodPost, "/v1/sound-generation", request, "master-only", false)
			if status != tc.status || calls.Load() != wantCalls || strings.Contains(string(body), "private upstream message") {
				t.Fatalf("status=%d upstream calls=%d body=%s", status, calls.Load(), body)
			}
			if tc.upstreamStatus == 429 && headers.Get("Retry-After") != "7" {
				t.Errorf("Retry-After = %q", headers.Get("Retry-After"))
			}
			rec := getAccounting(t, f, headers.Get("X-LLM-Accounting-ID"), "master-only")
			if len(rec.Attempts) != 1 || rec.Attempts[0].Status != "failed" ||
				rec.Attempts[0].AudioGeneration == nil || rec.Cost.Amount != nil {
				t.Fatalf("unexpected failure accounting: %+v", rec)
			}
			if tc.budgetFailover && rec.Attempts[0].Pool == "primary" {
				t.Fatal("request used the exhausted primary pool")
			}
		})
	}
}

func elevenLabsDeployment(cfg *config.Config, alias, model string) config.ModelConfig {
	return config.ModelConfig{ModelName: alias, LiteLLMParams: config.LiteLLMParams{
		Model: "elevenlabs/" + model, APIKey: "elevenlabs-server-key", APIBase: cfg.ModelList[0].LiteLLMParams.APIBase,
	}}
}
