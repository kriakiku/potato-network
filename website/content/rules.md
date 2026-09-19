---
title: Path rules
weight: 30
---

File: `/data/rules.expr` (hot-reloaded on mtime). Language: [expr](https://github.com/expr-lang/expr).

Evaluated on each HTTP **response** (so `Via` / CDN headers exist). Return a **map** of fields (extensible later):

| Field | Meaning |
|-------|---------|
| `delay_ms` | One-way path delay to sleep before writing the response (ms) |

Clamping on `delay_ms` (after evaluation):

| Condition | Result |
|-----------|--------|
| Negative | `0` |
| Above max | Cap to max and log `WARN` |

Max delay defaults to **60000** ms (1 minute). Override with `POTATONETWORK_PATH_DELAY_MAX_MS`.

## Helpers

| Name | Meaning |
|------|---------|
| `route(dest)` | Catalog path-extra delay (ms) for destination id (e.g. `aws-eu-central-1`) vs CF / host baseline. `cf` or unknown → `0` |
| `jitter(base_ms, jitter_ms)` | `base ±` uniform jitter in ms (clamped ≥ 0); compose into `delay_ms` |
| `header(map, name)` | Header lookup |
| `match(regex, s)` | Regex match |
| `lower(s)` | Lower-case |
| `a contains b` | expr infix operator |

`route` and `jitter` return plain numbers, so you can divide, add, or nest them freely before putting the result in `delay_ms`.

## Environment

| Name | Type |
|------|------|
| `phase` | `"response"` (currently) |
| `host` | request host / SNI |
| `path` | request URI path |
| `request` | map of request headers (lower-case keys) |
| `response` | map of response headers |
| `country` | Active profile country code (e.g. `BD`, `AF`); empty when passthrough |
| `tier` | Active profile tier (`stable` / `typical` / `poor`); empty when passthrough |
| `passthrough` | `true` when last-mile shaping is off |
| `status_code` | Origin HTTP status code (int) |
| `cloudflare` | `true` when the response looks like Cloudflare (`cf-ray`, `cf-cache-status`, or `Server` contains `cloudflare`) |
| `cloudfront` | `true` when the response looks like CloudFront (`Via` contains `cloudfront`, or `x-amz-cf-id` set) |
| `websocket` | `true` when status is `101` and both request and response `Upgrade` include `websocket` (same criterion as MITM tunnel) |

### Branch by country / tier

expr has no `else if` — nest `if` / `else`, or use `?:`.

```text
if passthrough {
  { "delay_ms": 0 }
} else {
  if country == "BD" && tier == "poor" {
    { "delay_ms": jitter(route("aws-ap-south-1"), 30) }
  } else {
    if country == "BD" {
      { "delay_ms": route("aws-ap-south-1") / 2 }
    } else {
      { "delay_ms": route("aws-eu-central-1") }
    }
  }
}
```

## Examples

### Fixed delay

```text
{ "delay_ms": 250 }
```

### Catalog path-extra (origin farther than CF)

```text
{ "delay_ms": route("aws-eu-central-1") }
```

### Half the catalog extra (softer emulation)

```text
{ "delay_ms": route("aws-eu-central-1") / 2 }
```

### Jitter around a fixed base

```text
{ "delay_ms": jitter(150, 40) }
```

### Jitter around catalog path-extra

```text
{ "delay_ms": jitter(route("aws-eu-central-1"), 20) }
```

### Addition: catalog extra plus a jittered constant

Useful when you want PathExtra **and** an independent lab offset (here `jitter(150, 40)` ≈ 110–190 ms on top of `route`):

```text
{ "delay_ms": route("aws-eu-central-1") + jitter(150, 40) }
```

### Skip delay for WebSocket / Cloudflare; soften CloudFront

```text
if websocket {
  { "delay_ms": 0 }
} else {
  if cloudflare {
    { "delay_ms": 0 }
  } else {
    if cloudfront {
      { "delay_ms": jitter(route("aws-eu-central-1"), 20) }
    } else {
      { "delay_ms": route("aws-eu-central-1") }
    }
  }
}
```

### Branching on CDN / path / headers

Manual header checks still work (e.g. when you need a custom signal):

```text
if lower(header(response, "via")) contains "cloudfront" {
  { "delay_ms": jitter(route("aws-eu-central-1"), 20) }
} else {
  if match("^api\\.example\\.com$", host) && match("^/v1/", path) {
    { "delay_ms": route("aws-eu-central-1") / 2 }
  } else {
    if header(request, "x-env") in ["staging", "perf"] {
      { "delay_ms": route("aws-eu-central-1") + jitter(150, 40) }
    } else {
      { "delay_ms": 0 }
    }
  }
}
```

Default if missing/empty file:

```text
if passthrough {
  { "delay_ms": 0 }
} else {
  { "delay_ms": route("cf") }
}
```

`route("cf")` is always `0` (same edge baseline) — last-mile only until you change the script.

`GET /v1/rules` returns source and compile error (if any).
