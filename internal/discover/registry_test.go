package discover

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStaticModelsNeedNoDiscovery(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", Models: []string{"opus-5", "sonnet-5"}},
	}}
	r := NewRegistry(func() *config.Config { return cfg }, newStore(t))
	got := r.Aliases()
	if len(got) != 2 || got[0] != "sub:opus-5" {
		t.Errorf("Aliases = %v, want [sub:opus-5 sub:sonnet-5]", got)
	}
}

func TestRefreshKeyedFiltersAndPersists(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"id":"deepseek-chat"},{"id":"gpt-4o"}]}`)
	}))
	defer up.Close()
	t.Setenv("K", "sk")

	s := newStore(t)
	cfg := &config.Config{Providers: map[string]config.Provider{
		"ds": {Name: "ds", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "K", Include: []string{"deepseek-*"}},
	}}
	r := NewRegistry(func() *config.Config { return cfg }, s)
	r.RefreshKeyed()

	if got := r.Aliases(); len(got) != 1 || got[0] != "ds:deepseek-chat" {
		t.Errorf("Aliases = %v — include must filter out gpt-4o", got)
	}
	saved, err := s.Models()
	if err != nil || len(saved["ds"]) != 1 {
		t.Errorf("list was not saved: %v %v", saved, err)
	}
}

func TestExtraModelsComeOnTopOfDiscovery(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"id":"deepseek-flash"},{"id":"deepseek-v4-pro"}]}`)
	}))
	defer up.Close()
	t.Setenv("K", "sk")

	cfg := &config.Config{Providers: map[string]config.Provider{
		"ds": {Name: "ds", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "K",
			// deepseek-flash is listed in addition to discovery and must appear only once.
			ExtraModels: []string{"deepseek-flash", "deepseek-flash[1m]"}},
	}}
	r := NewRegistry(func() *config.Config { return cfg }, newStore(t))
	r.RefreshKeyed()

	want := []string{"ds:deepseek-flash", "ds:deepseek-flash[1m]", "ds:deepseek-v4-pro"}
	if got := r.Aliases(); !slices.Equal(got, want) {
		t.Errorf("Aliases = %v, want %v — extra_models is added to discovery", got, want)
	}
}

func TestRefreshKeyedProbesCredentialFreeProviders(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "" {
			t.Errorf("local provider must not get a credential, got %q", got)
		}
		io.WriteString(w, `{"data":[{"id":"ternary-bonsai-2-27b"},{"id":"text-embedding-nomic"}]}`)
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"lmstudio": {Name: "lmstudio", Format: "openai", Auth: "none", BaseURL: up.URL, Exclude: []string{"text-embedding*"}},
	}}
	r := NewRegistry(func() *config.Config { return cfg }, newStore(t))
	r.RefreshKeyed()

	if got := r.Aliases(); len(got) != 1 || got[0] != "lmstudio:ternary-bonsai-2-27b" {
		t.Errorf("Aliases = %v — auth=none must be discovered without a client request", got)
	}
}

func TestRefreshKeyedSkipsPassthroughWithoutCredential(t *testing.T) {
	var calls int
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"data":[{"id":"claude-opus-5"}]}`)
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	r := NewRegistry(func() *config.Config { return cfg }, newStore(t))
	r.RefreshKeyed()

	if calls != 0 {
		t.Errorf("passthrough without an in-flight request must not be probed, ran %d times", calls)
	}
}

func TestDeadProviderDoesNotBreakRegistry(t *testing.T) {
	t.Setenv("K", "sk")
	cfg := &config.Config{Providers: map[string]config.Provider{
		"dead": {Name: "dead", Format: "openai", Auth: "key", BaseURL: "http://127.0.0.1:1", APIKeyEnv: "K"},
		"fix":  {Name: "fix", Format: "anthropic", Auth: "passthrough", Models: []string{"opus-5"}},
	}}
	r := NewRegistry(func() *config.Config { return cfg }, newStore(t))
	r.RefreshKeyed()
	if got := r.Aliases(); len(got) != 1 || got[0] != "fix:opus-5" {
		t.Errorf("a dead provider must not break the list: %v", got)
	}
}

func TestNoteCredentialDiscoversPassthroughOnce(t *testing.T) {
	var calls int
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("authorization") != "Bearer oauth" {
			t.Errorf("the in-flight credential must be used, got %q", r.Header.Get("authorization"))
		}
		io.WriteString(w, `{"data":[{"id":"claude-opus-5"}]}`)
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	r := NewRegistry(func() *config.Config { return cfg }, newStore(t))

	h := http.Header{}
	h.Set("authorization", "Bearer oauth")
	r.NoteCredential("sub", h)
	r.NoteCredential("sub", h)
	r.wait()

	if calls != 1 {
		t.Errorf("discovery ran %d times, expected exactly once", calls)
	}
	if got := r.Aliases(); len(got) != 1 || got[0] != "sub:claude-opus-5" {
		t.Errorf("Aliases = %v", got)
	}
}

func TestFailedDiscoveryDoesNotLockProviderForever(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: "http://127.0.0.1:1"},
	}}
	r := NewRegistry(func() *config.Config { return cfg }, newStore(t))

	h := http.Header{}
	h.Set("authorization", "Bearer x")
	r.NoteCredential("sub", h)
	r.wait()

	if _, probed := r.probed.Load("sub"); probed {
		t.Fatal("a single failed attempt must not lock the provider permanently")
	}
}

func TestRegistryDoesNotReuseCredential(t *testing.T) {
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("authorization"))
		io.WriteString(w, `{"data":[{"id":"m"}]}`)
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	r := NewRegistry(func() *config.Config { return cfg }, newStore(t))

	h := http.Header{}
	h.Set("authorization", "Bearer secret")
	r.NoteCredential("sub", h)
	r.wait()

	r.probed.Delete("sub") // allow a second attempt
	r.NoteCredential("sub", http.Header{})
	r.wait()

	if len(seen) != 2 {
		t.Fatalf("expected two requests, got %d", len(seen))
	}
	if seen[0] != "Bearer secret" {
		t.Errorf("first request carried %q, expected the forwarded credential", seen[0])
	}
	if seen[1] != "" {
		t.Errorf("second request carried %q — the registry kept the first call's credential", seen[1])
	}
}
