---
title: gateway-created zrok shares are open
state: inbox
created: 2026-11-25
tags: [enhancement]
log:
  - stamp: 2026-11-25
    note: found in the desktop client design session, and closed decided there — docs/journal/2026-11-25.md
  - stamp: 2026-11-25
    note: carried by the desktop client spec — docs/future/local-inference-client.md
---

Create every zrok share the gateway makes for itself in closed permission mode, public and private alike, as zrok's own commands do. Today `newShare` in `gateway/share.go` passes `OpenPermissionMode`, so any account on the zrok service that learns an ephemeral share's token can reach it.

Give `zrok.share` a list of accounts to grant, passed through as the share's access grants. Without one a closed share answers only its owner's account, and an ephemeral share's token does not live long enough to be granted by hand. No way back to open is planned.

A share the gateway attaches to by `token` was not created by the gateway and is left as its creator made it.

This changes behavior for existing deployments: a client that reaches an ephemeral share from a different zrok account stops working until that account is listed. It wants a changelog entry, and the new field in `docs/current/zrok.md`.

## why

The local-inference epic puts root control behind the gateway's door. Its example serves on a persistent share made with `zrok2 create share`, which is closed, so the example does not depend on this. A hub started on an ephemeral share would be open, with the virtual key as its only gate.
