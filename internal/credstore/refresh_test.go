package credstore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeRenewable(t *testing.T, dir string, expires time.Time) string {
	t.Helper()
	path := filepath.Join(dir, ".credentials.json")
	raw, _ := json.Marshal(map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "old",
			"refreshToken":     "old-rt",
			"expiresAt":        expires.UnixMilli(),
			"scopes":           []string{"user:inference", "user:profile"},
			"subscriptionType": "max",
		},
		"otherData": 1,
	})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeTokenEndpoint(t *testing.T, h http.HandlerFunc) *int {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	old := tokenURL
	tokenURL = srv.URL
	t.Cleanup(func() { tokenURL = old })
	return &calls
}

func TestRenewWritesBackAndKeepsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := writeRenewable(t, dir, time.Now().Add(time.Minute))
	fakeTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		var got map[string]string
		json.NewDecoder(r.Body).Decode(&got)
		want := map[string]string{"grant_type": "refresh_token", "refresh_token": "old-rt",
			"client_id": clientID, "scope": "user:inference user:profile"}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("Request %s = %q, want %q", k, got[k], v)
			}
		}
		w.Write([]byte(`{"access_token":"new","refresh_token":"new-rt","expires_in":3600}`))
	})

	tok, err := Token(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "new" {
		t.Errorf("Token = %q, want new", tok)
	}
	raw, _ := os.ReadFile(path)
	var doc struct {
		ClaudeAiOauth map[string]any `json:"claudeAiOauth"`
		OtherData     int            `json:"otherData"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	o := doc.ClaudeAiOauth
	if o["accessToken"] != "new" || o["refreshToken"] != "new-rt" {
		t.Errorf("written back: %v", o)
	}
	if o["subscriptionType"] != "max" || doc.OtherData != 1 {
		t.Errorf("unknown fields lost: %s", raw)
	}
	if exp := time.UnixMilli(int64(o["expiresAt"].(float64))); time.Until(exp) < 50*time.Minute {
		t.Errorf("expiresAt = %v, want about one hour from now", exp)
	}
}

func TestNoRenewWithoutRefreshFlag(t *testing.T) {
	dir := t.TempDir()
	writeRenewable(t, dir, time.Now().Add(time.Minute))
	calls := fakeTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) {})

	tok, err := Token(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "old" || *calls != 0 {
		t.Errorf("Token = %q, calls = %d — without refresh nothing may be renewed", tok, *calls)
	}
}

func TestRejectedRenewLeavesCredentialUntouched(t *testing.T) {
	dir := t.TempDir()
	path := writeRenewable(t, dir, time.Now().Add(time.Minute))
	before, _ := os.ReadFile(path)
	fakeTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	if _, err := Token(dir, true); err == nil {
		t.Fatal("a rejected renewal must surface as an error")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("the file was changed despite the rejection")
	}
}
