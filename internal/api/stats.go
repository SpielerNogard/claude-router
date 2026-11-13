package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/SpielerNogard/claude-router/internal/store"
)

func Stats(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		since, err := time.Parse(time.RFC3339, q.Get("since"))
		if err != nil {
			writeJSONError(w, 400, "since: expected an RFC 3339 timestamp")
			return
		}
		sec, err := strconv.Atoi(q.Get("step"))
		if err != nil || sec < 60 {
			writeJSONError(w, 400, "step: seconds, at least 60")
			return
		}
		now, step := time.Now(), time.Duration(sec)*time.Second
		if n := now.Sub(since)/step + 1; n > maxBuckets {
			writeJSONError(w, 400, fmt.Sprintf("the range yields %d buckets, at most %d allowed", n, maxBuckets))
			return
		}
		st, err := s.Stats(since, step, now)
		if err != nil {
			writeJSONError(w, 500, err.Error())
			return
		}
		writeJSON(w, st)
	}
}
