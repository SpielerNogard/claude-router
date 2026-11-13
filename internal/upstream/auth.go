package upstream

import (
	"net/http"
	"os"
	"strings"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/credstore"
)

// Never forwarded; without dropping accept-encoding Go won't decompress and usageSniffer can't find "usage" in the gzip body.
var hopHeaders = []string{"connection", "keep-alive", "transfer-encoding", "upgrade", "host", "content-length", "accept-encoding"}

// With auth=key any client-sent credential is dropped before the provider key is set.
func ApplyAuth(out http.Header, p config.Provider, incoming http.Header) error {
	if p.Auth == "claude-credential-helper" {
		tok, err := credstore.Token(credstore.ConfigDir(p.ConfigDir), p.Refresh)
		if err != nil {
			return err
		}
		out.Del("x-api-key")
		out.Set("authorization", "Bearer "+tok)
		// Keep all client betas plus oauth-2025-04-20, without which Anthropic rejects subscription tokens.
		out.Set("anthropic-beta", mergeBetas(incoming.Values("anthropic-beta"), "oauth-2025-04-20"))
		out.Set("anthropic-version", firstNonEmpty(incoming.Get("anthropic-version"), "2023-06-01"))
		return nil
	}
	// Local servers without auth: set nothing and strip any client-sent credential.
	if p.Auth == "none" {
		out.Del("authorization")
		out.Del("x-api-key")
		return nil
	}
	if p.Auth == "passthrough" {
		// Only add what CopyForwardableHeaders didn't copy; a Set would drop all but the first anthropic-beta value.
		for _, k := range []string{"authorization", "x-api-key", "anthropic-beta", "anthropic-version"} {
			if out.Get(k) != "" {
				continue
			}
			for _, v := range incoming.Values(k) {
				out.Add(k, v)
			}
		}
		return nil
	}
	out.Del("authorization")
	out.Del("x-api-key")
	key := os.Getenv(p.APIKeyEnv)
	if p.Format == "anthropic" {
		out.Set("x-api-key", key)
		if v := incoming.Get("anthropic-version"); v != "" {
			out.Set("anthropic-version", v)
		} else {
			out.Set("anthropic-version", "2023-06-01")
		}
		return nil
	}
	out.Set("authorization", "Bearer "+key)
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func CopyForwardableHeaders(out, in http.Header) {
	skip := map[string]bool{}
	for _, h := range hopHeaders {
		skip[h] = true
	}
	for _, h := range []string{"authorization", "x-api-key"} {
		skip[h] = true
	}
	for k, vs := range in {
		if skip[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			out.Add(k, v)
		}
	}
}

// mergeBetas merges anthropic-beta headers into one deduplicated list and adds the required ones.
func mergeBetas(existing []string, required ...string) string {
	seen := map[string]bool{}
	var all []string
	add := func(raw string) {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			all = append(all, part)
		}
	}
	for _, v := range existing {
		add(v)
	}
	for _, v := range required {
		add(v)
	}
	return strings.Join(all, ",")
}
