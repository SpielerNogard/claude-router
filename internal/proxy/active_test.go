package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func accountsCfg() *config.Config {
	return &config.Config{Providers: map[string]config.Provider{
		"abo":      {Name: "abo", Format: "anthropic", Auth: "passthrough", Default: true, Fallback: "konto2"},
		"konto2":   {Name: "konto2", Format: "anthropic", Auth: "passthrough", Fallback: "deepseek:deepseek-flash"},
		"deepseek": {Name: "deepseek", Format: "openai", Auth: "none", BaseURL: "http://x"},
		"lmstudio": {Name: "lmstudio", Format: "openai", Auth: "none", BaseURL: "http://y"},
	}}
}

func TestAccounts(t *testing.T) {
	if got := Accounts(accountsCfg()); !reflect.DeepEqual(got, []string{"abo", "konto2"}) {
		t.Errorf("Accounts = %v, want [abo konto2]", got)
	}
	cfg := accountsCfg()
	abo := cfg.Providers["abo"]
	abo.Default = false
	cfg.Providers["abo"] = abo
	if got := Accounts(cfg); got != nil {
		t.Errorf("Accounts without default = %v, want nil", got)
	}
}

func TestReorderMovesActiveFirst(t *testing.T) {
	cfg := accountsCfg()
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	if err := px.SetActive("konto2"); err != nil {
		t.Fatal(err)
	}
	c := px.config()
	if !c.Providers["konto2"].Default || c.Providers["abo"].Default {
		t.Errorf("default not moved: abo=%v konto2=%v", c.Providers["abo"].Default, c.Providers["konto2"].Default)
	}
	if got := c.Providers["konto2"].Fallback; got != "abo" {
		t.Errorf("konto2.Fallback = %q, want abo", got)
	}
	if got := c.Providers["abo"].Fallback; got != "deepseek:deepseek-flash" {
		t.Errorf("abo.Fallback = %q, want the original tail", got)
	}
	if cfg.Providers["abo"].Fallback != "konto2" || !cfg.Providers["abo"].Default {
		t.Error("the registry's config was mutated")
	}
}

func TestReorderDefaultIsIdentity(t *testing.T) {
	cfg := accountsCfg()
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	if err := px.SetActive("abo"); err != nil {
		t.Fatal(err)
	}
	if px.config() != cfg {
		t.Error("choosing the config default must leave the config untouched")
	}
}

func TestSetActiveRejectsNonAccount(t *testing.T) {
	cfg := accountsCfg()
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	for _, name := range []string{"deepseek", "lmstudio", "nope"} {
		if err := px.SetActive(name); err == nil {
			t.Errorf("SetActive(%q) = nil, want error", name)
		}
	}
	if active, _ := px.Active(); active != "abo" {
		t.Errorf("Active = %q, want abo", active)
	}
}

func TestActiveClearedWhenConfigDropsIt(t *testing.T) {
	var cur atomic.Pointer[config.Config]
	cur.Store(accountsCfg())
	px := New(cur.Load, &nopRecorder{})
	if err := px.SetActive("konto2"); err != nil {
		t.Fatal(err)
	}
	next := accountsCfg()
	abo := next.Providers["abo"]
	abo.Fallback = ""
	next.Providers["abo"] = abo
	cur.Store(next)

	active, accounts := px.Active()
	if active != "abo" || !reflect.DeepEqual(accounts, []string{"abo"}) {
		t.Errorf("Active = %q, %v; want abo, [abo]", active, accounts)
	}
	if px.config() != next {
		t.Error("an account no longer in the chain must not reorder anything")
	}
	cur.Store(accountsCfg())
	if active, _ := px.Active(); active != "abo" {
		t.Errorf("Active after restoring the config = %q, want abo: a dropped account must stay cleared", active)
	}
}

// accountRig serves abo, konto2 and deepseek from test servers whose behaviour each test sets.
type accountRig struct {
	px    *Proxy
	rec   *nopRecorder
	limit map[string]bool
	calls map[string]int
	mu    sync.Mutex
}

