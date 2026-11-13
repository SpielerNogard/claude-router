package api

import (
	"encoding/json"
	"net/http"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func GetConfig(reg *config.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, config.Redact(reg.Get()))
	}
}

func PutConfig(reg *config.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var next config.Config
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&next); err != nil {
			writeJSONError(w, 400, "body is not valid JSON: "+err.Error())
			return
		}
		if err := reg.Replace(&next); err != nil {
			writeJSONError(w, 400, err.Error())
			return
		}
		writeJSON(w, config.Redact(reg.Get()))
	}
}
