# Local Inference: The Hub

*First spec of the [local inference](roadmap/local-inference.md) epic. Its work order will be `local-inference-hub-work-order.md`. Calls marked **(proposed)** are the design agent's and have not yet been confirmed by Michael; everything else was decided in the design session.*

## What This Is

The epic turns the gateway into an always-on hub in front of private inference hosts. This spec is the part where the hub comes to know itself: which endpoint serves which model, what state each is in, and what the traffic through it has been doing.

Three things are built on that knowledge. Routing sends a request only where its model lives. A status surface reports what the hub knows. And a read-only level of virtual key may look at it.

Nothing here starts or stops anything. Control arrives in the next spec, with the per-host harness. But everything that spec and the desktop client will need to show is defined here, so this is the ground the rest of the epic stands on.

## The Lab This Is Built For

Four hosts on one network, none of which sleep. A 3090 and a 5090 each run an engine serving `qwen3.8-27b`, one under vLLM and one custom. A 2080ti runs whisper and embeddings, and a 4090 sits idle. Engines get stopped by hand when a user-facing application needs a GPU.

Today's gateway treats `providers.local.endpoints` as a homogeneous pool. It rotates through healthy endpoints without looking at the model a request names, while `/v1/models` advertises the union of what every endpoint lists. In any lab where hosts carry different models, the gateway advertises models it then delivers only by luck. A request lands on a host that doesn't have the model, that host's not-found error comes back, and because an application error travels with the request it is correctly not retried elsewhere.

The same flaw appears in two more places. The embedding and classifier calls that semantic routing makes ride a second copy of the same rotation and miss the same way. And provider selection, one layer up, reads a model's name rather than its location, so a local `gpt-oss:20b` is sent to OpenAI.

Meanwhile the hub can't say what it knows. Health exists only as log lines and a Prometheus gauge, and `/health` answers `ok` unconditionally. The probe that maintains health already asks each endpoint for its model list on every interval... and throws the answer away.

## The Catalog

The catalog is the hub's record of which endpoint serves which model. Each local endpoint contributes the model ids it serves, in one of two ways.

By default they are *discovered*: the model list the health probe already fetches is kept. Where an endpoint's own listing can't be trusted — an engine that answers to a name it doesn't list — the operator *declares* them:

```yaml
providers:
  local:
    endpoints:
      - name: box-3090
        base_url: "http://10.0.0.30:8000"
      - name: box-5090
        base_url: "http://10.0.0.50:9000"
        weight: 3
        models: ["qwen3.8-27b"]
```

A declaration replaces discovery for that endpoint rather than adding to it, so the answer to "why did this request go there" is always readable from one place. When a declared endpoint's own listing disagrees with its declaration, the hub says so in status and in the log, and keeps to the declaration. Merging the two was considered and set aside for that reason.

A declared endpoint's models stay in the catalog while the endpoint is unhealthy. That is what lets the hub say a model is known but unavailable, and it is what the next spec needs in order to know what a stopped engine would serve.

The endpoints serving a given model id form that model's *pool*. Inside a pool nothing changes: weighted rotation, health, and failover on network errors work as they do today, scoped to the pool's members. The single-`base_url` form of the local provider is a pool of one, not a separate path.

The catalog is the only way a request reaches a local endpoint. Chat completions, streams, and the embedding and classifier calls made on the hub's own behalf all select through it.

## Routing a Request

The decision sits exactly where provider dispatch sits today: after semantic routing and capability aliases have resolved the model, and after the key's permission check. The hub then decides in a fixed order.

1. If the local catalog holds the model id, the request goes to that model's pool.
2. Otherwise, if the name matches a hosted provider's prefix and that provider is configured, it goes there, as today.
3. Otherwise the hub refuses it.

Local-first is what fixes the naming collision. A model the lab serves is the lab's, whatever it is called.

The third step is a deliberate change. Today any name that isn't a hosted prefix is forwarded to a local backend to see what happens. From here, a name no endpoint serves is answered by the hub itself.

There are two refusals, both OpenAI-shaped and both originating at the hub. A model the catalog doesn't hold is a 404 `not_found_error`. A model the catalog holds, with no endpoint able to serve it, is a 503 `service_unavailable` naming the model and the reason. Refusals are counted for the status surface and, like every request rejected before dispatch today, write no usage record.

