package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func getRequests(t *testing.T, s *store.Store, query string) []map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	Requests(s)(w, httptest.NewRequest("GET", "/api/requests"+query, nil))
	if w.Code != 200 {
		t.Fatalf("Status = %d, Body = %s", w.Code, w.Body.String())
	}
	var body struct{ Entries []map[string]any }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Entries
}

func TestRequestsListsNewestWithFields(t *testing.T) {
	s := openStore(t)
	for i := 0; i < 3; i++ {
		s.Insert(store.Record{TS: time.Now().UTC(), Alias: "account2:opus", Provider: "account2",
			UpstreamModel: "claude-opus-5-5", AuthMode: "claude-credential-helper",
			CacheRead: 900, Status: "ok"})
	}
	got := getRequests(t, s, "?after=1")
	if len(got) != 2 || got[0]["id"] != 3.0 {
		t.Fatalf("after=1: %v", got)
	}
	if got[0]["model"] != "claude-opus-5-5" || got[0]["cache_read"] != 900.0 || got[0]["cost_usd"] != nil {
		t.Errorf("fields: %v", got[0])
	}
}

func TestRequestsCapsLimit(t *testing.T) {
	s := openStore(t)
	for i := 0; i < maxMessageList+5; i++ {
		s.Insert(store.Record{TS: time.Now().UTC(), Alias: "a:m", Provider: "a", AuthMode: "key", Status: "ok"})
	}
	if got := getRequests(t, s, "?limit=9999"); len(got) != maxMessageList {
		t.Errorf("limit=9999 returns %d rows, max is %d", len(got), maxMessageList)
	}
	if got := getRequests(t, s, ""); len(got) != 100 {
		t.Errorf("without limit %d rows, want 100", len(got))
	}
}

func TestRequestsEmptyIsArray(t *testing.T) {
	w := httptest.NewRecorder()
	Requests(openStore(t))(w, httptest.NewRequest("GET", "/api/requests", nil))
	if !strings.Contains(w.Body.String(), `"entries":[]`) {
		t.Errorf("empty DB must return []: %s", w.Body.String())
	}
}

func TestRequestsFiltersAndReportsTotal(t *testing.T) {
	s := openStore(t)
	for i := 0; i < 4; i++ {
		p := "sub"
		if i%2 == 1 {
			p = "account2"
		}
		s.Insert(store.Record{TS: time.Now().UTC(), Alias: "a", Provider: p, AuthMode: "passthrough", Status: "ok"})
	}
	w := httptest.NewRecorder()
	Requests(s)(w, httptest.NewRequest("GET", "/api/requests?provider=account2&limit=1&offset=1", nil))
	var body struct {
		Entries []map[string]any
		Total   int
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Total != 2 || len(body.Entries) != 1 || body.Entries[0]["id"] != 2.0 {
		t.Errorf("Body = %s", w.Body.String())
	}
}

func TestRequestsRejectsBadSince(t *testing.T) {
	w := httptest.NewRecorder()
	Requests(openStore(t))(w, httptest.NewRequest("GET", "/api/requests?since=yesterday", nil))
	if w.Code != 400 {
		t.Errorf("Status = %d, want 400", w.Code)
	}
}
