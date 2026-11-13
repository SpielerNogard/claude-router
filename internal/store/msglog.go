package store

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

const schemaLog = `
CREATE TABLE IF NOT EXISTS log_entries (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  ts         INTEGER NOT NULL,
  request_id TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_log_ts ON log_entries(ts);
CREATE INDEX IF NOT EXISTS idx_log_request ON log_entries(request_id);

CREATE TABLE IF NOT EXISTS log_docs (
  entry_id INTEGER NOT NULL REFERENCES log_entries(id) ON DELETE CASCADE,
  kind     TEXT    NOT NULL,
  body     BLOB    NOT NULL,
  PRIMARY KEY (entry_id, kind)
);
`

type LogEntry struct {
	ID        int64     `json:"id"`
	TS        time.Time `json:"ts"`
	RequestID string    `json:"request_id"`
}

// LogLimits are the four [log] settings. MsgLog reads them fresh on every use, otherwise changes via PUT /api/config would need a restart.
type LogLimits struct {
	Level     string
	KeepDays  int
	MaxSizeMB int
	MaxBodyKB int
}

type MsgLog struct {
	db     *sql.DB
	path   string
	limits func() LogLimits
}

func OpenLog(path string, limits func() LogLimits) (*MsgLog, error) {
	if limits == nil {
		return nil, fmt.Errorf("OpenLog: limits must not be nil")
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaLog); err != nil {
		db.Close()
		return nil, err
	}
	return &MsgLog{db: db, path: path, limits: limits}, nil
}

// StaticLimits returns fixed LogLimits, for callers whose config doesn't change (tests, one-off scripts).
func StaticLimits(level string, keepDays, maxSizeMB, maxBodyKB int) func() LogLimits {
	lim := LogLimits{Level: level, KeepDays: keepDays, MaxSizeMB: maxSizeMB, MaxBodyKB: maxBodyKB}
	return func() LogLimits { return lim }
}

func (l *MsgLog) Close() error { return l.db.Close() }

// Enabled reports whether entries are written at all — not with "off".
func (l *MsgLog) Enabled() bool { return l.limits().Level != "off" }

// StoresDocuments reports whether documents are stored (only with "full"), so callers can skip collecting them.
func (l *MsgLog) StoresDocuments() bool { return l.limits().Level == "full" }

func (l *MsgLog) Write(requestID string, docs map[string][]byte) error {
	return l.writeAt(requestID, docs, time.Now().UTC())
}

func (l *MsgLog) writeAt(requestID string, docs map[string][]byte, ts time.Time) error {
	lim := l.limits()
	if lim.Level == "off" {
		return nil
	}
	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO log_entries (ts, request_id) VALUES (?,?)`, ts.UTC().Unix(), requestID)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if lim.Level == "full" {
		for kind, body := range docs {
			blob, err := gzipBytes(l.truncate(body, lim.MaxBodyKB))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO log_docs (entry_id, kind, body) VALUES (?,?,?)`, id, kind, blob); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (l *MsgLog) truncate(body []byte, maxBodyKB int) []byte {
	limit := maxBodyKB * 1024
	if limit <= 0 || len(body) <= limit {
		return body
	}
	marker := fmt.Sprintf("\n… truncated, %d bytes dropped", len(body)-limit)
	// Own buffer: appending to body[:limit] would overwrite the caller's data.
	out := make([]byte, 0, limit+len(marker))
	out = append(out, body[:limit]...)
	return append(out, marker...)
}

func (l *MsgLog) List(limit, offset int) ([]LogEntry, error) {
	rows, err := l.db.Query(`SELECT id, ts, request_id FROM log_entries ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.RequestID); err != nil {
			return nil, err
		}
		e.TS = time.Unix(ts, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

func (l *MsgLog) Get(id int64) (map[string][]byte, error) {
	rows, err := l.db.Query(`SELECT kind, body FROM log_docs WHERE entry_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var kind string
		var blob []byte
		if err := rows.Scan(&kind, &blob); err != nil {
			return nil, err
		}
		plain, err := gunzipBytes(blob)
		if err != nil {
			return nil, err
		}
		out[kind] = plain
	}
	return out, rows.Err()
}

// With several entries for the same request_id, the newest wins.
func (l *MsgLog) GetByRequestID(requestID string) (map[string][]byte, error) {
	var id int64
	err := l.db.QueryRow(`SELECT id FROM log_entries WHERE request_id = ? ORDER BY id DESC LIMIT 1`, requestID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	return l.Get(id)
}

func (l *MsgLog) SizeBytes() (int64, error) {
	var total int64
	for _, suffix := range []string{"", "-wal"} {
		fi, err := os.Stat(l.path + suffix)
		if err != nil {
			if suffix != "" && os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		total += fi.Size()
	}
	return total, nil
}

// OverLimit is deliberately cheap (stat only) so it can run often; Cleanup itself needs a VACUUM.
func (l *MsgLog) OverLimit() (bool, error) {
	maxMB := l.limits().MaxSizeMB
	if maxMB <= 0 {
		return false, nil
	}
	size, err := l.SizeBytes()
	if err != nil {
		return false, err
	}
	return size > int64(maxMB)<<20, nil
}

// Both limits apply; without VACUUM SQLite doesn't release space and max_size_mb would have no effect.
func (l *MsgLog) Cleanup() error {
	lim := l.limits()
	if lim.KeepDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -lim.KeepDays).Unix()
		if _, err := l.db.Exec(`DELETE FROM log_entries WHERE ts < ?`, cutoff); err != nil {
			return err
		}
	}
	if lim.MaxSizeMB <= 0 {
		return l.vacuum()
	}
	limit := int64(lim.MaxSizeMB) << 20
	for i := 0; i < 50; i++ {
		if err := l.vacuum(); err != nil {
			return err
		}
		size, err := l.SizeBytes()
		if err != nil {
			return err
		}
		if size <= limit {
			return nil
		}
		var n int64
		if err := l.db.QueryRow(`SELECT COUNT(*) FROM log_entries`).Scan(&n); err != nil {
			return err
		}
		if n <= 1 {
			log.Printf("message-log: %d bytes over the limit of %d, but only %d entry left", size, limit, n)
			return nil
		}
		drop := n / 4
		if drop < 1 {
			drop = 1
		}
		if _, err := l.db.Exec(
			`DELETE FROM log_entries WHERE id IN (SELECT id FROM log_entries ORDER BY id ASC LIMIT ?)`, drop); err != nil {
			return err
		}
	}
	if err := l.vacuum(); err != nil {
		return err
	}
	size, _ := l.SizeBytes()
	log.Printf("message-log: size limit not reached after 50 passes (%d bytes, limit %d)", size, limit)
	return nil
}

func (l *MsgLog) vacuum() error {
	if _, err := l.db.Exec(`VACUUM`); err != nil {
		return err
	}
	// VACUUM alone leaves the WAL file untruncated in WAL mode; without this
	// checkpoint SizeBytes keeps counting bytes VACUUM already freed.
	_, err := l.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipBytes(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}
