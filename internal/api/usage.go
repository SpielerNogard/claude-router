package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/SpielerNogard/claude-router/internal/store"
)

const (
	maxBuckets     = 2000
	maxMessageList = 500
)

// Allowed bucket lengths, used only for the upfront check; the store's conversion is authoritative.
var bucketSpans = map[string]time.Duration{
	"":     24 * time.Hour,
	"hour": time.Hour,
	"day":  24 * time.Hour,
	"week": 7 * 24 * time.Hour,
}

func Usage(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		to, err := parseDate(q.Get("to"), time.Now().UTC(), true)
		if err != nil {
			writeJSONError(w, 400, "to: "+err.Error())
			return
		}
		from, err := parseDate(q.Get("from"), to.AddDate(0, 0, -29), false)
		if err != nil {
			writeJSONError(w, 400, "from: "+err.Error())
			return
		}
		if from.After(to) {
			writeJSONError(w, 400, "from is after to")
			return
		}
		bucket := q.Get("bucket")
		span, ok := bucketSpans[bucket]
		if !ok {
			writeJSONError(w, 400, fmt.Sprintf("bucket %q: allowed are hour, day, week", bucket))
			return
		}
		if n := to.Sub(from)/span + 1; n > maxBuckets {
			writeJSONError(w, 400, fmt.Sprintf(
				"the range yields %d buckets, at most %d allowed — choose a coarser bucket or a shorter range",
				n, maxBuckets))
			return
		}
		if bucket == "" {
			bucket = "day"
		}
		u, err := s.Usage(from, to, bucket, q.Get("group_by"))
		if err != nil {
			writeJSONError(w, 400, err.Error())
			return
		}
		writeJSON(w, u)
	}
}

// Invalid input errors rather than silently falling back; endOfDay extends a plain date to its last nanosecond so a "to" bound keeps that day.
func parseDate(s string, def time.Time, endOfDay bool) (time.Time, error) {
	if s == "" {
		return def, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		t = t.UTC()
		if endOfDay {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%q is not a date in 2006-01-02 or RFC 3339 format", s)
}

func parseInt(s string, def, max int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