When every member of a pool is marked unhealthy, the hub still makes one attempt before answering 503, as it does today for the whole pool. Health marks can lag recovery by minutes under probe backoff, and a pool of one is the common case in a lab. **(proposed)**

`/v1/models` is answered from the catalog: the local models a request would succeed for right now, plus the hosted providers' lists and `auto` as today. It no longer queries every endpoint on each call. **(proposed)**

## The Status Surface

What the hub knows, made readable. It has three kinds of content.

The *snapshot* is the present tense. Every endpoint with its health, its last probe and how long that probe took, its models and whether they were declared or discovered, and any declaration mismatch. Every model with its pool and whether it is servable right now. Requests in flight, hub uptime, and version.

The *window* is a bounded history held in memory. Per endpoint and per model it keeps request and error rates, time to first token, tokens per second, and duration, alongside health transitions and refusal counts by reason. It is fixed in size and lost on restart. That is acceptable for now; a persistent store is deferred.

The *request feed* is the most recent requests, each carrying what a usage record carries plus the endpoint that served it and its timing.

Everything is measured once. The hub already has a single point where each completed request is metered. That point gains the serving endpoint, time to first token, and tokens per second, and it feeds all three outputs: the Prometheus instruments, the usage records, and the window. There is no second measurement path. **(proposed)**

The surface lives under its own path prefix beside `/v1`, on the same listeners; `/gateway/v1` is the working name. No OpenAI route or response changes shape, and the serving endpoint is not added to a completion.

The surface returns data, not presentation. Rendering belongs to its readers. The first reader is a small `llm-gateway status` command **(proposed)**; the second will be the desktop client.

## Levels

A virtual key carries a level, and the level decides what it may read here.

- **consumer** is the default and is what every key is today: inference only.
- **read-only** may also read the snapshot and the window.
- **root** may also read the request feed and anything broken down by key. Control joins this level in the next spec.

A read-only key never sees a key name or a per-key number. Someone sharing the lab's capacity can find out whether a host is up without learning who else is using it.

Levels are independent of restrictions. `allowed_models` and `allowed_routes` go on governing inference exactly as they do now; the level governs only the status surface.

```yaml
api_keys:
  enabled: true
  keys:
    - name: michael
      key: "${OWNER_KEY}"
      level: root
    - name: guest
      key: "${GUEST_KEY}"
      level: read-only
      allowed_models: ["qwen3.8-27b"]
```

Root comes only from the gateway's own config, never from a file or HTTP key source, so a remote key source cannot mint an owner. This spec extends the same rule to read-only: source records cannot carry a level at all. That leaves the published key-source contract untouched. The cost is that adding or removing a read-only key means a restart, which also empties the window.

With authentication disabled the gateway is open, as today, and every caller is treated as read-only. The root views need keys enabled. **(proposed)**

`/metrics` is left as it is: exempt from authentication, and off unless the operator enables it. That leaves a known gap. With the exporter on, anyone who can reach the door can read per-key counters, a guest included. This is an accepted residual; the Deferred section says why it isn't closed here.

## Scenarios

**Two engines, one model.** The 3090 and the 5090 both serve `qwen3.8-27b`, weighted 1 to 3. Requests for it rotate between those two hosts and never reach the 2080ti. The snapshot shows one model with a pool of two.

**An engine stopped by hand.** A user-facing application needs the 5090, and its engine is stopped over SSH. A request in flight to it fails over to the 3090. Within a probe interval the snapshot shows the 5090 endpoint unhealthy and `qwen3.8-27b` still servable. When the engine is started again it rejoins the pool. Nobody touched the hub.

**The only engine for a model is down.** The pool has one member and it is marked unhealthy. The hub tries it once anyway. If that fails, the client gets a 503 that names the model, not a timeout and not another host's error.

**A name nobody serves.** An assistant is configured with `qwen3.8-27`. The hub answers 404 at once and no engine is touched. The refusal shows in the window, so the owner can see that something keeps asking for a model that isn't there.

**A local model with a hosted-sounding name.** `gpt-oss:20b` is brought up on the 4090. The catalog holds it, so it is served locally, whether or not OpenAI is configured.