func newAccountRig(t *testing.T) *accountRig {
	t.Helper()
	r := &accountRig{limit: map[string]bool{}, calls: map[string]int{}}
	url := func(name string) string {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			r.mu.Lock()
			r.calls[name]++
			limited := r.limit[name]
			r.mu.Unlock()
			if limited {
				rejectedLimit(w)
				return
			}
			io.WriteString(w, `{"id":"m","type":"message","role":"assistant","content":[],
			  "usage":{"input_tokens":3,"output_tokens":2}}`)
		}))
		t.Cleanup(s.Close)
		return s.URL
	}
	cfg := &config.Config{Providers: map[string]config.Provider{
		"abo": {Name: "abo", Format: "anthropic", Auth: "passthrough", BaseURL: url("abo"),
			Default: true, Fallback: "konto2"},
		"konto2": {Name: "konto2", Format: "anthropic", Auth: "passthrough", BaseURL: url("konto2"),
			Fallback: "deepseek:deepseek-flash"},
		"deepseek": {Name: "deepseek", Format: "anthropic", Auth: "passthrough", BaseURL: url("deepseek")},
	}}
	r.rec = &nopRecorder{}
	r.px = New(func() *config.Config { return cfg }, r.rec)
	return r
}

func (r *accountRig) setLimit(name string) {
	r.mu.Lock()
	r.limit[name] = true
	r.mu.Unlock()
}

func (r *accountRig) post(t *testing.T, model string) string {
	t.Helper()
	w := httptest.NewRecorder()
	r.px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"`+model+`","max_tokens":5,"messages":[]}`)))
	r.px.WaitForLogs()
	if w.Code != 200 {
		t.Fatalf("Status = %d, Body = %s", w.Code, w.Body.String())
	}
	return r.rec.last.Provider
}

func TestActiveRoutesUnprefixedAndFallsBack(t *testing.T) {
	r := newAccountRig(t)
	if err := r.px.SetActive("konto2"); err != nil {
		t.Fatal(err)
	}
	if got := r.post(t, "opus-5"); got != "konto2" {
		t.Fatalf("served by %q, want konto2", got)
	}
	r.setLimit("konto2")
	if got := r.post(t, "opus-5"); got != "abo" {
		t.Fatalf("konto2 limited: served by %q, want abo", got)
	}
	r.setLimit("abo")
	if got := r.post(t, "opus-5"); got != "deepseek" {
		t.Fatalf("both limited: served by %q, want deepseek", got)
	}
	if r.rec.last.UpstreamModel != "deepseek-flash" {
		t.Errorf("UpstreamModel = %q, want deepseek-flash", r.rec.last.UpstreamModel)
	}
}

func TestPrefixedFormerDefaultFallsToTail(t *testing.T) {
	r := newAccountRig(t)
	if err := r.px.SetActive("konto2"); err != nil {
		t.Fatal(err)
	}
	r.setLimit("abo")
	if got := r.post(t, "abo:opus-5"); got != "deepseek" {
		t.Errorf("served by %q, want deepseek", got)
	}
	if r.calls["abo"] != 1 {
		t.Errorf("abo called %d times, want 1: the prefixed provider is tried first", r.calls["abo"])
	}
	if r.calls["konto2"] != 0 {
		t.Errorf("konto2 called %d times, want 0: abo now falls back to the tail", r.calls["konto2"])
	}
}

func TestSetActiveConcurrentWithRequests(t *testing.T) {
	cfg := accountsCfg()
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = px.SetActive([]string{"abo", "konto2"}[i%2])
		}(i)
		go func() {
			defer wg.Done()
			_ = px.config().Providers["abo"].Fallback
			px.Active()
		}()
	}
	wg.Wait()
}

func TestActiveKeepsRequestedAlias(t *testing.T) {
	r := newAccountRig(t)
	if err := r.px.SetActive("konto2"); err != nil {
		t.Fatal(err)
	}
	if got := r.post(t, "opus-5"); got != "konto2" {
		t.Fatalf("served by %q, want konto2", got)
	}
	if r.rec.last.Alias != "abo:opus-5" {
		t.Errorf("Alias = %q, want abo:opus-5: usage by alias must not split with the switch", r.rec.last.Alias)
	}
}
