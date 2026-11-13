package store

import "time"

type Totals struct {
	Requests     int64    `json:"requests"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CacheRead    int64    `json:"cache_read"`
	CacheWrite   int64    `json:"cache_write"`
	CostUSD      *float64 `json:"cost_usd"`
}

type StatsSeries struct {
	InputTokens  []int64 `json:"input_tokens"`
	OutputTokens []int64 `json:"output_tokens"`
	CacheRead    []int64 `json:"cache_read"`
	CacheWrite   []int64 `json:"cache_write"`
	// Unknown prices add 0 here; the totals keep the null distinction.
	CostUSD []float64 `json:"cost_usd"`
}

type Stats struct {
	Totals     Totals            `json:"totals"`
	ByProvider map[string]Totals `json:"by_provider"`
	Start      time.Time         `json:"start"`
	StepSec    int64             `json:"step"`
	Series     StatsSeries       `json:"series"`
}

func (t *Totals) add(in, out, cr, cw int64, cost *float64) {
	t.Requests++
	t.InputTokens += in
	t.OutputTokens += out
	t.CacheRead += cr
	t.CacheWrite += cw
	// Unknown prices don't count as 0, otherwise a local model looks free instead of unknown.
	if cost != nil {
		sum := *cost
		if t.CostUSD != nil {
			sum += *t.CostUSD
		}
		t.CostUSD = &sum
	}
}

// Stats aggregates requests since since; buckets start at multiples of step.
func (s *Store) Stats(since time.Time, step time.Duration, now time.Time) (*Stats, error) {
	start := since.UTC().Truncate(step)
	n := int(now.Sub(start)/step) + 1
	st := &Stats{
		ByProvider: map[string]Totals{}, Start: start, StepSec: int64(step / time.Second),
		Series: StatsSeries{
			InputTokens: make([]int64, n), OutputTokens: make([]int64, n),
			CacheRead: make([]int64, n), CacheWrite: make([]int64, n), CostUSD: make([]float64, n),
		},
	}
	rows, err := s.db.Query(`SELECT ts, provider, input_tokens, output_tokens, cache_read, cache_write, cost_usd
FROM requests WHERE ts >= ? AND ts <= ?`, since.UTC().Unix(), now.UTC().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ts, in, out, cr, cw int64
		var provider string
		var cost *float64
		if err := rows.Scan(&ts, &provider, &in, &out, &cr, &cw, &cost); err != nil {
			return nil, err
		}
		st.Totals.add(in, out, cr, cw, cost)
		p := st.ByProvider[provider]
		p.add(in, out, cr, cw, cost)
		st.ByProvider[provider] = p
		if i := int(time.Unix(ts, 0).Sub(start) / step); i >= 0 && i < n {
			st.Series.InputTokens[i] += in
			st.Series.OutputTokens[i] += out
			st.Series.CacheRead[i] += cr
			st.Series.CacheWrite[i] += cw
			if cost != nil {
				st.Series.CostUSD[i] += *cost
			}
		}
	}
	return st, rows.Err()
}
