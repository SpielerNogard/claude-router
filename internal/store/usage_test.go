package store

import (
	"testing"
	"time"
)

func TestUsageBucketsAreGapless(t *testing.T) {
	s := openTemp(t)
	day := func(d int) time.Time { return time.Date(2026, 9, d, 12, 0, 0, 0, time.UTC) }
	cost := 1.0
	mustInsert(t, s, Record{TS: day(1), Alias: "a:m", Provider: "a", AuthMode: "key", CostUSD: &cost, InputTokens: 10, Status: "ok"})
	mustInsert(t, s, Record{TS: day(3), Alias: "a:m", Provider: "a", AuthMode: "key", CostUSD: &cost, InputTokens: 10, Status: "ok"})

	u, err := s.Usage(day(1), day(3), "day", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-09-01", "2026-09-02", "2026-09-03"}
	if len(u.Buckets) != 3 {
		t.Fatalf("Buckets = %v, want %v", u.Buckets, want)
	}
	for i := range want {
		if u.Buckets[i] != want[i] {
			t.Fatalf("Buckets = %v, want %v", u.Buckets, want)
		}
	}
	if u.Series[0].Requests[1] != 0 {
		t.Errorf("the empty Sep 2 must appear as 0, not be missing")
	}
}

func TestUsageSeparatesBilledFromSubscription(t *testing.T) {
	s := openTemp(t)
	ts := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	cost := 2.5
	mustInsert(t, s, Record{TS: ts, Alias: "sub:opus", Provider: "sub", AuthMode: "passthrough", CostUSD: &cost, Status: "ok"})
	mustInsert(t, s, Record{TS: ts, Alias: "ds:chat", Provider: "ds", AuthMode: "key", CostUSD: &cost, Status: "ok"})

	u, err := s.Usage(ts, ts, "day", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Series) != 2 {
		t.Fatalf("expected 2 series, got %d", len(u.Series))
	}
	byKey := map[string]Series{}
	for _, se := range u.Series {
		byKey[se.Key] = se
	}
	if byKey["sub"].Billed {
		t.Error("passthrough series must not be marked billed")
	}
	if !byKey["ds"].Billed {
		t.Error("key series must be marked billed")
	}
}

func TestUsageRejectsUnknownBucketAndGroup(t *testing.T) {
	s := openTemp(t)
	now := time.Now().UTC()
	if _, err := s.Usage(now, now, "fortnight", ""); err == nil {
		t.Error("unknown bucket must be rejected")
	}
	if _, err := s.Usage(now, now, "day", "; DROP TABLE requests"); err == nil {
		t.Error("unknown group_by must be rejected")
	}
}

func TestUsageWeekBucketsStartOnMonday(t *testing.T) {
	s := openTemp(t)
	// 2026-09-06 is a Sunday in the week starting Monday 2026-08-31; 2026-09-07 starts a new week.
	sunday := time.Date(2026, 9, 6, 23, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC)
	mustInsert(t, s, Record{TS: sunday, Alias: "a:m", Provider: "a", AuthMode: "key", InputTokens: 10, Status: "ok"})
	mustInsert(t, s, Record{TS: monday, Alias: "a:m", Provider: "a", AuthMode: "key", InputTokens: 20, Status: "ok"})

	u, err := s.Usage(sunday, monday, "week", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-08-31", "2026-09-07"}
	if len(u.Buckets) != len(want) {
		t.Fatalf("Buckets = %v, want %v", u.Buckets, want)
	}
	for i := range want {
		if u.Buckets[i] != want[i] {
			t.Fatalf("Buckets = %v, want %v", u.Buckets, want)
		}
	}
	if len(u.Series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(u.Series))
	}
	if got := u.Series[0].TokensIn; got[0] != 10 || got[1] != 20 {
		t.Errorf("TokensIn = %v, want [10 20] — Sunday belongs to the previous week", got)
	}
}

func TestUsageSeriesAreSortedByKey(t *testing.T) {
	s := openTemp(t)
	ts := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	for _, provider := range []string{"zebra", "alpha", "mid"} {
		mustInsert(t, s, Record{TS: ts, Alias: provider + ":m", Provider: provider, AuthMode: "key", InputTokens: 1, Status: "ok"})
	}

	u, err := s.Usage(ts, ts, "day", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Series) != 3 {
		t.Fatalf("expected 3 series, got %d", len(u.Series))
	}
	want := []string{"alpha", "mid", "zebra"}
	for i, k := range want {
		if u.Series[i].Key != k {
			t.Errorf("Series[%d].Key = %q, want %q — order must be deterministic", i, u.Series[i].Key, k)
		}
	}
}

func mustInsert(t *testing.T, s *Store, r Record) {
	t.Helper()
	if err := s.Insert(r); err != nil {
		t.Fatal(err)
	}
}

func TestUsageSumsCacheTokens(t *testing.T) {
	s := openTemp(t)
	ts := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	mustInsert(t, s, Record{TS: ts, Alias: "a:m", Provider: "a", AuthMode: "passthrough",
		InputTokens: 10, CacheRead: 800, CacheWrite: 50, Status: "ok"})
	mustInsert(t, s, Record{TS: ts, Alias: "a:m", Provider: "a", AuthMode: "passthrough",
		InputTokens: 5, CacheRead: 200, Status: "ok"})

	u, err := s.Usage(ts, ts, "day", "provider")
	if err != nil {
		t.Fatal(err)
	}
	se := u.Series[0]
	if se.CacheRead[0] != 1000 || se.CacheWrite[0] != 50 || se.TokensIn[0] != 15 {
		t.Errorf("cache_read=%v cache_write=%v tokens_in=%v", se.CacheRead, se.CacheWrite, se.TokensIn)
	}
}
