# Claude Code integration

## Pointing Claude Code at the router

```bash
ANTHROPIC_BASE_URL=http://127.0.0.1:4141 claude
```

You set `ANTHROPIC_BASE_URL` yourself. The router doesn't start Claude Code and doesn't
write its configuration.

## Models in the `/model` picker

Claude Code fills its picker from the gateway's `GET /v1/models`, but only under two
conditions: a credential (`ANTHROPIC_AUTH_TOKEN`, `apiKeyHelper` or an API key) is present,
and discovery is enabled:

```bash
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1
```

Without a credential, Claude Code's debug log shows:

```
[gatewayDiscovery] skipped: no credential (ANTHROPIC_AUTH_TOKEN, apiKeyHelper, or API key)
```

Plain `passthrough` creates a conflict:

| | Picker filled | Subscription via passthrough |
|---|---|---|
| without `ANTHROPIC_AUTH_TOKEN` | no | **yes** |
| with `ANTHROPIC_AUTH_TOKEN` | **yes** | no |

With `ANTHROPIC_AUTH_TOKEN` set, Claude Code sends *that* token instead of its OAuth
token. You have two ways out:

1. **Use `auth = "claude-credential-helper"`** for the subscription provider. The router
   reads the OAuth token itself, so the client can send a dummy `ANTHROPIC_AUTH_TOKEN`
   (see [Configuration](configuration.md#claude-credential-helper)).
2. **Keep `passthrough`** and maintain the list yourself in `~/.claude/settings.json`:

   ```json
   {
     "modelPicker": [
       {"model": "claude-opus-5",           "label": "Opus (subscription)"},
       {"model": "claude-sonnet-5",         "label": "Sonnet (subscription)"},
       {"model": "deepseek:deepseek-flash", "label": "DeepSeek Flash"}
     ]
   }
   ```

### How aliases appear

The picker only accepts IDs that look like Anthropic models. The router therefore wraps
its aliases as `anthropic/claude-router-h<hex>`, with the readable name in
`display_name`, and unwraps them on the way back. Typed aliases
(`/model deepseek:deepseek-flash`) always work.

The router re-fetches provider model lists (`GET /v1/models` upstream) every five minutes,
because models change at runtime in LM Studio. `POST /api/models/refresh` triggers a fetch
right away. Claude Code caches the list per base URL in
`~/.claude/cache/gateway-models.json`. Delete that file after changing ports.

### `extra_models` and the `[1m]` suffix

`extra_models` adds models that discovery doesn't return. Discovery stays on alongside it.

```toml
[providers.deepseek]
extra_models = ["deepseek-flash[1m]"]
```

The `[1m]` suffix is a promise to Claude Code, not part of the model name. Claude Code
then assumes a 1M-token context instead of the default 200k for unknown models, and
compacts later. It strips the suffix before sending, so the suffix never reaches the
upstream. Only add it if the model really accepts that much context. Otherwise you get
an API error instead of timely compaction.

## Auto mode: server-side checks

In auto mode, Claude Code can have the server check actions inline. It sends a
`safeguards` field with a matching `anthropic-beta` value and expects
`safeguard_results` in the response. On the passthrough path, the router forwards body,
headers and response byte for byte, so this keeps working. The
`TestPassthroughKeepsSafeguardsAndBeta` test pins that behavior.

| Session runs via | Server-side checks |
|---|---|
| `anthropic` provider (passthrough) | yes |
| translated provider (`openai` format) | no, the upstream isn't Anthropic |
| a provider reached through `fallback` | no, even for Anthropic models, because the request lands elsewhere |

The `Auto mode server` line in `/status` shows which case applies. For sessions that
aren't checked server-side, set `CLAUDE_CODE_AUTO_MODE_SERVER=0`.

## Attribution header on translated providers

On the translated path, the router concatenates system blocks into one text. The leading
attribution block (`x-anthropic-billing-header: …`) then becomes ordinary prompt text,
because Anthropic only strips it when it arrives unchanged as the first block. If you
don't want it in prompts to local models, set `CLAUDE_CODE_ATTRIBUTION_HEADER=0` on the
**client**.
