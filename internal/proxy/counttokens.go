package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/SpielerNogard/claude-router/internal/convert"
	"github.com/SpielerNogard/claude-router/internal/upstream"
)

// Claude Code aborts if this endpoint fails, so every path ends with 200.
func (px *Proxy) CountTokens(w http.ResponseWriter, r *http.Request) {
	body, err := readLimited(r)
	if err != nil {
		estimateResponse(w, nil)
		return
	}
	var a convert.AnthropicRequest
	if err := json.Unmarshal(body, &a); err != nil {
		estimateResponse(w, body)
		return
	}
	tgt, rerr := Resolve(px.cfg(), a.Model)
	if rerr != nil || tgt.Provider.Format != "anthropic" {
		estimateResponse(w, body)
		return
	}
	if answer, ok := px.countTokensUpstream(r, tgt, body); ok {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(200)
		w.Write(answer)
		return
	}
	estimateResponse(w, body)
}

// Returns the provider's answer only if it responded with 200; otherwise we estimate.
func (px *Proxy) countTokensUpstream(r *http.Request, tgt *Target, body []byte) ([]byte, bool) {
	base := tgt.Provider.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	req, err := http.NewRequestWithContext(r.Context(), "POST",
		strings.TrimSuffix(base, "/")+"/v1/messages/count_tokens",
		bytes.NewReader(replaceModel(body, tgt.UpstreamModel)))
	if err != nil {
		return nil, false
	}
	upstream.CopyForwardableHeaders(req.Header, r.Header)
	if err := upstream.ApplyAuth(req.Header, tgt.Provider, r.Header); err != nil {
		return nil, false
	}
	req.Header.Set("content-type", "application/json")

	resp, err := upstream.Send(px.client, req, tgt.Provider)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false
	}
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || !json.Valid(answer) {
		return nil, false
	}
	return answer, true
}

// Rough and deliberately rounded up: too low a count makes Claude Code compact too late,
// too high only costs an earlier summary.
func estimateResponse(w http.ResponseWriter, body []byte) {
	runes := utf8.RuneCount(body)
	extra := len(body) - runes
	tokens := (runes+3)/4 + extra
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(200)
	json.NewEncoder(w).Encode(map[string]int{"input_tokens": tokens})
}
