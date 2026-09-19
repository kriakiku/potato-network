---
title: Overview
weight: 1
aliases:
  - /overview
cascade:
  type: docs
---

**Last-mile network lab for Docker** — make browsers, scrapers, and APIs feel like they run from Bangladesh, Brazil, or Japan without VPNs, agents, or a heavy proxy stack.

PotatoNetwork is **open source** under the [MIT License](https://github.com/kriakiku/potato-network/blob/main/LICENSE).

Attach workloads with `network_mode: service:potatonetwork`, pick a country profile over a tiny JSON API, and every packet in that netns — including DNS — gets the delay, loss, and bandwidth of real last-mile. Transparent HTTPS MITM adds path delay on top (edge vs origin), so Waterfall timings look believable under interception.

[API](api) · [Rules](rules) · [CA](ca) · [Examples](examples) · [Profiles](profiles-gallery) · [Limitations](limitations)

---

## Architecture

```text
sidecar ──► PotatoNetwork netns
              ├─ DNS :53 (shaped) → Docker/resolv upstream (exempt)
              ├─ netem on uplink via netlink (shaped)
              ├─ transparent MITM 80/443 (nftables redirect) → expr path delay
              └─ API :7783 (exempt — no lab delay)
```

- **No WireGuard** inside PotatoNetwork — for phones, run a separate WG container with `network_mode: service:potatonetwork`.
- **No web UI** — JSON API only.
- **Go only** — no Python / mitmproxy.

## Layers

1. **Last-mile (netlink netem)** — one-way delay ≈ `(country_cf_rtt − host_cf_rtt) / 2`, plus loss and rates from the catalog tier.
2. **Path delay (`rules.expr`)** — after origin response headers, sleep `delay_ms` from the script (`route("aws-…")`, `jitter(…)`, literals, arithmetic).
3. **TLS handshake delay** — extra sleep before local MITM ServerHello so client-visible SSL time is not ~0 under MITM.

API traffic and DNS **upstream** queries are fwmark-exempt from netem (nftables mark + netlink fw filter). Redirect uses nftables NAT. Optional `POTATONETWORK_SHAPE_EXCLUDE` (IPv4/CIDRs) applies the same full bypass — no netem and no MITM — for listed destinations.

---

## What it does

| Capability | Why it matters |
|------------|----------------|
| **Country last-mile** | Delay, loss, down/up rates from a Radar-backed catalog (stable / typical / poor tiers) |
| **Shared Docker netns** | Sidecars inherit shaping automatically — no SOCKS config, no per-app agents |
| **Shaped DNS** | Local `:53` forwarder so lookups suffer the same last-mile as TCP |
| **Transparent MITM** | nftables REDIRECT of 80/443 → in-process TLS terminator; path delay via [`rules.expr`](rules); `ws`/`wss` upgraded to a duplex tunnel after `101` |
| **Per-endpoint path rules** | Same host, different delay: e.g. static already on a Cloudflare edge vs `/api/*` that still hits AWS |
| **Host baseline** | Persisted RTT probe so delay = country − *your* edge, not absolute fiction |
| **API-only control** | `PUT /v1/profile`, CA download, catalog refresh — CI-friendly, no panel |

Typical loop: probe baseline → set `{"country":"BD","tier":"typical"}` → run Playwright / curl / your service in the same network namespace.

Flexible [`rules.expr`](rules) policies can slow traffic **by path (or headers) on one domain**: return `{ "delay_ms": 0 }` for edge/static, or `{ "delay_ms": route("aws-…") }` / `jitter(150, 40)` for API origin hops. That matches setups where assets are already on a Cloudflare edge node but backend traffic still goes to AWS.

Phones and non-Docker clients: run any WireGuard (or other VPN) container with `network_mode: service:potatonetwork` — PotatoNetwork stays the shaping core, not a VPN product.

---

## Stack (deliberately thin)

- **One static Go binary** — no Python, no mitmproxy, no Node UI
- **Dataplane in-kernel** — [netlink](https://github.com/vishvananda/netlink) HTB/netem/IFB for shaping; [google/nftables](https://github.com/google/nftables) for marks, REDIRECT, QUIC drop
- **DNS** — [miekg/dns](https://github.com/miekg/dns) forwarder; upstream from Docker’s resolv / Compose `dns:`
- **Policy** — [expr-lang](https://github.com/expr-lang/expr) scripts for path delay (dest, Via, CloudFront, …)
- **Trust** — embedded Mozilla roots ([breml/rootcerts](https://github.com/breml/rootcerts)); auto-minted MITM CA on the data volume

Control plane is a small HTTP API on `:7783` (SPUD on a phone keypad), fwmark-exempt so labs don’t shape their own remote.

---

## Lightweight on purpose

Runtime image is **`scratch` + UPX-compressed binary**:

- Catalog and CA roots are **embedded** — no `apt`, no `ca-certificates`, no `tc`/`iptables` CLI helpers
- No web UI assets, no sidecar proxy process, no WireGuard daemon inside the image
- Needs only **`NET_ADMIN`** (+ `ifb` on the host kernel) and a data volume

What you *don’t* ship: a GUI, a VPN mesh, or a multi-service compose of “emulator + mitm + DNS + panel”. One container owns the netns; everything else plugs in.

---

## Who it’s for

- **QA / perf** — “does checkout still work on a poor mobile last-mile?”
- **Scraping / automation** — see timeouts and CDN behavior as a distant user would
- **CDN / edge debugging** — path extra latency when the origin sits farther than CF
- **CI** — token-gated API, cron-refreshable catalog and baseline, docs on GitHub Pages

See also [Limitations](limitations) — statistical emulation, not a live replica of a country’s internet. Start with [Examples](examples) for a Playwright sidecar.
