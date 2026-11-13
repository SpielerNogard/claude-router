package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/alias"
)

type fakeSource struct{ list []string }

func (f *fakeSource) Aliases() []string { return f.list }
func (f *fakeSource) RefreshKeyed()     {}

func TestModelsAreEncodedForThePicker(t *testing.T) {
	w := httptest.NewRecorder()
	Models(&fakeSource{list: []string{"deepseek:deepseek-flash", "sub:claude-opus-5"}})(
		w, httptest.NewRequest("GET", "/v1/models", nil))

	var v struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			CreatedAt   string `json:"created_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Data) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(v.Data))
	}
	for _, m := range v.Data {
		if !strings.HasPrefix(m.ID, "anthropic/claude-") {
			t.Errorf("id %q does not look like an Anthropic model — the picker drops it", m.ID)
		}
		back, ok := alias.Decode(m.ID)
		if !ok || back != m.DisplayName {
			t.Errorf("id %q does not decode to %q", m.ID, m.DisplayName)
		}
		if m.CreatedAt == "" {
			t.Errorf("created_at missing for %q", m.ID)
		}
	}
}
