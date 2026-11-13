package api

import (
	"encoding/json"
	"github.com/SpielerNogard/claude-router/internal/alias"
	"net/http"
	"sort"
)

type ModelSource interface {
	Aliases() []string
	RefreshKeyed()
}

func Models(src ModelSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		aliases := src.Aliases()
		sort.Strings(aliases)
		// The picker only accepts IDs that look like Anthropic models; the real alias is the display name and travels encoded in the id.
		data := make([]map[string]string, 0, len(aliases))
		for _, a := range aliases {
			data = append(data, map[string]string{
				"id":           alias.Encode(a),
				"type":         "model",
				"display_name": a,
				"created_at":   "2026-01-01T00:00:00Z",
			})
		}
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data, "has_more": false})
	}
}

// RefreshKeyed can take up to 15 seconds per provider, so the handler only triggers it instead of blocking the request.
func RefreshModels(src ModelSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		go src.RefreshKeyed()
		w.WriteHeader(http.StatusAccepted)
	}
}
