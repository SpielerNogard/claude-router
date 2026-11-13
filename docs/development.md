# Development

```bash
make test        # Go tests with -race
make ui-test     # frontend tests (vitest)
make build       # UI + binary
make release     # cross-compile into dist/
make prices      # refresh internal/pricing/prices.json from LiteLLM
```

## Layout

| Path | Contents |
|---|---|
| `cmd/router` | CLI (Cobra), server wiring, `check`, `stop`, debug output |
| `internal/proxy` | Request handling, routing, fallback, cooldown, stream watchdog |
| `internal/convert` | Anthropic ↔ OpenAI translation, including SSE streams |
| `internal/upstream` | Sending requests and setting auth headers |
| `internal/credstore` | Claude Code credential reading (Keychain / file) and refresh |
| `internal/discover` | Model discovery per provider |
| `internal/store` | SQLite usage store and message log |
| `internal/api` | `/api/*` and `/healthz` handlers |
| `internal/pricing` | Cost calculation from LiteLLM's price list |
| `web/` | Vue 3 + TypeScript UI, embedded via `go:embed` |

## UI

```bash
cd web && npm run dev
```

Hot reload needs a router running on `:4141`. The Vite dev server proxies `/api` and
`/healthz` to it. `go build ./cmd/router` embeds whatever is currently in `web/dist`.

## Documentation

These docs are built with [Zensical](https://zensical.org) and deployed to GitHub Pages
on every push to `main`:

```bash
uvx zensical serve
```
