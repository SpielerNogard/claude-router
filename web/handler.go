package web

import (
	"io"
	"io/fs"
	"net/http"
)

// Handler serves the UI under /ui/, or a hint instead of an empty page when it wasn't built (no make ui, or UI=0).
func Handler(dist fs.FS) http.Handler {
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, "UI not built — run make ui (or make build without UI=0)\n")
		})
	}
	return http.StripPrefix("/ui/", http.FileServerFS(dist))
}
