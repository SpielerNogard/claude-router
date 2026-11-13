package discover

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func TestDiscoverReadsOpenAIModelList(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		io.WriteString(w, `{"data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"}]}`)
	}))
	defer up.Close()
	t.Setenv("K", "sk")

	got, err := Discover(context.Background(), up.Client(),
		config.Provider{Name: "ds", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "K"},
		http.Header{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "deepseek-chat" {
		t.Errorf("models = %v", got)
	}
}

func TestDiscoverErrorIsReturnedNotFatal(t *testing.T) {
	t.Setenv("K", "sk")
	_, err := Discover(context.Background(), http.DefaultClient,
		config.Provider{Name: "x", Format: "openai", Auth: "key", BaseURL: "http://127.0.0.1:1", APIKeyEnv: "K"},
		http.Header{})
	if err == nil {
		t.Fatal("unreachable provider must return an error")
	}
}

func TestFilterIncludeExclude(t *testing.T) {
	all := []string{"deepseek-chat", "deepseek-reasoner", "gpt-4o", "llama-3"}
	got := Filter(all, []string{"deepseek-*"}, nil)
	if len(got) != 2 {
		t.Errorf("include = %v", got)
	}
	got = Filter(all, nil, []string{"gpt-*", "llama-*"})
	if len(got) != 2 {
		t.Errorf("exclude = %v", got)
	}
	if len(Filter(all, nil, nil)) != 4 {
		t.Error("without filters everything must pass")
	}
}

func TestFilterHandlesSlashesInModelNames(t *testing.T) {
	all := []string{
		"openrouter/openai/gpt-4o",
		"openrouter/anthropic/claude-sonnet-4.5",
		"openrouter/deepseek/deepseek-chat",
		"gpt-4o",
	}
	if got := Filter(all, []string{"*/gpt-4o"}, nil); len(got) != 1 || got[0] != "openrouter/openai/gpt-4o" {
		t.Errorf("*/gpt-4o = %v — a star must also span slashes", got)
	}
	if got := Filter(all, []string{"openrouter/*"}, nil); len(got) != 3 {
		t.Errorf("openrouter/* = %v, expected all three", got)
	}
	if got := Filter(all, nil, []string{"*anthropic*"}); len(got) != 3 {
		t.Errorf("exclude *anthropic* = %v, expected three remaining", got)
	}
	if got := Filter(all, []string{"gpt-4o"}, nil); len(got) != 1 || got[0] != "gpt-4o" {
		t.Errorf("a pattern without a star must match exactly, got %v", got)
	}
}

func TestFilterIgnoresUnusableExcludePattern(t *testing.T) {
	all := []string{"a", "b"}
	// Everything except * is escaped, so a pattern can no longer be unusable.
	if got := Filter(all, nil, []string{"[unbalanced"}); len(got) != 2 {
		t.Errorf("= %v, expected both — the pattern literally matches nothing", got)
	}
}
