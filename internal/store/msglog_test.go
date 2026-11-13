package store

import (
	"bytes"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openLog(t *testing.T, level string, keepDays, maxMB, maxKB int) *MsgLog {
	t.Helper()
	return openLogWithLimits(t, StaticLimits(level, keepDays, maxMB, maxKB))
}

func openLogWithLimits(t *testing.T, limits func() LogLimits) *MsgLog {
	t.Helper()
	l, err := OpenLog(filepath.Join(t.TempDir(), "log.db"), limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestOpenLogRejectsNilLimits(t *testing.T) {
	_, err := OpenLog(filepath.Join(t.TempDir(), "log.db"), nil)
	if err == nil {
		t.Fatal("OpenLog with nil limits must return an error instead of panicking later")
	}
}

func TestLogOffWritesNothing(t *testing.T) {
	l := openLog(t, "off", 7, 500, 256)
	if err := l.Write("r1", map[string][]byte{"in": []byte(`{"a":1}`)}); err != nil {
		t.Fatal(err)
	}
	entries, _ := l.List(10, 0)
	if len(entries) != 0 {
		t.Fatalf("level=off must not write anything, has %d entries", len(entries))
	}
}

func TestLogFullRoundTripsThroughGzip(t *testing.T) {
	l := openLog(t, "full", 7, 500, 256)
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	if err := l.Write("r1", map[string][]byte{"in": body}); err != nil {
		t.Fatal(err)
	}
	entries, err := l.List(10, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("List = %v, %v", entries, err)
	}
	docs, err := l.Get(entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(docs["in"], body) {
		t.Errorf("round trip broken: %s", docs["in"])
	}
}

func TestLogMetaStoresNoBodies(t *testing.T) {
	l := openLog(t, "meta", 7, 500, 256)
	l.Write("r1", map[string][]byte{"in": []byte(`{"messages":[{"role":"user","content":"secret"}]}`)})
	entries, _ := l.List(10, 0)
	if len(entries) != 1 {
		t.Fatalf("meta must create one entry, has %d", len(entries))
	}
	docs, _ := l.Get(entries[0].ID)
	for _, d := range docs {
		if strings.Contains(string(d), "secret") {
			t.Fatal("level=meta must not store content")
		}
	}
}

func TestBodyIsTruncatedAndMarked(t *testing.T) {
	l := openLog(t, "full", 7, 500, 1) // 1 KB
	big := append([]byte(`{"x":"`), bytes.Repeat([]byte("a"), 8000)...)
	l.Write("r1", map[string][]byte{"in": big})
	entries, _ := l.List(10, 0)
	docs, _ := l.Get(entries[0].ID)
	if len(docs["in"]) > 2048 {
		t.Errorf("body was not truncated: %d bytes", len(docs["in"]))
	}
	if !bytes.Contains(docs["in"], []byte("truncated")) {
		t.Error("truncated bodies must be recognizable as such")
	}
}

func TestCleanupDropsOldEntries(t *testing.T) {
	l := openLog(t, "full", 1, 500, 256)
	l.writeAt("old", map[string][]byte{"in": []byte("{}")}, time.Now().AddDate(0, 0, -5))
	l.Write("new", map[string][]byte{"in": []byte("{}")})
	if err := l.Cleanup(); err != nil {
		t.Fatal(err)
	}
	entries, _ := l.List(10, 0)
	if len(entries) != 1 {
		t.Fatalf("keep_days=1 must delete the 5-day-old entry, %d remain", len(entries))
	}
}

func TestCleanupEnforcesSizeCap(t *testing.T) {
	l := openLog(t, "full", 3650, 1, 4096) // keep_days effectively off, 1 MB cap
	// Random bytes, since gzip shrinks repeated patterns to a few hundred bytes and the cap would never be hit.
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20; i++ {
		blob := make([]byte, 300_000)
		if _, err := rng.Read(blob); err != nil {
			t.Fatal(err)
		}
		if err := l.Write("r", map[string][]byte{"in": blob}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := l.SizeBytes()
	if err != nil {
		t.Fatal(err)
	}
	if before < 1<<20 {
		t.Fatalf("precondition failed: file is only %d bytes, the cap is never reached", before)
	}
	if err := l.Cleanup(); err != nil {
		t.Fatal(err)
	}
	after, err := l.SizeBytes()
	if err != nil {
		t.Fatal(err)
	}
	if after > 2<<20 {
		t.Errorf("file is %d bytes — the size cap does not apply (VACUUM missing?)", after)
	}
	if entries, _ := l.List(100, 0); len(entries) == 0 {
		t.Error("the size cap must not delete everything")
	}
}

func TestZeroDisablesLimit(t *testing.T) {
	l := openLog(t, "full", 0, 0, 0)
	l.writeAt("old", map[string][]byte{"in": []byte("{}")}, time.Now().AddDate(-5, 0, 0))
	l.Cleanup()
	if entries, _ := l.List(10, 0); len(entries) != 1 {
		t.Error("keep_days=0 disables the limit, the old entry must stay")
	}
}

func TestTruncateDoesNotTouchCallersBuffer(t *testing.T) {
	l := openLog(t, "full", 7, 500, 1) // 1 KB
	body := make([]byte, 4096)
	for i := range body {
		body[i] = 'a'
	}
	original := append([]byte(nil), body...)
	l.truncate(body, 1)
	if !bytes.Equal(body, original) {
		t.Error("truncate modified the caller's buffer")
	}
}

func TestLiveLimitsApplyWithoutReopening(t *testing.T) {
	level := "off"
	l := openLogWithLimits(t, func() LogLimits { return LogLimits{Level: level, KeepDays: 7, MaxSizeMB: 500, MaxBodyKB: 256} })

	if err := l.Write("r1", map[string][]byte{"in": []byte(`{"a":1}`)}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := l.List(10, 0); len(entries) != 0 {
		t.Fatalf("level=off must not write anything, has %d entries", len(entries))
	}

	level = "full" // changed via PUT /api/config without reopening the log
	if err := l.Write("r2", map[string][]byte{"in": []byte(`{"a":2}`)}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := l.List(10, 0); len(entries) != 1 {
		t.Fatalf("the changed level must apply immediately without restart, has %d entries", len(entries))
	}
}

func TestDeletingEntryRemovesItsDocuments(t *testing.T) {
	l := openLog(t, "full", 7, 500, 256)
	if err := l.Write("r1", map[string][]byte{"in": []byte(`{"a":1}`)}); err != nil {
		t.Fatal(err)
	}
	entries, _ := l.List(10, 0)
	if _, err := l.db.Exec(`DELETE FROM log_entries WHERE id = ?`, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := l.db.QueryRow(`SELECT COUNT(*) FROM log_docs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d orphaned documents left — ON DELETE CASCADE not working, the file would never shrink", n)
	}
}

func TestGetByRequestIDReturnsNewestEntry(t *testing.T) {
	l := openLog(t, "full", 7, 500, 256)
	l.Write("r1", map[string][]byte{"in": []byte(`old`)})
	l.Write("r2", map[string][]byte{"in": []byte(`other`)})
	l.Write("r1", map[string][]byte{"in": []byte(`new`), "out": []byte(`reply`)})

	docs, err := l.GetByRequestID("r1")
	if err != nil {
		t.Fatal(err)
	}
	if string(docs["in"]) != "new" || string(docs["out"]) != "reply" {
		t.Errorf("expected the newest entry, got %q", docs)
	}

	docs, err = l.GetByRequestID("missing")
	if err != nil || len(docs) != 0 {
		t.Errorf("unknown request_id: %v, %v", docs, err)
	}
}

func TestRequestIDIsIndexed(t *testing.T) {
	l := openLog(t, "full", 7, 500, 256)
	var plan string
	var id, parent, notused int
	err := l.db.QueryRow(`EXPLAIN QUERY PLAN SELECT id FROM log_entries WHERE request_id = ? ORDER BY id DESC LIMIT 1`, "x").
		Scan(&id, &parent, &notused, &plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "idx_log_request") {
		t.Errorf("lookup by request_id uses no index: %s", plan)
	}
}

func TestOverLimitComparesFileSizeWithMaxSize(t *testing.T) {
	l := openLog(t, "full", 3650, 1, 4096)
	if over, err := l.OverLimit(); err != nil || over {
		t.Fatalf("empty log must not be over the limit: %v, %v", over, err)
	}
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 5; i++ {
		blob := make([]byte, 300_000)
		rng.Read(blob)
		if err := l.Write("r", map[string][]byte{"in": blob}); err != nil {
			t.Fatal(err)
		}
	}
	if over, err := l.OverLimit(); err != nil || !over {
		t.Errorf("1.5 MB random data with 1 MB cap: OverLimit = %v, %v", over, err)
	}

	unlimited := openLog(t, "full", 3650, 0, 4096)
	unlimited.Write("r", map[string][]byte{"in": make([]byte, 10)})
	if over, _ := unlimited.OverLimit(); over {
		t.Error("max_size_mb = 0 disables the limit")
	}
}
