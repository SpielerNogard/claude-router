package store

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type Record struct {
	ID            int64     `json:"id"`
	TS            time.Time `json:"ts"`
	Alias         string    `json:"alias"`
	Provider      string    `json:"provider"`
	UpstreamModel string    `json:"model"`
	AuthMode      string    `json:"auth_mode"`
	InputTokens   int64     `json:"input_tokens"`
	OutputTokens  int64     `json:"output_tokens"`
	CacheRead     int64     `json:"cache_read"`
	CacheWrite    int64     `json:"cache_write"`
	CostUSD       *float64  `json:"cost_usd"`
	DurationMS    int64     `json:"duration_ms"`
	Status        string    `json:"status"`
	ErrorCode     string    `json:"error_code"`
	Client        string    `json:"client"`
	RequestID     string    `json:"request_id"`
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaRequests); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureRequestIDColumn(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_requests_request_id ON requests(request_id)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// ensureRequestIDColumn migrates databases created before request_id; CREATE TABLE IF NOT EXISTS doesn't add columns to existing tables.
func ensureRequestIDColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(requests)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "request_id" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE requests ADD COLUMN request_id TEXT NOT NULL DEFAULT ''`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Insert(r Record) error {
	_, err := s.db.Exec(`
INSERT INTO requests (ts, alias, provider, upstream_model, auth_mode,
  input_tokens, output_tokens, cache_read, cache_write, cost_usd,
  duration_ms, status, error_code, client, request_id)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.TS.UTC().Unix(), r.Alias, r.Provider, r.UpstreamModel, r.AuthMode,
		r.InputTokens, r.OutputTokens, r.CacheRead, r.CacheWrite, r.CostUSD,
		r.DurationMS, r.Status, r.ErrorCode, r.Client, r.RequestID)
	return err
}

type RequestFilter struct {
	After      int64
	Since      time.Time
	Provider   string
	Model      string
	ErrorsOnly bool
	Limit      int
	Offset     int
}

// Recent returns the newest rows first, plus the total match count for pagination.
func (s *Store) Recent(f RequestFilter) ([]Record, int, error) {
	where, args := "id > ?", []any{f.After}
	if !f.Since.IsZero() {
		where, args = where+" AND ts >= ?", append(args, f.Since.UTC().Unix())
	}
	if f.Provider != "" {
		where, args = where+" AND provider = ?", append(args, f.Provider)
	}
	if f.Model != "" {
		where, args = where+" AND upstream_model = ?", append(args, f.Model)
	}
	if f.ErrorsOnly {
		where += " AND status != 'ok'"
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`
SELECT id, ts, alias, provider, upstream_model, auth_mode, input_tokens, output_tokens,
  cache_read, cache_write, cost_usd, duration_ms, status, error_code, client, request_id
FROM requests WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		var ts int64
		if err := rows.Scan(&r.ID, &ts, &r.Alias, &r.Provider, &r.UpstreamModel, &r.AuthMode,
			&r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheWrite, &r.CostUSD,
			&r.DurationMS, &r.Status, &r.ErrorCode, &r.Client, &r.RequestID); err != nil {
			return nil, 0, err
		}
		r.TS = time.Unix(ts, 0).UTC()
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// test only
func (s *Store) all() ([]Record, error) {
	rows, err := s.db.Query(`SELECT ts, alias, provider, auth_mode, input_tokens, output_tokens, cost_usd, status FROM requests ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var ts int64
		if err := rows.Scan(&ts, &r.Alias, &r.Provider, &r.AuthMode, &r.InputTokens, &r.OutputTokens, &r.CostUSD, &r.Status); err != nil {
			return nil, err
		}
		r.TS = time.Unix(ts, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}
