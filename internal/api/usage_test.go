package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/store"
)

func TestUsageDefaultsToLastThirtyDays(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cost := 1.5
	s.Insert(store.Record{TS: time.Now().UTC(), Alias: "ds:chat", Provider: "ds",
		AuthMode: "key", CostUSD: &cost, InputTokens: 10, Status: "ok"})

	w := httptest.NewRecorder()
	Usage(s)(w, httptest.NewRequest("GET", "/api/usage", nil))

	if w.Code != 200 {
		t.Fatalf("Status = %d, Body = %s", w.Code, w.Body.String())
	}
	var u store.Usage
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	if len(u.Buckets) < 28 {
		t.Errorf("without parameters 30 days are expected, got %d buckets", len(u.Buckets))
	}
	if len(u.Series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(u.Series))
	}
}

func TestUsageRejectsBadParams(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "m.db"))
	defer s.Close()
	w := httptest.NewRecorder()
	Usage(s)(w, httptest.NewRequest("GET", "/api/usage?bucket=fortnight", nil))
	if w.Code != 400 {
		t.Errorf("Status = %d, want 400", w.Code)
	}
}

func TestUsageEmptyResultIsArrayNotNull(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "m.db"))
	defer s.Close()
	w := httptest.NewRecorder()
	Usage(s)(w, httptest.NewRequest("GET", "/api/usage", nil))
	if got := w.Body.String(); !strings.Contains(got, `"series":[]`) {
		t.Errorf("empty result must be [], not null: %s", got)
	}
}

func TestUsageToDateIncludesTheWholeLastDay(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cost := 2.0
	// Shortly before midnight on the last day of the range.
	ts := time.Date(2026, 9, 24, 23, 30, 0, 0, time.UTC)
	if err := s.Insert(store.Record{TS: ts, Alias: "ds:chat", Provider: "ds",
		AuthMode: "key", CostUSD: &cost, InputTokens: 10, Status: "ok"}); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	Usage(s)(w, httptest.NewRequest("GET", "/api/usage?from=2026-09-01&to=2026-09-24", nil))
	if w.Code != 200 {
		t.Fatalf("Status = %d, Body = %s", w.Code, w.Body.String())
	}
	var u store.Usage
	if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	if len(u.Series) != 1 {
		t.Fatalf("a plain date as to must include the whole last day, series = %v", u.Series)
	}
}

func TestUsageRejectsInvalidDates(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "m.db"))
	defer s.Close()
	for _, query := range []string{"?from=2026-13-45", "?to=tomorrow", "?from=2026-09-24&to=2026-09-01"} {
		w := httptest.NewRecorder()
		Usage(s)(w, httptest.NewRequest("GET", "/api/usage"+query, nil))
		if w.Code != 400 {
			t.Errorf("%s: Status = %d, want 400 — invalid input must not silently become a different range", query, w.Code)
		}
	}
}

func TestUsageRejectsTooManyBuckets(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "m.db"))
	defer s.Close()
	w := httptest.NewRecorder()
	Usage(s)(w, httptest.NewRequest("GET", "/api/usage?bucket=hour&from=1970-01-01&to=2026-09-24", nil))
	if w.Code != 400 {
		t.Fatalf("Status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "buckets") {
		t.Errorf("the message should explain what to do: %s", w.Body.String())
	}
}

func TestUsageAcceptsValidRange(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "m.db"))
	defer s.Close()
	w := httptest.NewRecorder()
	Usage(s)(w, httptest.NewRequest("GET", "/api/usage?bucket=hour&from=2026-09-01&to=2026-09-24", nil))
	if w.Code != 200 {
		t.Errorf("a plausible hourly range must pass, Status = %d, Body = %s", w.Code, w.Body.String())
	}
}

func TestMessagesLimitIsCapped(t *testing.T) {
	l, err := store.OpenLog(filepath.Join(t.TempDir(), "log.db"), store.StaticLimits("full", 7, 500, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for i := 0; i < 3; i++ {
		l.Write("r", map[string][]byte{"in": []byte(`{"a":1}`)})
	}
	w := httptest.NewRecorder()
	Messages(l)(w, httptest.NewRequest("GET", "/api/messages?limit=1000000", nil))
	if w.Code != 200 {
		t.Fatalf("Status = %d", w.Code)
	}
	if parseInt("1000000", 50, maxMessageList) != maxMessageList {
		t.Errorf("limit is not capped")
	}
}

func TestMessagesByRequestReturnsDocs(t *testing.T) {
	l, err := store.OpenLog(filepath.Join(t.TempDir(), "log.db"), store.StaticLimits("full", 7, 500, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Write("req-1", map[string][]byte{"in": []byte(`{"a":1}`)})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/messages/by-request/{request_id}", MessagesByRequest(l))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/messages/by-request/req-1", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"in":"{\"a\":1}"`) {
		t.Errorf("Status %d, Body %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/messages/by-request/missing", nil))
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "{}" {
		t.Errorf("unknown request_id: Status %d, Body %s", w.Code, w.Body.String())
	}
}
