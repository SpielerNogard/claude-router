package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertAndReadBack(t *testing.T) {
	s := openTemp(t)
	cost := 0.0123
	in := Record{
		TS: time.Now().UTC(), Alias: "deepseek:chat", Provider: "deepseek",
		UpstreamModel: "deepseek-chat", AuthMode: "key",
		InputTokens: 100, OutputTokens: 50, CostUSD: &cost,
		DurationMS: 420, Status: "ok", Client: "claude-cli/2.0",
	}
	if err := s.Insert(in); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	rows, err := s.all()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].CostUSD == nil || *rows[0].CostUSD != cost {
		t.Errorf("CostUSD = %v, want %v", rows[0].CostUSD, cost)
	}
}

func TestNullCostStaysNull(t *testing.T) {
	s := openTemp(t)
	if err := s.Insert(Record{TS: time.Now().UTC(), Alias: "x:y", Provider: "x", AuthMode: "key", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.all()
	if rows[0].CostUSD != nil {
		t.Fatalf("unknown price must stay NULL, got %v", *rows[0].CostUSD)
	}
}

func TestRequestIDIsStored(t *testing.T) {
	s := openTemp(t)
	if err := s.Insert(Record{TS: time.Now().UTC(), Alias: "x:y", Provider: "x", AuthMode: "key",
		Status: "ok", RequestID: "abc123"}); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := s.db.QueryRow(`SELECT request_id FROM requests`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Errorf("request_id = %q, want abc123", got)
	}
}

func TestEnsureRequestIDColumnMigratesOldSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "metrics.db")
	db, err := sql.Open("sqlite", p+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	// Schema as before request_id was introduced.
	if _, err := db.Exec(`CREATE TABLE requests (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		ts             INTEGER NOT NULL,
		alias          TEXT    NOT NULL,
		provider       TEXT    NOT NULL,
		upstream_model TEXT    NOT NULL DEFAULT '',
		auth_mode      TEXT    NOT NULL,
		input_tokens   INTEGER NOT NULL DEFAULT 0,
		output_tokens  INTEGER NOT NULL DEFAULT 0,
		cache_read     INTEGER NOT NULL DEFAULT 0,
		cache_write    INTEGER NOT NULL DEFAULT 0,
		cost_usd       REAL,
		duration_ms    INTEGER NOT NULL DEFAULT 0,
		status         TEXT    NOT NULL,
		error_code     TEXT    NOT NULL DEFAULT '',
		client         TEXT    NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(p)
	if err != nil {
		t.Fatalf("Open on an old schema must migrate instead of failing: %v", err)
	}
	defer s.Close()
	if err := s.Insert(Record{TS: time.Now().UTC(), Alias: "x:y", Provider: "x", AuthMode: "key",
		Status: "ok", RequestID: "abc"}); err != nil {
		t.Fatalf("Insert after migration: %v", err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "metrics.db")
	s1, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	s1.Close()
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("second Open on existing file: %v", err)
	}
	s2.Close()
}

func TestRecentNewestFirstWithAfterAndLimit(t *testing.T) {
	s := openTemp(t)
	for i := 0; i < 5; i++ {
		mustInsert(t, s, Record{TS: time.Now().UTC(), Alias: "a:m", Provider: "a", UpstreamModel: "m",
			AuthMode: "key", InputTokens: int64(i), CacheRead: 7, Status: "ok"})
	}

	all, _, err := s.Recent(RequestFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 || all[0].ID != 5 || all[4].ID != 1 {
		t.Fatalf("expected ids 5..1, got %+v", all)
	}
	if all[0].UpstreamModel != "m" || all[0].CacheRead != 7 || all[0].InputTokens != 4 {
		t.Errorf("fields not read back: %+v", all[0])
	}

	newer, _, _ := s.Recent(RequestFilter{After: 3, Limit: 100})
	if len(newer) != 2 || newer[0].ID != 5 || newer[1].ID != 4 {
		t.Errorf("after=3: %+v", newer)
	}

	two, _, _ := s.Recent(RequestFilter{Limit: 2})
	if len(two) != 2 || two[0].ID != 5 {
		t.Errorf("limit=2: %+v", two)
	}
}

func TestRecentEmptyIsNotNil(t *testing.T) {
	got, _, err := openTemp(t).Recent(RequestFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("empty result must be [], not nil")
	}
}

func TestRecentFiltersPagesAndCounts(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		r := Record{TS: base.Add(time.Duration(i) * time.Minute), Alias: "a:m", Provider: "sub", UpstreamModel: "opus",
			AuthMode: "passthrough", Status: "ok"}
		if i%2 == 1 {
			r.Provider, r.Status = "account2", "error"
		}
		mustInsert(t, s, r)
	}

	rows, total, err := s.Recent(RequestFilter{Since: base.Add(2 * time.Minute), Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(rows) != 2 || rows[0].ID != 5 || rows[1].ID != 4 {
		t.Errorf("since+offset: total=%d rows=%+v", total, rows)
	}

	rows, total, _ = s.Recent(RequestFilter{Provider: "account2", Limit: 10})
	if total != 3 || rows[0].ID != 6 {
		t.Errorf("provider: total=%d rows=%+v", total, rows)
	}
	if _, total, _ = s.Recent(RequestFilter{ErrorsOnly: true, Limit: 10}); total != 3 {
		t.Errorf("errors only: total=%d", total)
	}
	if _, total, _ = s.Recent(RequestFilter{Model: "sonnet", Limit: 10}); total != 0 {
		t.Errorf("model: total=%d", total)
	}
}
