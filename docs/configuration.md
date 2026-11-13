# Configuration

The default location is `~/.config/claude-router/config.toml`, which you can override
with `--config`. You can also read and write the configuration at runtime through
`GET/PUT /api/config`.

```toml
[server]
listen              = "127.0.0.1:4141"
stream_idle_timeout = "120s"          # default; "0s" disables the watchdog

[log]
level       = "off"                   # off | meta | full
keep_days   = 7
max_size_mb = 500
max_body_kb = 256

[providers.subscription]                       # Claude subscription, client credential passed through
format   = "anthropic"
auth     = "passthrough"
default  = true
fallback = "deepseek:deepseek-flash"

[providers.api]                       # Anthropic API key
format      = "anthropic"
auth        = "key"
api_key_env = "ANTHROPIC_API_KEY"

[providers.deepseek]                  # any OpenAI-compatible API
format       = "openai"
auth         = "key"
base_url     = "https://api.deepseek.com"
api_key_env  = "DEEPSEEK_API_KEY"
include      = ["deepseek-*"]
extra_models = ["deepseek-flash[1m]"]

[providers.lmstudio]                  # local server, no credential
format   = "openai"
auth     = "none"
base_url = "http://127.0.0.1:1234"
exclude  = ["text-embedding*"]
```

!!! warning "Keys live in the environment"
    API keys never go in the file. Only the *name* of the environment variable does.
    That variable must be exported **before** the router starts. If it isn't, the
    router refuses to start rather than failing on the first request.

## Logging

| Key | Meaning |
|---|---|
| `level` | `off`: usage only. `meta`: plus request metadata. `full`: plus request and response bodies (needed for the UI inspector). |
| `keep_days` | Retention of the message log |
| `max_size_mb` | Size cap of the message log, checked hourly |
| `max_body_kb` | Bodies beyond this size are truncated and marked as such |

## Provider options

| Key | Meaning |
|---|---|
| `format` | `anthropic` (passthrough) or `openai` (translated) |
| `auth` | `passthrough`, `key`, `none` or `claude-credential-helper` (see below) |
| `base_url` | Upstream URL; required for `openai` and `none` |
| `api_key_env` | Environment variable holding the key (`auth = "key"`) |
| `default` | Receives model names without a `provider:` prefix |
| `fallback` | Where to go when the subscription window is exhausted: `provider` or `provider:model` |
| `include` / `exclude` | Glob filters for the discovered model list |
| `models` | Static model list, for providers without `GET /v1/models` |
| `extra_models` | Added on top of discovery; not filtered by `include`/`exclude` |
| `config_dir` | Claude Code config directory (`claude-credential-helper` only) |
| `refresh` | Let the router refresh the OAuth token itself (`claude-credential-helper` only) |

## Auth modes

| Mode | What the router sends upstream |
|---|---|
| `passthrough` | The client's own `Authorization`/`x-api-key` header, unchanged. The router holds no credential. |
| `key` | The key from `api_key_env`, re-read on every request. |
| `none` | Nothing. Any client credential is stripped so it can't leak to a local process. Requires `base_url`. |
| `claude-credential-helper` | The OAuth access token from Claude Code's own login. |

### `claude-credential-helper`

This mode lets the client send a dummy `ANTHROPIC_AUTH_TOKEN`, which enables Claude
Code's gateway model discovery, while requests still go through your subscription
(see [Claude Code integration](claude-code.md#models-in-the-model-picker)).

```toml
[providers.subscription]
format     = "anthropic"
auth       = "claude-credential-helper"
config_dir = "~/.claude"
default    = true
```

!!! tip "Always set `config_dir`, even for the default"
    Without it, the router follows `CLAUDE_CONFIG_DIR` from its own environment. Start it
    from a shell where that variable is set, and it silently uses a different login. For
    the Claude Code bundled with the desktop app, use
    `~/Library/Application Support/Claude/claude-code`.

- By default the router only **reads** the access token. It never touches the refresh
  token, because two parties that share a refresh token invalidate each other. That is
  the mechanism that breaks logins.
- Claude Code keeps refreshing its token while you use it with that config directory,
  even while `ANTHROPIC_AUTH_TOKEN` is set. In one test, a token that had expired two and
  a half days earlier was renewed on the next message, and the router picked up the new
  one.
- The router may run without any Claude Code session, e.g. on a server. In that case,
  either use `auth = "key"`, or give the router a config directory of its own and set
  `refresh = true`. **Never** use `refresh = true` on a directory that a running Claude
  Code also uses.
- An expired token is reported explicitly instead of as a bare 401. On a 401 the router
  re-reads the credential once and retries.

The credential location depends on the platform:

- **macOS:** the Keychain entry `Claude Code-credentials` for `~/.claude`, and
  `Claude Code-credentials-<sha256(path)[:8]>` for any other directory.
- **Linux and Windows:** `<config_dir>/.credentials.json` (untested).

### Local servers: `auth = "none"`

Ollama, LM Studio, vLLM and llama.cpp need no credential. `auth = "none"` strips any
client credential, so it can't leak to a local process. It also requires `base_url`,
because the mode is meant for local servers, not the open internet.

## Subscription fallback

A Claude subscription can be exhausted. Anthropic then answers with `429` and
`anthropic-ratelimit-unified-status: rejected`, with no overage in effect. When the
provider has a `fallback`, it is marked exhausted until the window resets, and requests
are rerouted:

```toml
[providers.subscription]
format   = "anthropic"
auth     = "passthrough"
default  = true
fallback = "account2"                  # provider only: keep the requested model

[providers.account2]                   # once: CLAUDE_CONFIG_DIR=~/.claude-account2 claude, then /login
format     = "anthropic"
auth       = "claude-credential-helper"
config_dir = "~/.claude-account2"
refresh    = true
fallback   = "deepseek:deepseek-flash" # provider:model: switch model too
```

- Without `fallback`, the error goes to the client as before.
- Ordinary rate limits without that evidence are passed through. Silently switching
  models would be the worse outcome.
- Only `format = "anthropic"` providers send these headers, so only those can fall back.
- Switching accounts costs one prompt-cache miss, because the cache is per account.
- Blocked providers show up in `GET /healthz`:

```json
{"blocked": {"subscription": "2026-09-25T15:59:32+02:00"}, "status": "ok"}
```

## Stream watchdog: `stream_idle_timeout`

If a stream goes silent mid-response (dead connection, network change, firewall), the
router would otherwise wait forever, and so would the client. Instead, it aborts after
`stream_idle_timeout`. It then writes an `event: error` into the stream and logs
`status=partial`, `code=stalled`.

The watchdog applies **only to streams**. A non-streaming answer arrives in one piece, so
any idle limit would be a false alarm there. Claude Code aborts silent streams on its own
after 3 minutes (5 behind a gateway). The 120 s default is set below that, so the error
comes from the router and the client can retry right away.
