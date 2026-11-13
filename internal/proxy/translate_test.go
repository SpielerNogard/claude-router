package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func TestStreamProviderErrorBecomesVisibleErrorEvent(t *testing.T) {
	sse := "data: {\"id\":\"c\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"teil\"}}]}\n\n" +
		"data: {\"error\":{\"message\":\"rate limited, key sk-live-abcdefghijklmnop\"}}\n\n" +
		"data: [DONE]\n\n"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, sse)
	}))
	defer up.Close()
	t.Setenv("UP_KEY", "sk")

	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "UP_KEY"},
	}}
	rec := &nopRecorder{}
	px := New(func() *config.Config { return cfg }, rec)

	w := httptest.NewRecorder()
	px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"up:m","stream":true,"max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`)))

	body := w.Body.String()
	errPos := strings.Index(body, "event: error")
	if errPos < 0 {
		t.Fatalf("expected an event: error in the stream, got: %s", body)
	}
	if !strings.Contains(body, `"type":"error"`) {
		t.Errorf("error event must have Anthropic shape: %s", body)
	}
	if strings.Contains(body, "sk-live-abcdefghijklmnop") {
		t.Error("the key in the error message should have been redacted")
	}
	if stopPos := strings.Index(body, "event: message_stop"); stopPos < errPos {
		t.Error("event: error must come before message_stop")
	}
	if rec.last.Status != "partial" {
		t.Errorf("Status = %q, want partial", rec.last.Status)
	}
}
