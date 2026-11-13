package api

import (
	"net/http"
	"time"

	"github.com/SpielerNogard/claude-router/internal/store"
)

func Requests(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		since, err := parseDate(q.Get("since"), time.Time{}, false)
		if err != nil {
			writeJSONError(w, 400, "since: "+err.Error())
			return
		}
		entries, total, err := s.Recent(store.RequestFilter{
			After:      int64(parseInt(q.Get("after"), 0, 1<<62)),
			Since:      since,
			Provider:   q.Get("provider"),
			Model:      q.Get("model"),
			ErrorsOnly: q.Get("errors") == "1",
			Limit:      parseInt(q.Get("limit"), 100, maxMessageList),
			Offset:     parseInt(q.Get("offset"), 0, 1<<30),
		})
		if err != nil {
			writeJSONError(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"entries": entries, "total": total})
	}
}
