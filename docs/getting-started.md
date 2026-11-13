# Getting started

## Install

Download the binary for your platform from the
[latest release](https://github.com/SpielerNogard/claude-router/releases/latest). The web UI
is built in.

=== "macOS (Apple Silicon)"

    ```bash
    curl -fsSL -o claude-router \
      https://github.com/SpielerNogard/claude-router/releases/latest/download/claude-router-darwin-arm64
    chmod +x claude-router && sudo mv claude-router /usr/local/bin/
    ```

=== "macOS (Intel)"

    ```bash
    curl -fsSL -o claude-router \
      https://github.com/SpielerNogard/claude-router/releases/latest/download/claude-router-darwin-amd64
    chmod +x claude-router && sudo mv claude-router /usr/local/bin/
    ```

=== "Linux"

    ```bash
    arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
    curl -fsSL -o claude-router \
      "https://github.com/SpielerNogard/claude-router/releases/latest/download/claude-router-linux-$arch"
    chmod +x claude-router && sudo mv claude-router /usr/local/bin/
    ```

=== "Windows"

    Download `claude-router-windows-amd64.exe` from the release page and put it on your `PATH`.

Check the install with `claude-router --version`. To verify the binary, compare it
against `checksums.txt` from the release (`shasum -a 256 -c --ignore-missing checksums.txt`).

!!! note "macOS Gatekeeper"
    The binaries aren't signed. `curl` downloads are fine. If you downloaded with a
    browser, run `xattr -d com.apple.quarantine claude-router` once.

## Build from source

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
claude-router &
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
