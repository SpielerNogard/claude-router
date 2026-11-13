package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithDebugPassesThroughUnchanged(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(418)
		w.Write([]byte("teapot"))
	})
	for _, enabled := range []bool{false, true} {
		w := httptest.NewRecorder()
		withDebug(enabled, inner).ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
		if w.Code != 418 || w.Body.String() != "teapot" {
			t.Errorf("debug=%v alters the response: %d %q", enabled, w.Code, w.Body.String())
		}
	}
}

func TestStatusWriterRemembersFirstCode(t *testing.T) {
	sw := &statusWriter{ResponseWriter: httptest.NewRecorder()}
	sw.WriteHeader(404)
	sw.WriteHeader(500)
	if sw.status != 404 {
		t.Errorf("status = %d, want 404 — only the first call counts", sw.status)
	}
}

func TestStatusWriterDefaultsTo200OnWrite(t *testing.T) {
	sw := &statusWriter{ResponseWriter: httptest.NewRecorder()}
	sw.Write([]byte("x"))
	if sw.status != 200 {
		t.Errorf("status = %d, want 200", sw.status)
	}
}

func TestStatusWriterKeepsFlusher(t *testing.T) {
	// Streaming depends on it: without Flush, SSE events never reach the client.
	var sw http.ResponseWriter = &statusWriter{ResponseWriter: httptest.NewRecorder()}
	if _, ok := sw.(http.Flusher); !ok {
		t.Fatal("statusWriter must implement http.Flusher")
	}
}
