package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/upstream"
)

type checkResult struct {
	Name    string
	OK      bool
	Skipped bool
	Models  int
	Took    time.Duration
	Detail  string
}

var checkClient = &http.Client{Timeout: 15 * time.Second}

// Same key shapes as in the proxy; providers sometimes echo the sent key in error messages.
const minKeyLen = 8

var checkSecretPattern = regexp.MustCompile(
	`(?:\b(?:sk|rk|gsk|xai|pplx|ghp|glpat|shpat)[-_][A-Za-z0-9_-]{8,})` +
		`|(?:\bAIza[A-Za-z0-9_-]{30,})` +
		`|(?:\beyJ[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,})`)

func checkProvider(p config.Provider) checkResult {
	r := checkResult{Name: p.Name}
	if p.Auth == "passthrough" {
		r.Skipped = true
		r.Detail = "auth = passthrough — the router holds no credential, checkable only on the first request"
		return r
	}
	// Only auth=key needs an env var; ApplyAuth loads the subscription credential itself.
	key := ""
	if p.Auth == "key" {
		key = os.Getenv(p.APIKeyEnv)
		if key == "" {
			r.Detail = fmt.Sprintf("environment variable %s is empty or not set", p.APIKeyEnv)
			return r
		}
	}
	redact := func(s string) string { return redactKey(s, key) }

	started := time.Now()
	n, status, err := probeModels(p)
	r.Took = time.Since(started)

	switch {
	case err != nil:
		r.Detail = redact(err.Error())
	case status == 200:
		r.OK, r.Models = true, n
	case status == 404 || status == 405:
		// Not every provider has /v1/models — then only a real request remains.
		r.OK, r.Detail = probeCompletion(p, redact)
		r.Took = time.Since(started)
	default:
		r.Detail = fmt.Sprintf("%s responded %d", modelsURL(p), status)
	}
	return r
}

func baseURL(p config.Provider) string {
	if p.BaseURL != "" {
		return strings.TrimSuffix(p.BaseURL, "/")
	}
	return "https://api.anthropic.com"
}

func modelsURL(p config.Provider) string { return baseURL(p) + "/v1/models" }

func probeModels(p config.Provider) (models int, status int, err error) {
	req, err := http.NewRequest("GET", modelsURL(p), nil)
	if err != nil {
		return 0, 0, err
	}
	if err := upstream.ApplyAuth(req.Header, p, http.Header{}); err != nil {
		return 0, 0, err
	}

	resp, err := checkClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, resp.StatusCode, nil
	}
	var v struct {
		Data []struct{} `json:"data"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, resp.StatusCode, fmt.Errorf("unreadable model list: %w", err)
	}
	return len(v.Data), 200, nil
}

// Fallback for providers without /v1/models: a one-token request using the first configured model.
func probeCompletion(p config.Provider, redact func(string) string) (ok bool, detail string) {
	if len(p.Models) == 0 {
		return false, fmt.Sprintf(
			"%s does not exist, and without models = [...] in the configuration there is no model name to test with", modelsURL(p))
	}
	model := p.Models[0]

	var url string
	var body []byte
	if p.Format == "anthropic" {
		url = baseURL(p) + "/v1/messages"
		body = []byte(fmt.Sprintf(
			`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`, model))
	} else {
		url = baseURL(p) + "/v1/chat/completions"
		body = []byte(fmt.Sprintf(
			`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`, model))
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return false, redact(err.Error())
	}
	req.Header.Set("content-type", "application/json")
	if err := upstream.ApplyAuth(req.Header, p, http.Header{}); err != nil {
		return false, redact(err.Error())
	}

	resp, err := checkClient.Do(req)
	if err != nil {
		return false, redact(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		return true, fmt.Sprintf("confirmed via a test request to %s", model)
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	return false, redact(fmt.Sprintf("%s responded %d: %s", url, resp.StatusCode, snippet(raw)))
}

func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// The actual key is replaced literally; the pattern list alone would miss unknown prefixes.
func redactKey(s, key string) string {
	// Short values would match everywhere and mangle the message.
	if len(key) >= minKeyLen {
		s = strings.ReplaceAll(s, key, "[redacted]")
	}
	return checkSecretPattern.ReplaceAllString(s, "[redacted]")
}

// runCheck checks all providers (or only one) and returns true if none failed.
func runCheck(cfg *config.Config, only string, out io.Writer) bool {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		if only == "" || name == only {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	if len(names) == 0 {
		fmt.Fprintf(out, "Provider %q is not in the configuration.\n", only)
		return false
	}

	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}

	allOK := true
	for _, name := range names {
		r := checkProvider(cfg.Providers[name])
		padded := fmt.Sprintf("%-*s", width, name)
		switch {
		case r.Skipped:
			fmt.Fprintf(out, "%s  %s  %s\n", paint(styleName, padded), paint(styleSkip, "–"), paint(styleDim, r.Detail))
		case r.OK:
			detail := r.Detail
			if detail == "" {
				detail = fmt.Sprintf("%d models", r.Models)
			}
			fmt.Fprintf(out, "%s  %s  %s %s\n", paint(styleName, padded), paint(styleOK, "✓"),
				detail, paint(styleDim, fmt.Sprintf("(%d ms)", r.Took.Milliseconds())))
		default:
			allOK = false
			fmt.Fprintf(out, "%s  %s  %s\n", paint(styleName, padded), paint(styleFail, "✗"), r.Detail)
		}
	}
	return allOK
}
