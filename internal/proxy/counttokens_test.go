package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func TestCountTokensEstimatesForOpenAIProviders(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{
		"ds": {Name: "ds", Format: "openai", Auth: "key", BaseURL: "https://example.invalid", APIKeyEnv: "K"},
	}}
	t.Setenv("K", "x")
	px := New(func() *config.Config { return cfg }, &nopRecorder{})

	body := `{"model":"ds:chat","messages":[{"role":"user","content":"` + strings.Repeat("a", 400) + `"}]}`
	w := httptest.NewRecorder()
	px.CountTokens(w, httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(body)))

	if w.Code != 200 {
		t.Fatalf("Status = %d, Body = %s", w.Code, w.Body.String())
	}
	var v struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.InputTokens < 100 {
		t.Errorf("input_tokens = %d, 400 chars should yield about 100 tokens", v.InputTokens)
	}
}

func TestCountTokensNeverFailsOnUnknownAlias(t *testing.T) {
	px := New(func() *config.Config {
		return &config.Config{Providers: map[string]config.Provider{}}
	}, &nopRecorder{})
	w := httptest.NewRecorder()
	px.CountTokens(w, httptest.NewRequest("POST", "/v1/messages/count_tokens",
		strings.NewReader(`{"model":"neither:nor","messages":[{"role":"user","content":"hi"}]}`)))
	if w.Code != 200 {
		t.Fatalf("count_tokens must never fail, Claude Code aborts otherwise. Status = %d", w.Code)
	}
}

func TestCountTokensFallsBackWhenUpstreamFails(t *testing.T) {
	for _, status := range []int{429, 500, 401} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, `{"type":"error","error":{"message":"nein"}}`)
		}))
		cfg := &config.Config{Providers: map[string]config.Provider{
			"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
		}}
		px := New(func() *config.Config { return cfg }, &nopRecorder{})

		w := httptest.NewRecorder()
		px.CountTokens(w, httptest.NewRequest("POST", "/v1/messages/count_tokens",
			strings.NewReader(`{"model":"sub:opus-5","messages":[{"role":"user","content":"hello"}]}`)))
		up.Close()

		if w.Code != 200 {
			t.Fatalf("provider answered %d, count_tokens must still return 200, got %d", status, w.Code)
		}
		var v struct {
			InputTokens int `json:"input_tokens"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || v.InputTokens <= 0 {
			t.Errorf("no usable estimate: %s", w.Body.String())
		}
	}
}

func TestCountTokensPassesThroughSuccess(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"input_tokens":4242}`)
	}))
	defer up.Close()
	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})

	w := httptest.NewRecorder()
	px.CountTokens(w, httptest.NewRequest("POST", "/v1/messages/count_tokens",
		strings.NewReader(`{"model":"sub:opus-5","messages":[]}`)))

	if !strings.Contains(w.Body.String(), "4242") {
		t.Errorf("the provider's real answer must be passed through: %s", w.Body.String())
	}
}

func TestCountTokensBrokenJSONStillEstimates(t *testing.T) {
	px := New(func() *config.Config { return &config.Config{Providers: map[string]config.Provider{}} }, &nopRecorder{})
	w := httptest.NewRecorder()
	px.CountTokens(w, httptest.NewRequest("POST", "/v1/messages/count_tokens",
		strings.NewReader(`{kaputt`)))
	if w.Code != 200 {
		t.Fatalf("Status = %d, want 200", w.Code)
	}
}

func TestCountTokensMultibyteIsNotUnderestimated(t *testing.T) {
	px := New(func() *config.Config { return &config.Config{Providers: map[string]config.Provider{}} }, &nopRecorder{})
	w := httptest.NewRecorder()
	px.CountTokens(w, httptest.NewRequest("POST", "/v1/messages/count_tokens",
		strings.NewReader(`{"model":"x:y","messages":[{"role":"user","content":"`+strings.Repeat("漢", 200)+`"}]}`)))

	var v struct {
		InputTokens int `json:"input_tokens"`
	}
	json.Unmarshal(w.Body.Bytes(), &v)
	if v.InputTokens < 200 {
		t.Errorf("input_tokens = %d, 200 CJK chars must not be estimated below 200 tokens", v.InputTokens)
	}
}
