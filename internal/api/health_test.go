package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHealthReportsBlockedProviders(t *testing.T) {
	// An active block parks the subscription for hours; without this it only shows up in the log.
	w := httptest.NewRecorder()
	Health(func() map[string]string {
		return map[string]string{"sub": "2026-09-25T15:57:27+02:00"}
	})(w, httptest.NewRequest("GET", "/healthz", nil))

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	blocked, ok := got["blocked"].(map[string]any)
	if !ok {
		t.Fatalf("blocked missing: %v", got)
	}
	if blocked["sub"] != "2026-09-25T15:57:27+02:00" {
		t.Errorf("blocked = %v", blocked)
	}
}

func TestHealthWithoutBlockedProviders(t *testing.T) {
	w := httptest.NewRecorder()
	Health(func() map[string]string { return nil })(w, httptest.NewRequest("GET", "/healthz", nil))

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v", got["status"])
	}
	if _, ok := got["blocked"]; ok {
		t.Error("blocked must not appear without a block")
	}
}
