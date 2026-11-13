package store

import (
	"fmt"
	"log"
	"sort"
	"time"
)

type Series struct {
	Key        string    `json:"key"`
	Billed     bool      `json:"billed"`
	CostUSD    []float64 `json:"cost_usd"`
	TokensIn   []int64   `json:"tokens_in"`
	TokensOut  []int64   `json:"tokens_out"`
	CacheRead  []int64   `json:"cache_read"`
	CacheWrite []int64   `json:"cache_write"`
	Requests   []int64   `json:"requests"`
}

type Usage struct {
	Buckets []string `json:"buckets"`
	Series  []Series `json:"series"`
}

var groupColumns = map[string]string{
	"":         "''",
	"provider": "provider",
	"model":    "upstream_model",
	"alias":    "alias",
	"client":   "client",
}

// Only these column names reach the SQL — group_by comes from a query parameter.
func (s *Store) Usage(from, to time.Time, bucket, groupBy string) (*Usage, error) {
	col, ok := groupColumns[groupBy]
	if !ok {
		return nil, fmt.Errorf("group_by %q: allowed are provider, model, alias, client or empty", groupBy)
	}
	step, layout, trunc, err := bucketSpec(bucket)
	if err != nil {
		return nil, err
	}

	buckets, index := buildBuckets(trunc(from.UTC()), trunc(to.UTC()), step, layout)

	rows, err := s.db.Query(fmt.Sprintf(`
SELECT ts, %s, auth_mode, COALESCE(cost_usd,0), input_tokens, output_tokens, cache_read, cache_write
FROM requests WHERE ts >= ? AND ts <= ?`, col),
		trunc(from.UTC()).Unix(), to.UTC().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type key struct {
		name   string
		billed bool
	}
	acc := map[key]*Series{}
	var order []key

	for rows.Next() {
		var ts int64
		var name, authMode string
		var cost float64
		var in, out, cr, cw int64
		if err := rows.Scan(&ts, &name, &authMode, &cost, &in, &out, &cr, &cw); err != nil {
			return nil, err
		}
		slot, ok := index[trunc(time.Unix(ts, 0).UTC()).Format(layout)]
		if !ok {
			log.Printf("usage: row with ts=%d (%s) is outside the buckets, skipped", ts, name)
			continue
		}
		k := key{name, authMode == "key"}
		se := acc[k]
		if se == nil {
			se = &Series{
				Key: name, Billed: k.billed,
				CostUSD:    make([]float64, len(buckets)),
				TokensIn:   make([]int64, len(buckets)),
				TokensOut:  make([]int64, len(buckets)),
				Requests:   make([]int64, len(buckets)),
				CacheRead:  make([]int64, len(buckets)),
				CacheWrite: make([]int64, len(buckets)),
			}
			acc[k] = se
			order = append(order, k)
		}
		se.CostUSD[slot] += cost
		se.TokensIn[slot] += in
		se.TokensOut[slot] += out
		se.CacheRead[slot] += cr
		se.CacheWrite[slot] += cw
		se.Requests[slot]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	u := &Usage{Buckets: buckets}
	for _, k := range order {
		u.Series = append(u.Series, *acc[k])
	}
	// Without a fixed series order the frontend chart colors would jump between calls, since row order depends on SQLite.
	sort.Slice(u.Series, func(i, j int) bool { return u.Series[i].Key < u.Series[j].Key })
	if u.Series == nil {
		u.Series = []Series{}
	}
	return u, nil
}

func bucketSpec(bucket string) (step time.Duration, layout string, trunc func(time.Time) time.Time, err error) {
	switch bucket {
	case "hour":
		return time.Hour, "2006-01-02T15", func(t time.Time) time.Time { return t.Truncate(time.Hour) }, nil
	case "day", "":
		return 24 * time.Hour, "2006-01-02", func(t time.Time) time.Time {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		}, nil
	case "week":
		return 7 * 24 * time.Hour, "2006-01-02", func(t time.Time) time.Time {
			d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			return d.AddDate(0, 0, -(int(d.Weekday())+6)%7)
		}, nil
	}
	return 0, "", nil, fmt.Errorf("bucket %q: allowed are hour, day, week", bucket)
}

func buildBuckets(from, to time.Time, step time.Duration, layout string) ([]string, map[string]int) {
	var buckets []string
	index := map[string]int{}
	for t := from; !t.After(to); t = t.Add(step) {
		index[t.Format(layout)] = len(buckets)
		buckets = append(buckets, t.Format(layout))
	}
	if buckets == nil {
		buckets = []string{}
	}
	return buckets, index
}
