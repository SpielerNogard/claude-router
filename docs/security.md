# Security

## What the router does with credentials

- **`passthrough`:** the client's header is forwarded unchanged and never stored.
- **`key`:** the key is read from the configured environment variable on every request.
  It never lives in the config file.
- **`claude-credential-helper`:** the access token is read from Claude Code's credential
  store. It's written back only with `refresh = true`.
- The router doesn't start Claude Code and doesn't write to `~/.claude/`.

## `ROUTER_API_TOKEN`

The `/api/*` endpoints are administrative, not proxy routes:

- `PUT /api/config` can point any environment variable, as a credential, at any URL.
- `GET /api/messages/{id}` returns full request content in plain text.

There's also a risk on the proxy routes. Once a provider uses `auth = "key"`, the router
inserts its own paid key, so an open port would act as a relay for it.

Rules:

- **Loopback only** (`127.0.0.1`, `::1`, `localhost`): no token needed.
- **Any other address** (private network, public deployment): `ROUTER_API_TOKEN`
  **must** be set. Without it, the router refuses to start.
- With a token set, every `/api/*` request needs `Authorization: Bearer <token>`.
  Otherwise the router answers with 401.
- **Off-loopback with at least one `auth = "key"` provider:** every `/v1/*` request also
  needs `X-Router-Token: <token>`. `Authorization` can't be used there, because it
  already carries the client's credential. In Claude Code:

```bash
export ANTHROPIC_CUSTOM_HEADERS="X-Router-Token: <token>"
```

## Reporting a vulnerability

Please open a [GitHub security advisory](https://github.com/SpielerNogard/claude-router/security/advisories/new)
rather than a public issue.
