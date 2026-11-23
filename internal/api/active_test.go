package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type fakeSwitcher struct{ active string }

func (f *fakeSwitcher) Active() (string, []string) { return f.active, []string{"abo", "konto2"} }

func (f *fakeSwitcher) SetActive(name string) error {
	if !slices.Contains([]string{"abo", "konto2"}, name) {
		return fmt.Errorf("unknown account %q", name)
	}
	f.active = name
	return nil
}

func decodeActive(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestGetActive(t *testing.T) {
	w := httptest.NewRecorder()
	GetActive(&fakeSwitcher{active: "abo"})(w, httptest.NewRequest("GET", "/api/active", nil))
	got := decodeActive(t, w)
	want := map[string]any{"active": "abo", "accounts": []any{"abo", "konto2"}}
	if w.Code != 200 || !reflect.DeepEqual(got, want) {
		t.Errorf("%d %v, want 200 %v", w.Code, got, want)
	}
}

func TestPutActive(t *testing.T) {
	s := &fakeSwitcher{active: "abo"}
	w := httptest.NewRecorder()
	PutActive(s)(w, httptest.NewRequest("PUT", "/api/active", strings.NewReader(`{"provider":"konto2"}`)))
	if w.Code != 200 || s.active != "konto2" || decodeActive(t, w)["active"] != "konto2" {
		t.Errorf("%d active=%q body=%s", w.Code, s.active, w.Body.String())
	}
}

func TestPutActiveUnknown(t *testing.T) {
	w := httptest.NewRecorder()
	PutActive(&fakeSwitcher{active: "abo"})(w, httptest.NewRequest("PUT", "/api/active", strings.NewReader(`{"provider":"x"}`)))
	if w.Code != 400 || decodeActive(t, w)["error"] != `unknown account "x"` {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
}

func TestPutActiveBadJSON(t *testing.T) {
	w := httptest.NewRecorder()
	PutActive(&fakeSwitcher{active: "abo"})(w, httptest.NewRequest("PUT", "/api/active", strings.NewReader(`nope`)))
	if w.Code != 400 {
		t.Errorf("Status = %d, want 400", w.Code)
	}
}
