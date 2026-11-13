package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// statusWriter remembers the status code the handler wrote.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = 200
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withDebug logs every request to show which endpoints a client actually hits.
func withDebug(enabled bool, next http.Handler) http.Handler {
	if !enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = 200
		}
		fmt.Fprintf(os.Stderr, "%s  %-5s %-32s %d  %d ms\n",
			started.Format("15:04:05"), r.Method, r.URL.Path, status, time.Since(started).Milliseconds())
	})
}
