package api

import "net/http"

func Limits(get func() any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { writeJSON(w, get()) }
}
