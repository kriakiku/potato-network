---
title: API
weight: 20
---

Last-mile network emulator control plane for Docker sidecars.

## Base URL
`http://<host>:7783` — publish **only** the API port from the PotatoNetwork container. JSON lives under `/v1/…`. Opening `http://<host>:7783/` **302-redirects** to these docs.

## Spec downloads
Machine-readable OpenAPI remains available alongside the Markdown API page:
- [swagger.json](https://kriakiku.github.io/potato-network/swagger.json)
- [swagger.yaml](https://kriakiku.github.io/potato-network/swagger.yaml)

## Auth
Optional. Set `POTATONETWORK_API_TOKEN` or `POTATONETWORK_API_TOKEN_FILE`. Send `Authorization: Bearer <token>`. If the token is empty, auth is off. `GET /` and `GET /v1/health` stay public.
When a token **is** set, the API also answers CORS with allow-all (`Access-Control-Allow-Origin/Methods/Headers: *`) so browser UIs can call with Bearer.

## Boot profile
Default is **passthrough**. Set `POTATONETWORK_PROFILE_COUNTRY` (and optional `POTATONETWORK_PROFILE_TIER`, default `typical`) to apply a country profile at start. Change later with `PUT /v1/profile`.

## Profile warnings
`GET`/`PUT /v1/profile` may include `emulationLimited: true` and `warning` when host CF RTT is already ≥ country CF RTT (last-mile delay clamped to 0). That is a **warning**, not an error — the profile still applies. The process also logs `WARN` on change.

## Examples
```bash
curl -s -X POST "$API/v1/baseline/probe"
curl -s -X PUT "$API/v1/profile" -H 'Content-Type: application/json' \
-d '{"country":"BD","tier":"typical"}'
curl -s -X PUT "$API/v1/profile" -H 'Content-Type: application/json' \
-d '{"passthrough":true}'
curl -s "$API/v1/stats"
curl -s -X POST "$API/v1/stats/reset"
```

Baseline lives in `/data/baseline.json`. Cron: `POTATONETWORK_BASELINE_CRON` (default ~every 3h). Docs home: [PotatoNetwork](https://kriakiku.github.io/potato-network/).

## DNS / TLS stats
`GET /v1/stats` returns in-memory aggregates since process start (or last reset): per-domain DNS forwarder RTT, MITM client TLS handshake time (includes synthetic last-mile sleep), upstream origin TLS handshake time, **HTTP request TTFB** (host+method+path, query stripped), **WebSocket** upgrade attempts + time-to-first-frame, an **`events`** array (`http_start` / `ws_start` with `atUnixMs`, query stripped) for aligning custom video overlays, **`slowHTTP`** (top 5 longest individual HTTP start→response samples; WebSocket excluded), and **`cfCache`** (map of raw `cf-cache-status` → count; key `NONE` = not Cloudflare). `POST /v1/stats/reset` clears the counters.

Default host in the generated OpenAPI spec: `localhost:7783` (base path `/`).

## Endpoints

### system

#### `GET /`

Redirect to docs

302 to GitHub Pages API docs

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `302` | `string` | Location: docs URL |
| `404` | `ErrorResponse` | Not Found |

#### `GET /v1/health`

Health check

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `HealthResponse` | OK |

### profile

#### `GET /v1/profile`

Get profile

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `Profile` | OK |
| `401` | `ErrorResponse` | Unauthorized |

#### `PUT /v1/profile`

Set profile

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Consumes: `application/json`

Produces: `application/json`

| Param | In | Required | Type | Description |
| --- | --- | --- | --- | --- |
| `body` | body | yes | `ProfilePutRequest` | Country+tier or passthrough |

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `Profile` | OK |
| `400` | `ErrorResponse` | Bad Request |
| `401` | `ErrorResponse` | Unauthorized |
| `500` | `ErrorResponse` | Internal Server Error |

### baseline

#### `GET /v1/baseline`

Get baseline

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `BaselineResponse` | OK |
| `401` | `ErrorResponse` | Unauthorized |

#### `PUT /v1/baseline`

Set baseline

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Consumes: `application/json`

Produces: `application/json`

| Param | In | Required | Type | Description |
| --- | --- | --- | --- | --- |
| `body` | body | yes | `BaselinePutRequest` | Host RTT map |

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `BaselineResponse` | OK |
| `400` | `ErrorResponse` | Bad Request |
| `401` | `ErrorResponse` | Unauthorized |

#### `POST /v1/baseline/probe`

Probe baseline

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `BaselineResponse` | OK |
| `401` | `ErrorResponse` | Unauthorized |

### catalog

#### `GET /v1/catalog`

Get catalog

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `Catalog` | OK |
| `401` | `ErrorResponse` | Unauthorized |

#### `POST /v1/catalog/refresh`

Refresh catalog

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `CatalogRefreshResponse` | OK |
| `401` | `ErrorResponse` | Unauthorized |
| `500` | `ErrorResponse` | Internal Server Error |

### ca

#### `GET /v1/ca.pem`

Download CA certificate

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Produces: `application/x-pem-file`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `string` | PEM certificate |
| `401` | `ErrorResponse` | Unauthorized |

### rules

#### `GET /v1/rules`

Rules status

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `RulesStatus` | OK |
| `401` | `ErrorResponse` | Unauthorized |

### stats

#### `GET /v1/stats`

Get DNS/TLS stats

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `Snapshot` | OK |
| `401` | `ErrorResponse` | Unauthorized |

#### `POST /v1/stats/reset`

Reset DNS/TLS stats

Auth: optional Bearer when `POTATONETWORK_API_TOKEN` is set.

Produces: `application/json`

| Status | Schema | Description |
| --- | --- | --- |
| `200` | `StatsResetResponse` | OK |
| `401` | `ErrorResponse` | Unauthorized |

## Schemas

### `BaselinePutRequest`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `hostRtt` | `map<string, integer>` | no |  |

### `BaselineResponse`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `hostRtt` | `map<string, integer>` | no |  |
| `probedAt` | `string` | no | example: `2026-01-01T00:00:00Z` |

### `CatalogRefreshResponse`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `generatedAt` | `string` | no | example: `2026-01-01T00:00:00Z` |
| `ok` | `boolean` | no | example: `true` |

### `ErrorResponse`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `error` | `string` | no | example: `unauthorized` |

### `HealthResponse`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `ok` | `boolean` | no | example: `true` |
| `product` | `string` | no | example: `PotatoNetwork` |
| `profile` | `string` | no | example: `passthrough` |
| `shape` | `string` | no | example: `passthrough` |

### `ProfilePutRequest`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `country` | `string` | no | example: `BD` |
| `passthrough` | `boolean` | no | example: `false` |
| `tier` | `string` | no | example: `typical` |

### `RulesStatus`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `compile` | `string` | no |  |
| `ok` | `boolean` | no | example: `true` |
| `path` | `string` | no | example: `/data/rules.expr` |
| `source` | `string` | no |  |

### `Catalog`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `countries` | `array<Country>` | no |  |
| `destinations` | `map<string, Destination>` | no |  |
| `generatedAt` | `string` | no |  |
| `source` | `string` | no |  |

### `Country`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | `string` | no |  |
| `name` | `string` | no |  |
| `nearestAws` | `string` | no |  |
| `rttSource` | `string` | no |  |
| `tiers` | `map<string, Tier>` | no |  |

### `Destination`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `awsRegion` | `string` | no |  |
| `calibrate` | `string` | no |  |
| `label` | `string` | no |  |
| `target` | `string` | no |  |

### `StatsResetResponse`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `ok` | `boolean` | no | example: `true` |

### `Snapshot`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `dns` | `DomainStat[]` | no |  |
| `tlsClient` | `DomainStat[]` | no |  |
| `tlsUpstream` | `DomainStat[]` | no |  |
| `http` | `RequestStat[]` | no | query stripped from path |
| `websocket` | `RequestStat[]` | no | `started` = upgrade attempts; latency = first frame |
| `events` | `Event[]` | no | ring-buffered `http_start` / `ws_start` timeline (`atUnixMs`) |
| `slowHTTP` | `HTTPSample[]` | no | top 5 longest HTTP start→response (desc by `durationMs`; not WS) |
| `cfCache` | `map<string, integer>` | no | raw `cf-cache-status` → count; `NONE` = not Cloudflare; `UNKNOWN` = CF without status |

### `Event`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `kind` | `string` | yes | `http_start` or `ws_start` |
| `host` | `string` | yes |  |
| `method` | `string` | no | HTTP only |
| `path` | `string` | yes | query stripped |
| `atUnixMs` | `integer` | yes | wall-clock ms |

### `HTTPSample`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `host` | `string` | yes |  |
| `method` | `string` | yes |  |
| `path` | `string` | yes | query stripped |
| `durationMs` | `integer` | yes | start → response/error |
| `failed` | `boolean` | yes |  |
| `atUnixMs` | `integer` | yes | when the sample was recorded |

### `RequestStat`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `host` | `string` | no |  |
| `method` | `string` | no | HTTP only |
| `path` | `string` | no | no query/hash |
| `count` | `integer` | no |  |
| `started` | `integer` | no | WebSocket upgrade attempts |
| `errorCount` | `integer` | no |  |
| `latencyMs` | `Latency` | no |  |

### `DomainStat`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `domain` | `string` | no |  |
| `count` | `integer` | no |  |
| `errorCount` | `integer` | no |  |
| `latencyMs` | `Latency` | no |  |

### `Latency`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `sumMs` | `integer` | no |  |
| `minMs` | `integer` | no |  |
| `maxMs` | `integer` | no |  |

### `Tier`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `downloadMbps` | `number` | no |  |
| `lossPercent` | `number` | no |  |
| `rttToDest` | `map<string, integer>` | no |  |
| `uploadMbps` | `number` | no |  |

### `Profile`

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `country` | `string` | no |  |
| `delayMs` | `integer` | no |  |
| `description` | `string` | no |  |
| `downloadMbps` | `number` | no |  |
| `emulationLimited` | `boolean` | no |  |
| `id` | `string` | no |  |
| `lossPercent` | `number` | no |  |
| `name` | `string` | no |  |
| `passthrough` | `boolean` | no |  |
| `tier` | `string` | no |  |
| `uploadMbps` | `number` | no |  |
| `warning` | `string` | no |  |

