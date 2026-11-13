package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/store"
)

func TestTranslatedDocumentsLandInTheLog(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"hallo"},
		  "finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer up.Close()
	t.Setenv("UP_KEY", "sk")

	l, err := store.OpenLog(filepath.Join(t.TempDir(), "log.db"), store.StaticLimits("full", 7, 500, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "UP_KEY"},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	px.SetMessageLog(l)

	w := httptest.NewRecorder()
	px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"up:m","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`)))
	px.WaitForLogs()

	entries, err := l.List(10, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %v (%v)", entries, err)
	}
	docs, err := l.Get(entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"in", "translated", "upstream", "out"} {
		if len(docs[kind]) == 0 {
			t.Errorf("document %q missing: the converter cannot be debugged without all four", kind)
		}
	}
}

func TestPassthroughLogsTwoDocuments(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","content":[],
		  "usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer up.Close()

	l, _ := store.OpenLog(filepath.Join(t.TempDir(), "log.db"), store.StaticLimits("full", 7, 500, 256))
	defer l.Close()
	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	px.SetMessageLog(l)

	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`)))
	px.WaitForLogs()

	entries, _ := l.List(10, 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	docs, _ := l.Get(entries[0].ID)
	if len(docs) != 2 {
		t.Errorf("passthrough does not translate: expected 2 documents, got %d", len(docs))
	}
}

func TestLogMetaWritesEntryWithoutDocuments(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"hallo"},
		  "finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer up.Close()
	t.Setenv("UP_KEY", "sk")

	l, err := store.OpenLog(filepath.Join(t.TempDir(), "log.db"), store.StaticLimits("meta", 7, 500, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "UP_KEY"},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	px.SetMessageLog(l)

	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"up:m","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`)))
	px.WaitForLogs()

	entries, err := l.List(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("level meta must create an entry, has %d", len(entries))
	}
	docs, _ := l.Get(entries[0].ID)
	if len(docs) != 0 {
		t.Errorf("level meta must not store content, has %d documents", len(docs))
	}
}

func TestStreamingDocumentsAreCaptured(t *testing.T) {
	sse := "data: {\"id\":\"c3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"id\":\"c3\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"c3\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n" +
		"data: [DONE]\n\n"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, sse)
	}))
	defer up.Close()
	t.Setenv("UP_KEY", "sk")

	l, err := store.OpenLog(filepath.Join(t.TempDir(), "log.db"), store.StaticLimits("full", 7, 500, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "UP_KEY"},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	px.SetMessageLog(l)

	w := httptest.NewRecorder()
	px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"up:m","max_tokens":5,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	px.WaitForLogs()

	entries, err := l.List(10, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %v (%v)", entries, err)
	}
	docs, err := l.Get(entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(docs["upstream"]), "Hello") {
		t.Errorf("upstream document missing or incomplete: %q", docs["upstream"])
	}
	for _, name := range []string{"message_start", "content_block_delta", "message_stop"} {
		if !strings.Contains(string(docs["out"]), name) {
			t.Errorf("out document has no %q event: %q", name, docs["out"])
		}
	}
}

func TestRequestIDIsUniquePerRequestAndLinksLogToRecord(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"hallo"},
		  "finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer up.Close()
	t.Setenv("UP_KEY", "sk")

	l, err := store.OpenLog(filepath.Join(t.TempDir(), "log.db"), store.StaticLimits("meta", 7, 500, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "UP_KEY"},
	}}
	rec := &nopRecorder{}
	px := New(func() *config.Config { return cfg }, rec)
	px.SetMessageLog(l)

	body := `{"model":"up:m","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`
	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
	px.WaitForLogs()
	firstID := rec.last.RequestID
	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
	px.WaitForLogs()
	secondID := rec.last.RequestID

	if firstID == "" || firstID == "up:m" {
		t.Fatalf("request_id must be a real ID, not the alias: %q", firstID)
	}
	if firstID == secondID {
		t.Error("two requests to the same model must get different request_ids")
	}

	entries, err := l.List(10, 0)
	if err != nil || len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %v (%v)", entries, err)
	}
	inLog := map[string]bool{entries[0].RequestID: true, entries[1].RequestID: true}
	if !inLog[firstID] || !inLog[secondID] {
		t.Errorf("log entries %+v must carry the same IDs as the cost rows (%q, %q)",
			entries, firstID, secondID)
	}
}

func TestLogOffMeansNoWrites(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"content":"x"},"finish_reason":"stop"}]}`)
	}))
	defer up.Close()
	t.Setenv("UP_KEY", "sk")

	l, _ := store.OpenLog(filepath.Join(t.TempDir(), "log.db"), store.StaticLimits("off", 7, 500, 256))
	defer l.Close()
	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "UP_KEY"},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	px.SetMessageLog(l)

	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"up:m","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`)))
	px.WaitForLogs()

	if entries, _ := l.List(10, 0); len(entries) != 0 {
		t.Fatalf("level=off must write nothing, has %d entries", len(entries))
	}
}
