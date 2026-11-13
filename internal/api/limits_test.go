package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitsWritesProviderView(t *testing.T) {
	w := httptest.NewRecorder()
	Limits(func() any { return map[string]any{"deepseek": map[string]string{"kind": "unlimited"}} })(
		w, httptest.NewRequest("GET", "/api/limits", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deepseek":{"kind":"unlimited"}`) {
		t.Errorf("Status %d, Body %s", w.Code, w.Body.String())
	}
}
