// Package alias hex-encodes router aliases into Anthropic-looking IDs so Claude Code's model picker doesn't drop them.
package alias

import (
	"encoding/hex"
	"strings"
)

const (
	prefix    = "anthropic/claude-router-h"
	ctxSuffix = "[1m]"
)

// Encode wraps an alias. A trailing [1m] stays visible because Claude Code interprets it itself.
func Encode(a string) string {
	body, suffix := a, ""
	if strings.HasSuffix(a, ctxSuffix) {
		body, suffix = strings.TrimSuffix(a, ctxSuffix), ctxSuffix
	}
	return prefix + hex.EncodeToString([]byte(body)) + suffix
}

func IsEncoded(s string) bool { return strings.HasPrefix(s, prefix) }

// Decode returns the original alias. A name not produced by Encode is returned unchanged with false.
func Decode(s string) (string, bool) {
	if !IsEncoded(s) {
		return s, false
	}
	body := strings.TrimPrefix(s, prefix)
	suffix := ""
	if strings.HasSuffix(body, ctxSuffix) {
		body, suffix = strings.TrimSuffix(body, ctxSuffix), ctxSuffix
	}
	raw, err := hex.DecodeString(body)
	if err != nil {
		return s, false
	}
	return string(raw) + suffix, true
}
