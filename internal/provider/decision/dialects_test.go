package decision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// A 1x1 PNG, so sniffing bare base64 has real magic bytes to read.
const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// upstream records the last request and replies with a canned status and body.
type upstream struct {
	*httptest.Server
	path   string
	header http.Header
	body   map[string]any
	status int
	reply  string
}

func newUpstream(t *testing.T, reply string) *upstream {
	t.Helper()
	u := &upstream{status: 200, reply: reply}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.path, u.header, u.body = r.URL.Path, r.Header, nil
		if err := json.NewDecoder(r.Body).Decode(&u.body); err != nil {
			t.Error(err)
		}
		w.WriteHeader(u.status)
		fmt.Fprint(w, u.reply)
	}))
	t.Cleanup(u.Close)
	return u
}

func parse(t *testing.T, body, format string) *model.DecisionRequest {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatal(err)
	}
	r, err := model.ParseDecisionRequest(fields, format)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func dialect(t *testing.T, name, key, base string, cfg config.DecisionConfig) *Provider {
	t.Helper()
	cfg.Pricing = &config.DecisionPricing{}
	p, err := New(name, provider.ProviderOptions{APIKey: key, APIBase: base}, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func decide(t *testing.T, p *Provider, body, format string) (map[string]any, *model.Usage) {
	t.Helper()
	req := parse(t, body, format)
	req.Model = "upstream-model" // the handler rewrites the alias per attempt
	response, err := p.Decide(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(response.Fields)
	var out map[string]any
	json.Unmarshal(data, &out)
	return out, response.Usage
}

const geminiReply = `{"answers":{
	"q_0":{"type":"noul","noul":0.99,"confidence":0.98},
	"q_1":{"type":"choice","choice":"billing","probabilities":{"billing":0.9,"shipping":0.1},"confidence":0.7},
	"q_2":{"type":"score","score":1.2,"legend":{"0":"low","1":"mid","2":"high"},"probabilities":{"0":0.1,"1":0.6,"2":0.3},"confidence":0.4}},
	"usage":{"total_input_tokens":120,"input_tokens_by_modality":[{"modality":"text","tokens":100},{"modality":"image","tokens":20}],"total_output_tokens":0,"total_tokens":120}}`

func TestGeminiFromDecisionsFormat(t *testing.T) {
	u := newUpstream(t, geminiReply)
	p := dialect(t, "gemini", "google-key", u.URL+"/v1beta", config.DecisionConfig{Protocol: "gemini", Modalities: []string{"text", "image"}})
	out, usage := decide(t, p, `{"model":"alias","input":[
		{"role":"user","content":[{"type":"input_text","text":"Order"},{"type":"input_json","data":{"id": 7}},{"type":"input_image","image_url":"data:image/png;base64,`+pngBase64+`"},{"type":"input_image","image_url":"https://cdn.example.test/receipt.JPG?sig=1"}]},
		{"role":"user","content":"Charged twice."}],
		"questions":[
		{"name":"damaged","type":"predicate","instructions":"Damaged?"},
		{"name":"team","type":"choice","instructions":"Who?","choices":[{"value":"billing","description":"Money"},{"value":"shipping"}]},
		{"type":"score","instructions":"Severity?","levels":[{"label":"low"},{"label":"mid"},{"label":"high"}]}]}`, model.DecisionsFormat)

	if u.path != "/v1beta/decisions" || u.header.Get("x-goog-api-key") != "google-key" || u.header.Get("Authorization") != "" {
		t.Fatalf("wrong Gemini endpoint or credentials: %s %v", u.path, u.header)
	}
	want := `[{"text":"Order\n{\"id\":7}","type":"text"},{"data":"` + pngBase64 + `","mime_type":"image/png","type":"image"},{"mime_type":"image/jpeg","type":"image","uri":"https://cdn.example.test/receipt.JPG?sig=1"},{"text":"Charged twice.","type":"text"}]`
	if got, _ := json.Marshal(u.body["input"]); string(got) != want {
		t.Fatalf("input blocks lost order or type:\n got %s\nwant %s", got, want)
	}
	questions, _ := json.Marshal(u.body["questions"])
	if string(questions) != `{"q_0":{"instructions":"Damaged?","type":"noul"},"q_1":{"criteria":{"billing":"Money","shipping":""},"instructions":"Who?","type":"choice"},"q_2":{"criteria":["low","mid","high"],"instructions":"Severity?","type":"score"}}` || u.body["model"] != "upstream-model" {
		t.Fatalf("questions: %s", questions)
	}

	answers := out["answers"].([]any)
	first, second, third := answers[0].(map[string]any), answers[1].(map[string]any), answers[2].(map[string]any)
	if first["type"] != "predicate" || first["name"] != "damaged" || first["probability"] != 0.99 || first["confidence"] != 0.98 {
		t.Fatalf("predicate: %v", first)
	}
	if second["choice"] != "billing" || second["probabilities"].([]any)[1].(map[string]any)["value"] != "shipping" {
		t.Fatalf("choice: %v", second)
	}
	level := third["probabilities"].([]any)[2].(map[string]any)
	if third["score"] != 1.2 || third["name"] != nil || level["label"] != "high" || level["probability"] != 0.3 || third["legend"] != nil {
		t.Fatalf("score: %v", third)
	}
	// Gemini omits the model and names its counters differently; billing needs both.
	if out["model"] != "upstream-model" || usage == nil || usage.PromptTokens != 120 || usage.CompletionTokens != 0 || usage.TotalTokens != 120 {
		t.Fatalf("model=%v usage=%+v", out["model"], usage)
	}
	if out["usage"].(map[string]any)["input_tokens_by_modality"] == nil {
		t.Fatal("Google's own usage detail was dropped")
	}
}

func TestGeminiFromSystemOneFormat(t *testing.T) {
	u := newUpstream(t, `{"answers":{"ok":{"type":"noul","noul":0.2,"confidence":0.6},"team":{"type":"choice","choice":"a","probabilities":{"a":1,"b":0},"confidence":1}},"usage":{"total_input_tokens":9,"total_output_tokens":0,"total_tokens":9}}`)
	p := dialect(t, "gemini", "k", u.URL, config.DecisionConfig{Protocol: "gemini", Modalities: []string{"text", "image", "audio"}})
	wav := base64.StdEncoding.EncodeToString([]byte("RIFF\x24\x00\x00\x00WAVEfmt "))
	out, usage := decide(t, p, `{"model":"alias","state":{"ticket":"late"},"images":["`+pngBase64+`"],"audio":["`+wav+`"],
		"questions":{"ok":{"type":"noul","instructions":"Fine?","criteria":{"true":"all good","false":"not good"}},
		"team":{"type":"choice","instructions":"Who?","criteria":{"a":null,"b":"other"}}}}`, model.SystemOneFormat)

	got, _ := json.Marshal(u.body)
	want := `{"input":[{"text":"{\"ticket\":\"late\"}","type":"text"},{"data":"` + pngBase64 + `","mime_type":"image/png","type":"image"},{"data":"` + wav + `","mime_type":"audio/wav","type":"audio"}],"model":"upstream-model","questions":{"ok":{"criteria":{"false_description":"not good","true_description":"all good"},"instructions":"Fine?","type":"noul"},"team":{"criteria":{"a":"","b":"other"},"instructions":"Who?","type":"choice"}}}`
	if string(got) != want {
		t.Fatalf("native Gemini body:\n got %s\nwant %s", got, want)
	}
	// The caller asked in System One, so answers keep their names and shape.
	ok := out["answers"].(map[string]any)["ok"].(map[string]any)
	if ok["noul"] != 0.2 || ok["confidence"] != 0.6 || usage.PromptTokens != 9 || out["usage"].(map[string]any)["input_tokens"] != float64(9) {
		t.Fatalf("answers=%v usage=%+v", out, usage)
	}
}

func TestGeminiRejectsWhatItCannotCarry(t *testing.T) {
	cfg := &config.DecisionConfig{Protocol: "gemini", Modalities: []string{"text", "image", "audio", "video"}, QuestionTypes: []string{"predicate", "choice", "score"}}
	q := `"questions":[{"type":"predicate","instructions":"?"}]`
	for name, c := range map[string]struct{ body, format, want string }{
		"transcript":      {`{"model":"m","input":[{"role":"system","content":"be strict"},{"role":"user","content":"hi"}],` + q + `}`, model.DecisionsFormat, ""},
		"transcript+img":  {`{"model":"m","input":[{"role":"assistant","content":"hi"},{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,` + pngBase64 + `"}]}],` + q + `}`, model.DecisionsFormat, "multi-role"},
		"tool call":       {`{"model":"m","input":[{"type":"function_call","call_id":"1","name":"f","arguments":"{}"}],` + q + `}`, model.DecisionsFormat, "native Decisions transport"},
		"image detail":    {`{"model":"m","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://x.test/a.png","detail":"high"}]}],` + q + `}`, model.DecisionsFormat, "detail"},
		"untyped url":     {`{"model":"m","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://x.test/download?id=4"}]}],` + q + `}`, model.DecisionsFormat, "never fetched"},
		"plain http":      {`{"model":"m","input":[{"role":"user","content":[{"type":"input_image","image_url":"http://x.test/a.png"}]}],` + q + `}`, model.DecisionsFormat, "HTTPS"},
		"extension":       {`{"model":"m","input":"x","prompt_cache_key":"k",` + q + `}`, model.DecisionsFormat, "prompt_cache_key"},
		"no instructions": {`{"model":"m","input":"x","questions":[{"type":"choice","choices":[{"value":"a"},{"value":"b"}]}]}`, model.DecisionsFormat, "instructions"},
		"assets":          {`{"model":"m","state":"x","assets":{"p":{"type":"image","format":"png","data_base64":"` + pngBase64 + `"}},"questions":{"a":{"type":"noul","instructions":"?"}}}`, model.SystemOneFormat, "assets"},
		"question key":    {`{"model":"m","state":"x","questions":{"has space":{"type":"noul","instructions":"?"}}}`, model.SystemOneFormat, "not valid for Gemini"},
		"noul criteria":   {`{"model":"m","state":"x","questions":{"a":{"type":"noul","instructions":"?","criteria":{"maybe":"x"}}}}`, model.SystemOneFormat, "true"},
		"wrong media":     {`{"model":"m","state":"x","videos":["` + pngBase64 + `"],"questions":{"a":{"type":"noul","instructions":"?"}}}`, model.SystemOneFormat, "image/png"},
	} {
		wire, err := Prepare(parse(t, c.body, c.format), cfg, "m")
		if c.want == "" {
			var input string
			if err != nil || json.Unmarshal(wire["input"], &input) != nil || input != `[{"content":"be strict","role":"system"},{"content":"hi","role":"user"}]` {
				t.Errorf("%s: transcript should become JSON text, got %s %v", name, wire["input"], err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", name, c.want, err)
		}
	}
}

func TestOllamaDialect(t *testing.T) {
	u := newUpstream(t, `{"model":"clef","answers":{"q_0":{"type":"noul","noul":0.8}},"usage":{"input_tokens":174,"output_tokens":1}}`)
	p := dialect(t, "ollama", "", u.URL+"/v1", config.DecisionConfig{Protocol: "ollama", Modalities: []string{"text", "image"}, ExtraBody: map[string]any{"keep_alive": "30m"}})
	out, usage := decide(t, p, `{"model":"alias","input":[{"role":"user","content":[{"type":"input_text","text":"Receipt"},{"type":"input_image","image_url":"data:image/png;base64,`+pngBase64+`"}]}],"questions":[{"name":"paid","type":"predicate","instructions":"Paid?"}]}`, model.DecisionsFormat)
	images, _ := json.Marshal(u.body["images"])
	if u.path != "/v1/systemone" || u.header.Get("Authorization") != "" || string(images) != `["`+pngBase64+`"]` || u.body["keep_alive"] != "30m" {
		t.Fatalf("Ollama request: %s auth=%q images=%s body=%v", u.path, u.header.Get("Authorization"), images, u.body)
	}
	answer := out["answers"].([]any)[0].(map[string]any)
	if answer["probability"] != 0.8 || answer["name"] != "paid" || usage.PromptTokens != 174 || usage.CompletionTokens != 1 {
		t.Fatalf("answer=%v usage=%+v", answer, usage)
	}

	// Native System One callers may send either encoding; a client keep_alive
	// cannot displace the deployment's.
	decide(t, p, `{"model":"alias","state":"x","keep_alive":0,"images":["data:image/jpeg;base64,AAAA","BBBB"],"questions":{"q_0":{"type":"noul","instructions":"?"}}}`, model.SystemOneFormat)
	images, _ = json.Marshal(u.body["images"])
	if string(images) != `["AAAA","BBBB"]` || u.body["keep_alive"] != "30m" {
		t.Fatalf("native Ollama body: %v", u.body)
	}

	cfg := &config.DecisionConfig{Protocol: "ollama", Modalities: []string{"text", "image", "audio", "video"}, QuestionTypes: []string{"predicate"}}
	for field, value := range map[string]string{"images": `["https://x.test/a.png"]`, "videos": `["data:video/mp4;base64,AAAA"]`, "assets": `{"p":{"type":"image"}}`} {
		if _, err := Prepare(parse(t, `{"model":"m","state":"x","`+field+`":`+value+`,"questions":{"a":{"type":"noul","instructions":"?"}}}`, model.SystemOneFormat), cfg, "m"); err == nil {
			t.Errorf("Ollama accepted %s=%s", field, value)
		}
	}

	// Ollama reports errors as a bare string; clients expect an error object.
	u.status, u.reply = 404, `{"error":"model \"clef\" not found, try pulling it first"}`
	_, err := p.Decide(context.Background(), parse(t, `{"model":"m","state":"x","questions":{"a":{"type":"noul","instructions":"?"}}}`, model.SystemOneFormat))
	var httpErr *provider.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 404 || !strings.Contains(err.Error(), "try pulling it first") || !strings.Contains(string(httpErr.Body), `{"error":{"message":`) {
		t.Fatalf("Ollama error shape: %v", err)
	}
}

func TestGraniteSwitchPlugin(t *testing.T) {
	// The vLLM plugin adds per-question diagnostics and takes a cache_salt.
	u := newUpstream(t, `{"model":"granite-switch","answers":{"queue":{"type":"choice","choice":"payments","probabilities":{"payments":0.9,"account":0.1},"confidence":0.5,"diagnostics":{"label_mass":0.97,"argmax_is_label":true}}},"usage":{"input_tokens":61,"output_tokens":1}}`)
	p := dialect(t, "vllm", "", u.URL+"/v1", config.DecisionConfig{Protocol: "systemone", ExtraBody: map[string]any{"cache_salt": "tenant-a"}})
	body := `{"model":"alias","state":{"ticket":"Payouts failed"},"questions":{"queue":{"type":"choice","instructions":"Which team?","criteria":{"payments":"Payout failures","account":"Login issues"}}}}`
	out, _ := decide(t, p, body, model.SystemOneFormat)
	diagnostics := out["answers"].(map[string]any)["queue"].(map[string]any)["diagnostics"].(map[string]any)
	if u.path != "/v1/systemone" || u.body["cache_salt"] != "tenant-a" || diagnostics["label_mass"] != 0.97 {
		t.Fatalf("Granite request=%v diagnostics=%v", u.body, diagnostics)
	}

	// jev-style validation errors have no "error" member; the detail must survive.
	u.status, u.reply = 422, `{"detail":[{"loc":["body","images"],"msg":"images are not supported","type":"value_error"}]}`
	_, err := p.Decide(context.Background(), parse(t, body, model.SystemOneFormat))
	if !provider.IsInvalidRequest(err) || !strings.Contains(err.Error(), "images are not supported") {
		t.Fatalf("validation error lost: %v", err)
	}
}

func TestDialectDefaults(t *testing.T) {
	for name, want := range map[string]string{"gemini": "https://generativelanguage.googleapis.com/v1beta/decisions", "ollama": "http://localhost:11434/v1/systemone"} {
		if p := dialect(t, name, "", "", config.DecisionConfig{Protocol: name}); p.url != want {
			t.Errorf("%s default URL = %s, want %s", name, p.url, want)
		}
	}
}