**A guest looks at the lab.** A guest key at read-only level, restricted to one model, runs `llm-gateway status` and sees four hosts, their health, and which models are servable. It sees no key names and no feed of other people's requests. A completion for any other model is refused with 403, as today.

## Seam Census

Each line is a boundary, the call, why, and what would reopen it.

- **Status data and its rendering.** Separate. The hub returns data and never formats for display, because two readers are planned from the start. Nothing foreseeable reopens this.
- **The catalog as the only route to a local endpoint.** Enforced. Chat, streams, embeddings, and the classifier all select through it; a second selection path is how the model-blind rotation came to exist twice. Nothing foreseeable reopens this.
- **The OpenAI front door.** Enforced. The gateway-native surface lives under its own prefix and no OpenAI route or response gains a field. The `openai-front-door` quality needs a boundary statement for a gateway-native surface; that edit is Michael's, in terminus-canon.
- **One metering point.** Enforced. Instruments, usage records, and the window are fed from the same measurement. **(proposed)**
- **The permission check's position.** Unchanged. Choosing an endpoint inside a pool is not model resolution; the model that runs is the model that was checked. This follows `deny-not-reroute`.
- **Status wire types and the model behind them.** Separate. The catalog and the status content know nothing of how they are served, and the new surface's wire types are bound with `dd` from the start rather than grown on the `providers` package's `encoding/json` types. **(proposed)**
- **Errors by tier.** At boot, a malformed declaration, a duplicate endpoint name, or an unknown level fails startup with a directed error. On the request path, a refusal is an OpenAI-shaped 404 or 503 and never ends the process. In the probe loop, failures are logged and the loop continues. A declaration mismatch is a status-visible warning and never a boot failure, since a backend may simply be down at boot.
- **One door, level on the key.** Fused. The owner is assumed to be the only root user, so a separate control door buys nothing yet. Reopen when root is offered over a public share.
- **Where elevated levels come from.** Config only, for root and read-only alike. A remote key source must not be able to mint an owner, and keeping levels out of source records leaves the published key-source contract untouched. Reopen when keys above consumer need to change without a restart, or need to come from a management plane.

## What Changes for Existing Deployments

Three behaviors change, and each wants a changelog entry.

A model name that no local endpoint serves and no hosted prefix matches is refused, where it used to be forwarded. A backend that answers to names it doesn't list — a server that ignores the model field is the common case — needs a `models:` declaration.

A locally served model wins over a hosted prefix.

`/v1/models` reflects what is servable now, where it used to be the union of whatever the healthy endpoints listed at the moment of the call.

## Deferred (and Why)

- **Endpoint states beyond healthy and unhealthy.** Stopped and claimed need the harness to tell them apart from failed. The snapshot's endpoint state is designed to take more values; the next spec adds them.
- **Catalog aliases.** Both engines in the lab report the same model id. An alias would be a new resolution step that must land before the permission check, so it waits for a case that needs it.
- **Per-key filtering of `/v1/models`.** Already carded. The catalog makes it cheap, and the guest scenario would benefit, but the hub is provable without it.
- **Embeddings and transcription routes for clients.** The 2080ti's services are inventory, not part of the example. Internal embedding calls for semantic routing do obey the catalog.
- **Booting without overlay-dialed backends.** Only an overlay-dialed endpoint can fail the boot, and the lab has none. Building it would also need an amendment to `explicit-config-fails-loud`.
- **A persistent store for history.** The in-memory window is enough to prove the surface.
- **Pushed status updates.** Polling serves a command-line reader, and the [desktop client](local-inference-client.md) polls too.
- **Authentication on `/metrics`.** The levels are a ladder, and a scraper wants per-key numbers without control, which the ladder doesn't express. Left as the residual named above.
- **Elevated keys that reload.** Follows from keeping levels out of source records.
- **Telling an inference client which endpoint served it.** Root reads it from the request feed; a consumer has no need for the lab's host names.
- **Credentials toward local endpoints.** The local provider sends no `Authorization` header upstream. It matters once a gateway dials another gateway. The desktop client is not one; it passes the caller's key through.
- **Opt-in hosted fallback.** Nothing crosses from local to hosted on failure. That is the guarantee, and this spec keeps it.
