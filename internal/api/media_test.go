package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
)

func TestOpenAIMediaHTTP(t *testing.T) {
	const imageEvent = "event: image_generation.completed\ndata: {\"type\":\"image_generation.completed\",\"b64_json\":\"eA==\",\"usage\":{\"input_tokens\":5,\"output_tokens\":10,\"total_tokens\":15}}\n\n"
	for _, tc := range []struct {
		name, path, input, requestType, output, responseType string
		status                                               int
		multipart                                            bool
	}{
		{"images", "/v1/images/generations", `{"model":"openai","prompt":"draw","complexity":"low"}`, "application/json", `{"data":[{"b64_json":"eA=="}]}`, "application/json", 200, false},
		{"image stream", "/v1/images/generations", `{"model":"openai","prompt":"draw","stream":true}`, "application/json", imageEvent, "text/event-stream", 200, false},
		{"edit via URL", "/v1/images/edits", `{"model":"openai","images":[{"image_url":"https://example.org/p.png"}],"prompt":"edit"}`, "application/json", `{"data":[{"b64_json":"eA=="}]}`, "application/json", 200, false},
		{"edit via upload", "/v1/images/edits", "", "", `{"data":[{"b64_json":"eA=="}]}`, "application/json", 200, true},
		{"speech bytes", "/v1/audio/speech", `{"model":"openai","input":"hi","voice":"coral"}`, "application/json", "\x00\xffWAV", "audio/wav", 200, false},
		{"transcription", "/v1/audio/transcriptions", "", "", "transcript", "text/plain", 200, true},
		{"file translation", "/v1/audio/translations", "", "", `{"text":"hello"}`, "application/json", 200, true},
		{"WebRTC secret", "/v1/realtime/client_secrets", `{"session":{"type":"realtime","model":"openai","audio":{"output":{"voice":"marin"}}}}`, "application/json", `{"value":"ek_fixture"}`, "application/json", 200, false},
		{"WebRTC call", "/v1/realtime/calls", "", "", "v=0\r\na=answer\r\n", "application/sdp", 201, true},
		{"upstream error", "/v1/audio/speech", `{"model":"openai","input":"hi"}`, "application/json", `{"error":{"message":"invalid voice","code":"bad_voice"}}`, "application/json", 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer upstream-only" ||
					r.Header.Get("OpenAI-Safety-Identifier") != "test-user" || r.Header.Get("X-Private") != "" {
					t.Errorf("unexpected upstream request %s %s %v", r.Method, r.URL.Path, r.Header)
				}
				var alias string
				if tc.multipart {
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					defer r.MultipartForm.RemoveAll()
					if tc.path == "/v1/realtime/calls" {
						var session map[string]any
						_ = json.Unmarshal([]byte(r.FormValue("session")), &session)
						alias, _ = session["model"].(string)
						if r.FormValue("sdp") != "v=0\r\na=offer\r\n" || session["type"] != "realtime" {
							t.Errorf("WebRTC form altered: %v", r.MultipartForm.Value)
						}
						w.Header().Set("Location", "/v1/realtime/calls/rtc_fixture")
					} else {
						alias = r.FormValue("model")
						if r.FormValue("language") != "en" {
							t.Error("lost multipart language")
						}
						fileField := "file"
						if tc.path == "/v1/images/edits" {
							fileField = "image[]"
						}
						files := r.MultipartForm.File[fileField]
						if len(files) != 2 {
							t.Errorf("lost repeated files: %v", files)
						}
						for i, file := range files {
							fileReader, err := file.Open()
							if err != nil {
								t.Error(err)
								continue
							}
							data, err := io.ReadAll(fileReader)
							fileReader.Close()
							if err != nil || string(data) != fmt.Sprintf("sample-%d", i) ||
								file.Filename != fmt.Sprintf("sample-%d.dat", i) || file.Header.Get("Content-Type") != "application/octet-stream" {
								t.Errorf("file not preserved: %v %q %v", file, data, err)
							}
						}
					}
				} else {
					var upstream map[string]any
					if err := json.NewDecoder(r.Body).Decode(&upstream); err != nil {
						t.Error(err)
					}
					var want map[string]any
					_ = json.Unmarshal([]byte(tc.input), &want)
					if tc.path == "/v1/realtime/client_secrets" {
						session := upstream["session"].(map[string]any)
						alias, _ = session["model"].(string)
						want["session"].(map[string]any)["model"] = "wire-openai"
					} else {
						alias, _ = upstream["model"].(string)
						want["model"] = "wire-openai"
					}
					gotJSON, _ := json.Marshal(upstream)
					wantJSON, _ := json.Marshal(want)
					if !bytes.Equal(gotJSON, wantJSON) {
						t.Errorf("JSON changed beyond alias: %s, want %s", gotJSON, wantJSON)
					}
				}
				if alias != "wire-openai" {
					t.Errorf("upstream model = %q", alias)
				}
				w.Header().Set("Content-Type", tc.responseType)
				w.Header().Set("Set-Cookie", "upstream-secret=hidden")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.output)
			})
			input, contentType := tc.input, tc.requestType
			if tc.multipart {
				input, contentType = mediaMultipart(t, tc.path, "openai", "")
			}
			resp, body := requestMedia(t, f, tc.path, input, contentType, "master-only")
			if resp.StatusCode != tc.status || string(body) != tc.output || resp.Header.Get("Content-Type") != tc.responseType ||
				resp.Header.Get("Set-Cookie") != "" || resp.Header.Get("X-LLM-Accounting-ID") == "" {
				t.Fatalf("response status=%d headers=%v body=%q", resp.StatusCode, resp.Header, body)
			}
			if tc.path == "/v1/realtime/calls" && resp.Header.Get("Location") != "/v1/realtime/calls/rtc_fixture" {
				t.Error("missing WebRTC call Location")
			}
			if tc.name == "image stream" {
				log := f.log(t, resp.Header.Get("X-Request-ID"))
				if !log.IsStreaming || !log.UsageKnown || log.InputTokens != 5 || log.OutputTokens != 10 {
					t.Errorf("stream accounting: %+v", log)
				}
			}
		})
	}
}

