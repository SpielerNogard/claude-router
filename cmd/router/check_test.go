package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func TestCheckReportsWorkingProvider(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer sk-real" {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, `{"data":[{"id":"a"},{"id":"b"}]}`)
	}))
	defer up.Close()
	t.Setenv("K", "sk-real")

	r := checkProvider(config.Provider{
		Name: "ds", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "K",
	})
	if !r.OK {
		t.Fatalf("provider should have worked: %+v", r)
	}
	if r.Models != 2 {
		t.Errorf("Models = %d, want 2", r.Models)
	}
}

func TestCheckReportsRejectedKey(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer up.Close()
	t.Setenv("K", "sk-wrong")

	r := checkProvider(config.Provider{
		Name: "ds", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "K",
	})
	if r.OK {
		t.Fatal("a rejected key must not count as working")
	}
	if !strings.Contains(r.Detail, "401") {
		t.Errorf("Detail = %q — the status belongs in the message", r.Detail)
	}
}

func TestCheckReportsUnreachableProvider(t *testing.T) {
	t.Setenv("K", "sk")
	r := checkProvider(config.Provider{
		Name: "dead", Format: "openai", Auth: "key", BaseURL: "http://127.0.0.1:1", APIKeyEnv: "K",
	})
	if r.OK {
		t.Fatal("an unreachable provider must not count as working")
	}
}

func TestCheckCannotVerifyPassthrough(t *testing.T) {
	r := checkProvider(config.Provider{Name: "subscription", Format: "anthropic", Auth: "passthrough"})
	if r.OK {
		t.Error("nothing can be confirmed without a credential")
	}
	if !r.Skipped {
		t.Error("a passthrough provider must be marked as uncheckable, not as broken")
	}
	if !strings.Contains(r.Detail, "passthrough") {
		t.Errorf("Detail = %q — the message should name the reason", r.Detail)
	}
}

func TestCheckFallsBackToCompletionWhenModelsMissing(t *testing.T) {
	var sawCompletion bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/models") {
			w.WriteHeader(404)
			return
		}
		sawCompletion = true
		if r.Header.Get("authorization") != "Bearer sk-real" {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"x"},
		  "finish_reason":"stop"}]}`)
	}))
	defer up.Close()
	t.Setenv("K", "sk-real")

	r := checkProvider(config.Provider{
		Name: "x", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "K",
		Models: []string{"some-model"},
	})
	if !sawCompletion {
		t.Fatal("without /v1/models check should have sent a request")
	}
	if !r.OK {
		t.Errorf("the fallback should have confirmed: %+v", r)
	}
}

func TestCheckWithoutModelsAndWithoutEndpointIsHonest(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer up.Close()
	t.Setenv("K", "sk")

	r := checkProvider(config.Provider{
		Name: "x", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "K",
	})
	if r.OK {
		t.Fatal("nothing may be confirmed without a way to check")
	}
	if !strings.Contains(r.Detail, "models") {
		t.Errorf("Detail = %q — the message should explain what is missing", r.Detail)
	}
}

func TestCheckReportsMissingEnvVar(t *testing.T) {
	r := checkProvider(config.Provider{
		Name: "x", Format: "openai", Auth: "key", BaseURL: "https://example.invalid", APIKeyEnv: "DOES_NOT_EXIST_XYZ",
	})
	if r.OK {
		t.Fatal("nothing may be confirmed without the variable set")
	}
	if !strings.Contains(r.Detail, "DOES_NOT_EXIST_XYZ") {
		t.Errorf("Detail = %q — the variable name belongs in the message", r.Detail)
	}
}

func TestCheckNeverLeaksTheKey(t *testing.T) {
	// Keys without a known prefix must be redacted too, not just pattern matches.
	for _, key := range []string{"sk-secret-0123456789abcdef", "inhouse_4711_no_prefix", "8f3c9a1b2d"} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"message":"Incorrect API key provided: `+key+`"}}`)
		}))
		t.Setenv("K", key)

		r := checkProvider(config.Provider{
			Name: "x", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "K",
		})
		up.Close()
		if strings.Contains(r.Detail, key) {
			t.Errorf("key %q appears in the output: %s", key, r.Detail)
		}
	}
}

func TestCheckWorksForLocalServerWithoutKey(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "" {
			t.Errorf("a local server must not see a credential: %q", r.Header.Get("authorization"))
		}
		io.WriteString(w, `{"data":[{"id":"qwen"},{"id":"bonsai"}]}`)
	}))
	defer up.Close()

	r := checkProvider(config.Provider{
		Name: "lmstudio", Format: "openai", Auth: "none", BaseURL: up.URL,
	})
	if !r.OK {
		t.Fatalf("a reachable local server must be confirmed: %+v", r)
	}
	if r.Models != 2 {
		t.Errorf("Models = %d, want 2", r.Models)
	}
}
