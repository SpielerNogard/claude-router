# Getting started

## Build

You need Go 1.25+, and Node/npm for the web UI.

```bash
git clone https://github.com/SpielerNogard/claude-router.git
cd claude-router
make build          # UI + binary → ./claude-router
make build UI=0     # without the UI; /ui/ then only shows a notice
make release        # cross-compile into dist/ for darwin/linux/windows
```

## Minimal configuration

The smallest working setup needs no key. It passes your Claude subscription through.
Create `~/.config/claude-router/config.toml` with this content:

```toml
[server]
listen = "127.0.0.1:4141"

[providers.subscription]
format  = "anthropic"
auth    = "passthrough"
default = true
```

## Run

```bash
./claude-router &
ANTHROPIC_BASE_URL=http://127.0.0.1:4141 claude
```

In Claude Code, `/model <provider>:<model>` selects the provider, e.g. `/model subscription:opus-5`.
Model names without a prefix go to the provider marked `default = true`.

To see the dashboard, open <http://127.0.0.1:4141/ui/>. Run `curl localhost:4141/api/usage`
to see usage as JSON.

## Add more providers

```toml
[providers.deepseek]
format      = "openai"
auth        = "key"
base_url    = "https://api.deepseek.com"
api_key_env = "DEEPSEEK_API_KEY"
include     = ["deepseek-*"]
```

```bash
export DEEPSEEK_API_KEY=...
claude-router check deepseek     # verifies base_url, env var and key
```

Then run `/model deepseek:deepseek-chat` in Claude Code. For all options, see
[Configuration](configuration.md).
