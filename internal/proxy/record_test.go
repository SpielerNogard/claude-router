package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/store"
)

type failingStore struct{ calls atomic.Int64 }

func (f *failingStore) Insert(store.Record) error {
	f.calls.Add(1)
	return io.ErrClosedPipe
}

func TestRecorderNeverBlocksRequest(t *testing.T) {
	r := NewAsyncRecorderWith(&failingStore{}, 1)
	defer r.Close()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			r.Record(store.Record{Alias: "a:b", Status: "ok"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Record blocked: a full buffer must drop, not wait")
	}
}

func TestRecordDuringCloseDoesNotPanic(t *testing.T) {
	r := NewAsyncRecorderWith(&failingStore{}, 4)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				r.Record(store.Record{Alias: "a:b", Status: "ok"})
			}
		}()
	}
	time.Sleep(5 * time.Millisecond)
	r.Close()
	wg.Wait() // a panic in any goroutine fails the test
}

func TestStoreFailureDoesNotBreakRequest(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},
		  "finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer up.Close()
	t.Setenv("UP_KEY", "sk")

	fs := &failingStore{}
	rec := NewAsyncRecorderWith(fs, 16)
	defer rec.Close()
	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "UP_KEY"},
	}}
	px := New(func() *config.Config { return cfg }, rec)

	w := httptest.NewRecorder()
	px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"up:m","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`)))
	px.WaitForLogs()

	if w.Code != 200 {
		t.Fatalf("a broken store must not affect the request, Status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"type":"message"`) {
		t.Errorf("response incomplete: %s", w.Body.String())
	}
}

func TestClientDisconnectMidStreamIsRecordedAsPartial(t *testing.T) {
	block := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		io.WriteString(w, "data: {\"id\":\"c\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"teil\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-block // provider never finishes sending
	}))
	defer up.Close()
	defer close(block)
	t.Setenv("UP_KEY", "sk")

	rec := &nopRecorder{}
	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "UP_KEY"},
	}}
	px := New(func() *config.Config { return cfg }, rec)

	req := httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"up:m","stream":true,"max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`))
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		px.Messages(httptest.NewRecorder(), req)
		px.WaitForLogs()
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel() // client hangs up

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler hangs after client abort: the upstream connection is not closed")
	}
	if rec.last.Status != "partial" {
		t.Errorf("Status = %q, want partial", rec.last.Status)
	}
}
