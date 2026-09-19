---
title: Limitations
weight: 80
---

PotatoNetwork is **not** a 100% reproduction of a real user’s network in that country. It gets **close** by applying delay, loss, and bandwidth drawn from statistical last-mile / RTT data for real locations (plus optional path delay from your rules), so apps and waterfalls usually feel believable.

It does **not** model live path quirks of that geography, for example:

- Backbone cuts, peering disputes, or regional outages
- Transient CDN / PoP congestion and cache-miss variance
- ISP traffic shaping, CGNAT quirks, or middlebox behavior unique to a carrier
- Packet reordering, jitter distributions, or wireless radio effects beyond simple netem loss/delay
- Exact DNS resolver topology or anycast routing from that country

Use it for labs, QA, and “good enough” distance/last-mile stress — not as a substitute for real probes or on-the-ground measurements when those specifics matter.

**WebSockets:** `ws://` and `wss://` through transparent MITM are supported (HTTP/1.1 `101` then a raw bidirectional tunnel; both sides are closed when either direction ends). Path delay from `rules.expr` applies to the upgrade handshake only, not to individual frames. WebSocket over HTTP/2 and per-frame shaping are not implemented.
