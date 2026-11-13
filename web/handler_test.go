package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerWithoutBuildExplains(t *testing.T) {
	w := httptest.NewRecorder()
	Handler(fstest.MapFS{".gitkeep": {}}).ServeHTTP(w, httptest.NewRequest("GET", "/ui/", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "make ui") {
		t.Errorf("Status %d, Body %q", w.Code, w.Body.String())
	}
}

func TestHandlerServesBuild(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":    {Data: []byte("<div id=app></div>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	for path, want := range map[string]string{"/ui/": "id=app", "/ui/assets/app.js": "console.log"} {
		w := httptest.NewRecorder()
		Handler(dist).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: Status %d, Body %q", path, w.Code, w.Body.String())
		}
	}
}
