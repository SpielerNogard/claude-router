package api

import (
	"encoding/json"
	"net/http"
)

type ActiveSwitcher interface {
	Active() (string, []string)
	SetActive(string) error
}

func writeActive(w http.ResponseWriter, s ActiveSwitcher) {
	active, accounts := s.Active()
	writeJSON(w, map[string]any{"active": active, "accounts": accounts})
}

func GetActive(s ActiveSwitcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { writeActive(w, s) }
}

func PutActive(s ActiveSwitcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
			writeJSONError(w, 400, "body is not valid JSON: "+err.Error())
			return
		}
		if err := s.SetActive(body.Provider); err != nil {
			writeJSONError(w, 400, err.Error())
			return
		}
		writeActive(w, s)
	}
}
