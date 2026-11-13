package proxy

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Claude Code evaluates the same headers; without them there is no evidence
// of how a limit was detected.
const limitHeaderPrefix = "anthropic-ratelimit-unified-"

// limitHeaders collects a response's limit headers for the message log.
// nil if none match, otherwise every error response would get an empty document.
func limitHeaders(h http.Header) []byte {
	out := map[string]string{}
	for k, vs := range h {
		lk := strings.ToLower(k)
		if len(vs) == 0 {
			continue
		}
		if strings.HasPrefix(lk, limitHeaderPrefix) || lk == "retry-after" {
			out[lk] = vs[0]
		}
	}
	if len(out) == 0 {
		return nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return b
}
