package store

const schemaRequests = `
CREATE TABLE IF NOT EXISTS requests (
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
  client         TEXT    NOT NULL DEFAULT '',
  request_id     TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_requests_ts ON requests(ts);

CREATE TABLE IF NOT EXISTS discovered_models (
  provider TEXT NOT NULL,
  model    TEXT NOT NULL,
  seen_at  INTEGER NOT NULL,
  PRIMARY KEY (provider, model)
);
`