func TestOpenAIMediaUploadFailoverAndAuthorization(t *testing.T) {
	const fileSize = 2 << 20
	var attempts atomic.Int32
	f := newConfiguredMigrationFixture(t, nil, func(cfg *config.Config) {
		cfg.ModelList = append(cfg.ModelList, cfg.ModelList[0])
		cfg.ModelList[len(cfg.ModelList)-1].LiteLLMParams.Model = "openai/wire-alternate"
	}, func(w http.ResponseWriter, r *http.Request) {
		parts, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for {
			part, err := parts.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				return
			}
			if part.FormName() == "image[]" {
				if n, err := io.Copy(io.Discard, part); err != nil || n != fileSize {
					t.Errorf("upload length = %d, err = %v", n, err)
				}
			}
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[]}`)
	})
	team := f.create(t, "/admin/teams", `{"name":"media"}`)
	app := f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"media","team_id":%.0f}`, team["id"]))
	keyPath := fmt.Sprintf("/admin/applications/%.0f/keys", app["id"])
	allowed := f.create(t, keyPath, `{"name":"allowed","allowed_models":["openai"]}`)["key"].(string)
	blocked := f.create(t, keyPath, `{"name":"blocked","allowed_models":["openrouter"]}`)["key"].(string)
	for _, tc := range []struct {
		key, alias string
		status     int
	}{
		{"", "openai", 401}, {blocked, "openai", 403}, {"master-only", "missing", 404},
		{"master-only", "anthropic", 503},
	} {
		status, _, body := f.request(t, http.MethodPost, "/v1/images/generations", fmt.Sprintf(`{"model":%q}`, tc.alias), tc.key, false)
		if status != tc.status {
			t.Errorf("model %s status = %d, want %d: %s", tc.alias, status, tc.status, body)
		}
	}
	status, _, _ := f.request(t, http.MethodPost, "/v1/realtime/client_secrets", `{"session":{"type":"realtime","model":"openai"}}`, allowed, false)
	if status != 403 || attempts.Load() != 0 {
		t.Fatal("disallowed request reached upstream")
	}
	input, contentType := mediaMultipart(t, "/v1/images/edits", "openai", strings.Repeat("x", fileSize))
	resp, body := requestMedia(t, f, "/v1/images/edits", input, contentType, allowed)
	if resp.StatusCode != 200 || attempts.Load() != 2 {
		t.Fatalf("upload retry: status=%d attempts=%d body=%s", resp.StatusCode, attempts.Load(), body)
	}
	if row := f.log(t, resp.Header.Get("X-Request-ID")); row.APIKeyID == nil {
		t.Error("upload was not attributed to its app key")
	}
}

func mediaMultipart(t *testing.T, path, alias, largeFile string) (string, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if path == "/v1/realtime/calls" {
		_ = form.WriteField("session", fmt.Sprintf(`{"type":"realtime","model":%q}`, alias))
		_ = form.WriteField("sdp", "v=0\r\na=offer\r\n")
	} else {
		_ = form.WriteField("model", alias)
		_ = form.WriteField("language", "en")
		name := "file"
		if path == "/v1/images/edits" {
			name = "image[]"
		}
		for i, sample := range []string{"sample-0", "sample-1"} {
			if largeFile != "" {
				sample = largeFile
			}
			header := make(textproto.MIMEHeader)
			header.Set("Content-Type", "application/octet-stream")
			header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename="sample-%d.dat"`, name, i))
			part, err := form.CreatePart(header)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(part, sample); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	return body.String(), form.FormDataContentType()
}

func requestMedia(t *testing.T, f *migrationFixture, path, input, contentType, key string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.url+path, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("OpenAI-Safety-Identifier", "test-user")
	req.Header.Set("X-Private", "never forward")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}
