package api

import "net/http"

// blocked returns the exhausted providers with their reset time; nil if none is blocked.
func Health(blocked func() map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"status": "ok"}
		if blocked != nil {
			if b := blocked(); len(b) > 0 {
				body["blocked"] = b
			}
		}
		writeJSON(w, body)
	}
}
