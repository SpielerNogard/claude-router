package upstream

import (
	"net/http"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/credstore"
)

// Claude Code renews its token earlier than the router and Anthropic revokes the old one at once: on 401 re-read once.
func Send(client *http.Client, req *http.Request, p config.Provider) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || p.Auth != "claude-credential-helper" || req.GetBody == nil {
		return resp, err
	}
	dir := credstore.ConfigDir(p.ConfigDir)
	credstore.Forget(dir)
	tok, terr := credstore.Token(dir, p.Refresh)
	if terr != nil || "Bearer "+tok == req.Header.Get("authorization") {
		return resp, nil
	}
	body, berr := req.GetBody()
	if berr != nil {
		return resp, nil
	}
	resp.Body.Close()
	retry := req.Clone(req.Context())
	retry.Body = body
	retry.Header.Set("authorization", "Bearer "+tok)
	return client.Do(retry)
}
