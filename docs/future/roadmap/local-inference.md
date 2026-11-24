---
title: local inference
state: researching
created: 2026-11-24
tags: [epic]
log:
  - stamp: 2026-11-24
    note: design session, decisions and reasons — docs/journal/2026-11-24.md
  - stamp: 2026-11-24
    note: hub spec drawn — docs/future/local-inference-hub.md
---

Use the models on your own hardware from wherever you work. Make the gateway an always-on hub in front of private inference hosts: it knows which engine serves which model, reports its own state, and lets its owner operate the hardware from a laptop — reached over zrok, never as a public model API.

The work lives on the `local-inference` branch until a working example has been shown. The example below is the acceptance target; the branch merges to `main`, or does not, on the strength of it.

## Discussion

The example runs on four LAN hosts behind one hub, with a laptop connected over a zrok private share:

1. **Connect** — open the desktop client away from home; it connects to the hub, and the assistant points at localhost.
2. **See** — the client shows each host, its GPU, and which engines and models live where, with the hub's request and token statistics.
3. **Use** — a request for a model is served by a host that has it; the client shows the request, the host that served it, and the tokens.
4. **Operate** — claim a GPU for another application: its engines drain and stop, requests for a model with a second engine keep flowing, and release restores what the claim stopped.
5. **Share** — hand someone a key that can use one model and see status, and change nothing.

Built as specs in order, each with its own work order:

- **hub** — per-model routing from a catalog, a status surface with a bounded in-memory history, and the read-only key level.
- **harness and control** — a small per-host harness with declared engines, start and stop, GPU claims owned by the host, and the root key level.
- **desktop client** — connect, observe, and control from the laptop, built on `dfw`.
- **raw ziti** — a third overlay transport beside zrok and Agora; last, and outside the example.

Deferred: embeddings and transcription routes, opt-in hosted fallback, standby engines that start when a claim removes capacity, engines started by an arriving request, booting without overlay-dialed backends, and a persistent store for history.
