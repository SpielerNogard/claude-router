package api

import (
	"net/http"
	"strconv"

	"github.com/SpielerNogard/claude-router/internal/store"
)

func Messages(l *store.MsgLog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := parseInt(r.URL.Query().Get("limit"), 50, maxMessageList)
		offset := parseInt(r.URL.Query().Get("offset"), 0, 1<<30)
		entries, err := l.List(limit, offset)
		if err != nil {
			writeJSONError(w, 500, err.Error())
			return
		}
		writeJSON(w, map[string]any{"entries": entries})
	}
}

func MessageDetail(l *store.MsgLog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeJSONError(w, 400, "id must be a number")
			return
		}
		docs, err := l.Get(id)
		if err != nil {
			writeJSONError(w, 500, err.Error())
			return
		}
		writeDocs(w, docs)
	}
}

func MessagesByRequest(l *store.MsgLog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		docs, err := l.GetByRequestID(r.PathValue("request_id"))
		if err != nil {
			writeJSONError(w, 500, err.Error())
			return
		}
		writeDocs(w, docs)
	}
}

func writeDocs(w http.ResponseWriter, docs map[string][]byte) {
	out := map[string]string{}
	for k, v := range docs {
		out[k] = string(v)
	}
	writeJSON(w, out)
}
