package credstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Same client ID as Claude Code — the refresh token is bound to it.
const clientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

var (
	tokenURL   = "https://platform.claude.com/v1/oauth/token"
	httpClient = &http.Client{Timeout: 30 * time.Second}
)

// renew renews the token and writes it, with all unknown fields, back to where it came from.
func renew(configDir string) (Credential, error) {
	raw, save, err := source(configDir)
	if err != nil {
		return Credential{}, err
	}
	var doc map[string]json.RawMessage
	var oauth map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Credential{}, fmt.Errorf("unreadable credential: %w", err)
	}
	if err := json.Unmarshal(doc["claudeAiOauth"], &oauth); err != nil {
		return Credential{}, fmt.Errorf("credential without claudeAiOauth: %w", err)
	}
	refresh, _ := oauth["refreshToken"].(string)
	if refresh == "" {
		return Credential{}, fmt.Errorf("no refreshToken found — is this Claude Code signed in?")
	}

	req := map[string]string{"grant_type": "refresh_token", "refresh_token": refresh, "client_id": clientID}
	if scopes, ok := oauth["scopes"].([]any); ok {
		var parts []string
		for _, s := range scopes {
			if str, ok := s.(string); ok {
				parts = append(parts, str)
			}
		}
		if len(parts) > 0 {
			req["scope"] = strings.Join(parts, " ")
		}
	}
	body, _ := json.Marshal(req)
	resp, err := httpClient.Post(tokenURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return Credential{}, fmt.Errorf("renew: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != 200 {
		return Credential{}, fmt.Errorf("renewal rejected (%d): %s — sign in to the account again",
			resp.StatusCode, respBody)
	}
	var tok struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &tok); err != nil || tok.AccessToken == "" || tok.ExpiresIn <= 0 {
		return Credential{}, fmt.Errorf("renew: unusable response")
	}

	oauth["accessToken"] = tok.AccessToken
	if tok.RefreshToken != "" {
		oauth["refreshToken"] = tok.RefreshToken
	}
	oauth["expiresAt"] = time.Now().Add(time.Duration(tok.ExpiresIn * float64(time.Second))).UnixMilli()
	doc["claudeAiOauth"], _ = json.Marshal(oauth)
	out, _ := json.Marshal(doc)
	if err := save(out); err != nil {
		// The old refresh token is used up: after a restart only signing in again helps.
		log.Printf("credstore: token for %q renewed but not saved: %v", configDir, err)
	}
	return parse(out)
}
