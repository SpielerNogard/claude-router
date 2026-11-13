package store

import (
	"testing"
	"time"
)

func TestStatsTotalsByProviderAndSeries(t *testing.T) {
	s := openTemp(t)
	now := time.Date(2026, 10, 2, 12, 10, 0, 0, time.UTC)
	cost := 0.5
	mustInsert(t, s, Record{TS: now.Add(-9 * time.Minute), Alias: "a", Provider: "sub", AuthMode: "passthrough",
		InputTokens: 10, OutputTokens: 5, CacheRead: 90, CostUSD: &cost, Status: "ok"})
	mustInsert(t, s, Record{TS: now.Add(-1 * time.Minute), Alias: "a", Provider: "lmstudio", AuthMode: "none",
		InputTokens: 20, Status: "ok"})
	mustInsert(t, s, Record{TS: now.Add(-2 * time.Hour), Alias: "a", Provider: "sub", AuthMode: "passthrough",
		InputTokens: 999, Status: "ok"})

	st, err := s.Stats(now.Add(-10*time.Minute), 5*time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if st.Totals.Requests != 2 || st.Totals.InputTokens != 30 || st.Totals.CacheRead != 90 {
		t.Errorf("totals = %+v", st.Totals)
	}
	if st.Totals.CostUSD == nil || *st.Totals.CostUSD != 0.5 {
		t.Errorf("cost = %v", st.Totals.CostUSD)
	}
	if st.ByProvider["lmstudio"].CostUSD != nil {
		t.Errorf("without a known price the sum must be null")
	}
	if st.ByProvider["sub"].OutputTokens != 5 {
		t.Errorf("sub = %+v", st.ByProvider["sub"])
	}
	if len(st.Series.InputTokens) != 3 || st.Series.InputTokens[0] != 10 || st.Series.InputTokens[1] != 20 || st.Series.InputTokens[2] != 0 {
		t.Errorf("series = %+v (start %v)", st.Series.InputTokens, st.Start)
	}
	if len(st.Series.CostUSD) != 3 || st.Series.CostUSD[0] != 0.5 || st.Series.CostUSD[1] != 0 {
		t.Errorf("cost series = %+v", st.Series.CostUSD)
	}
}
