package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/store"
)

func TestStatsSumsSinceWithStep(t *testing.T) {
	s := openStore(t)
	s.Insert(store.Record{TS: time.Now().UTC(), Alias: "a", Provider: "sub", AuthMode: "passthrough", InputTokens: 7, Status: "ok"})
	since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	w := httptest.NewRecorder()
	Stats(s)(w, httptest.NewRequest("GET", "/api/stats?since="+since+"&step=60", nil))
	var st store.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || w.Code != 200 {
		t.Fatalf("Status %d, Body %s", w.Code, w.Body.String())
	}
	if st.Totals.InputTokens != 7 || st.ByProvider["sub"].Requests != 1 || len(st.Series.InputTokens) < 60 {
		t.Errorf("stats = %+v", st)
	}
}

func TestStatsRejectsBadParams(t *testing.T) {
	week := time.Now().Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	for _, q := range []string{"", "?since=yesterday&step=60", "?since=" + week + "&step=30", "?since=" + week + "&step=60"} {
		w := httptest.NewRecorder()
		Stats(openStore(t))(w, httptest.NewRequest("GET", "/api/stats"+q, nil))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "error") {
			t.Errorf("%q: Status %d, Body %s", q, w.Code, w.Body.String())
		}
	}
}
