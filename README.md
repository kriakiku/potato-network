# 🥔 PotatoNetwork

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![CI](https://github.com/kriakiku/potato-network/actions/workflows/ci.yml/badge.svg)](https://github.com/kriakiku/potato-network/actions/workflows/ci.yml)
[![Docs](https://img.shields.io/badge/docs-GitHub%20Pages-blue?logo=github)](https://kriakiku.github.io/potato-network/)
[![OpenAPI](https://img.shields.io/badge/OpenAPI-Swagger-85EA2D?logo=swagger&logoColor=black)](https://kriakiku.github.io/potato-network/api/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Go-only last-mile network emulator for Docker: run PotatoNetwork as a container, attach apps with `network_mode: service:potatonetwork`, pick a country profile via API, and traffic (including DNS) gets shaped. Transparent MITM applies path delay from an [expr](https://github.com/expr-lang/expr) script. No WireGuard, no web UI. **Open source under the [MIT License](LICENSE).**

## Quick start

Runtime image is **`scratch` + UPX-compressed static binary** (catalog and Mozilla CA roots are embedded; no apt packages).

Prebuilt images: `ghcr.io/kriakiku/potato-network`

| Tag | Meaning |
|-----|---------|
| `latest` | Tip of `main` from human / catalog commits, or a `v*` release — **not** moved by Dependabot merges |
| `nightly` | Tip of `main` (includes Dependabot dependency bumps) |
| `sha-<commit>` | Immutable commit build |
| `vX.Y.Z` / `X.Y` | GitHub release tags |

### Releasing (`v*` tags)

Push a version tag — CI does the rest:

```bash
git tag v0.1.0
git push origin v0.1.0
```

That run:

1. Publishes the GHCR image (`latest`, `v0.1.0`, `0.1`, `sha-…`)
2. Builds static **linux/amd64** and **linux/arm64** binaries
3. Creates a [GitHub Release](https://github.com/kriakiku/potato-network/releases) with auto-generated notes (commits/PRs since the previous tag) and attaches the binaries + `.sha256`

Prerelease tags with a hyphen (e.g. `v0.2.0-rc.1`) are marked as prerelease. Full dataplane still needs Linux + `NET_ADMIN` (same as the container).

```bash
docker compose up -d --build
curl -s localhost:7783/v1/health
curl -s -X POST localhost:7783/v1/baseline/probe
curl -s -X PUT localhost:7783/v1/profile \
  -H 'Content-Type: application/json' \
  -d '{"country":"BD","tier":"typical"}'
```

Sidecar example:

```yaml
services:
  potatonetwork:
    build: .
    cap_add: [NET_ADMIN]
    ports: ["7783:7783"]
    volumes: ["pn-data:/data"]
  browser:
    image: zenika/alpine-chrome
    network_mode: service:potatonetwork
    depends_on: [potatonetwork]
```

Sidecars share the netns, so DNS is already `127.0.0.1` after PotatoNetwork rewrites resolv.conf. Trust CA from `/data/ca/potatonetwork-ca.pem` or `GET /v1/ca.pem`.

## Data volume (`/data`)

| Path | Purpose |
|------|---------|
| `ca/potatonetwork-ca.pem` | MITM root CA (auto-created) |
| `ca/potatonetwork-ca-key.pem` | CA private key |
| `rules.expr` | Path-delay policy (expr script) |
| `catalog.json` | Country profiles (Radar catalog) |
| `baseline.json` | Host RTT baseline + `probedAt` |

## ENV

| Variable | Default | Notes |
|----------|---------|--------|
| `POTATONETWORK_DATA` | `/data` | |
| `POTATONETWORK_API_ADDR` | `:7783` | Not shaped (7783 ≈ SPUD on a phone keypad) |
| `POTATONETWORK_API_TOKEN` / `_FILE` | empty | Empty = no auth; when set, API also answers CORS with allow-all |
| `POTATONETWORK_PROFILE_COUNTRY` | empty | Boot country profile (e.g. `BD`); empty = passthrough |
| `POTATONETWORK_PROFILE_TIER` | `typical` | Used with `PROFILE_COUNTRY` (`stable` / `typical` / `poor`) |
| `POTATONETWORK_CATALOG_CRON` | empty | Empty/unset = Tuesday random UTC; `false` = disable; or `M H * * D` |
| `POTATONETWORK_BASELINE_CRON` | empty | Empty/unset = every 3h at random UTC minute; `false` = disable; or `M */N * * *` |
| `POTATONETWORK_UPLINK` | auto | Egress iface for netlink shaping |
| `POTATONETWORK_PATH_DELAY_MAX_MS` | `60000` | Cap for a single `rules.expr` path-delay sleep (ms); over-cap is clamped with a WARN |
| `POTATONETWORK_SHAPE_EXCLUDE` | empty | Comma/space-separated IPv4 or CIDRs that bypass netem **and** MITM (e.g. `10.0.0.0/8,1.2.3.4`) |
| `POTATONETWORK_TLS_INSECURE` | empty | Exact `true` only: skip upstream origin TLS cert verification (MITM→origin) |

`GET /` on the API port always redirects to the API docs (`https://kriakiku.github.io/potato-network/api/`).

DNS upstream comes from the container’s `/etc/resolv.conf` (Docker embedded `127.0.0.11`, or Compose `dns:`). After boot, resolv is rewritten to `127.0.0.1` so the netns uses PotatoNetwork’s shaped `:53`. Do not set `dns: [127.0.0.1]` on the potatonetwork service — that hides the real upstream.

Boot profile is **passthrough** unless `POTATONETWORK_PROFILE_COUNTRY` is set (then that country/tier is applied at start). You can still change it later with `PUT /v1/profile`.

## Docs

Site sources live under [`website/`](website/) ([Hextra](https://imfing.github.io/hextra/)); published via GitHub Pages.

- Docs home: https://kriakiku.github.io/potato-network/
- API: https://kriakiku.github.io/potato-network/api/ (Markdown generated from OpenAPI, same Hextra styling as the rest of the docs)
- Spec download: [swagger.json](https://kriakiku.github.io/potato-network/swagger.json) · [swagger.yaml](https://kriakiku.github.io/potato-network/swagger.yaml) (generated on Pages build / CI; not stored in git)

Regenerate OpenAPI + API page locally (swagger files under `website/static/` are gitignored; `api.md` is written into content):

```bash
go run github.com/swaggo/swag/cmd/swag@v1.16.4 init \
  -g main.go \
  -d ./cmd/potatonetwork,./internal/api,./internal/profiles,./internal/catalog \
  -o ./website/static \
  --outputTypes json,yaml \
  --parseDependency --parseInternal
go run -tags genapi ./cmd/genapi/
```

## Tests

- `go test ./...` — unit tests (no root / Docker).
- `go test -tags e2e ./e2e/` — integration against `compose.e2e.yaml` (local HTTP origin `:80`, WebSocket echo `:8765`).
- `go run -tags gallery ./cmd/gengallery/` — regenerate profiles gallery markdown.
- `go run -tags genapi ./cmd/genapi/` — regenerate API markdown from `website/static/swagger.json`.
- `go run -tags radar ./cmd/genradar/` — refresh Radar/CloudPing catalog (optional `CLOUDFLARE_API_TOKEN`).

## License

[MIT](LICENSE) — free to use, modify, and distribute, including commercially. See [LICENSE](LICENSE) for the full text.
