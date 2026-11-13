package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/upstream"
)

// auth is the header of an in-flight request — for passthrough providers the only source of a valid credential; used, never stored.
func Discover(ctx context.Context, c *http.Client, p config.Provider, auth http.Header) ([]string, error) {
	base := p.BaseURL
	if base == "" && p.Format == "anthropic" {
		base = "https://api.anthropic.com"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(base, "/")+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if err := upstream.ApplyAuth(req.Header, p, auth); err != nil {
		return nil, err
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("provider %s: /v1/models returned %d", p.Name, resp.StatusCode)
	}
	var v struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("provider %s: unreadable model list: %w", p.Name, err)
	}
	out := make([]string, 0, len(v.Data))
	for _, d := range v.Data {
		if d.ID != "" {
			out = append(out, d.ID)
		}
	}
	return out, nil
}

// Not path.Match: it treats / as a separator, so * wouldn't span segments in names like openrouter/openai/gpt-4o.
func compileGlob(pattern string) (*regexp.Regexp, error) {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.Compile("^" + strings.Join(parts, ".*") + "$")
}

func compileAll(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := compileGlob(p)
		if err != nil {
			log.Printf("discovery: pattern %q unusable, skipped: %v", p, err)
			continue
		}
		out = append(out, re)
	}
	return out
}

func Filter(models, include, exclude []string) []string {
	inc := compileAll(include)
	exc := compileAll(exclude)
	out := make([]string, 0, len(models))
	for _, m := range models {
		if len(inc) > 0 && !matchAny(inc, m) {
			continue
		}
		if matchAny(exc, m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func matchAny(patterns []*regexp.Regexp, s string) bool {
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
