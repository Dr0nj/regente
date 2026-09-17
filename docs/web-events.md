# Web event authorization

The web event connection uses a 30-second, single-use ticket issued by
`POST /api/auth/event-ticket`. A reconnect obtains a new ticket. Browser origins
must match the server origin or the configured `-app-url`; clients without an
Origin header still require a valid ticket. Long-lived tokens in URLs and direct
bearer WebSocket handshakes are not supported.

## Access policy

Each connection is tied to its current human session, role and folder ACLs.
The server checks access immediately before writing each message, including
messages already queued. Every node reads the same session/ACL database;
authorization does not depend on receiving a revocation notification over NATS.
The one-second watchdog closes idle connections after access changes. The UI
reconnects, obtains a fresh ticket and reloads its authorized view and role.

Folder semantics match the existing REST policy: administrators have full
access; a non-admin with no ACL entries has global read access; once ACL entries
exist, only exact folder names containing `r` are readable. Removing the final
ACL intentionally restores global read access. To disable a user, use the user
access control, rather than deleting their last ACL. Replacing an ACL set is
atomic; readers cannot observe a temporarily empty set between delete and insert.

An optional `environment` query parameter further narrows the subscription:
omitted means all authorized environments, and an empty value means unlabeled
jobs only. This is a delivery filter, not a new human permission or a tenant
boundary. Environment changes are checked against the stored instance on send.
The standard UI uses the full folder-authorized subscription.

## Event contract

| Event | Delivery rule and payload |
|---|---|
| `instance.changed` | Resolve the stored instance folder/environment; send only its ID, status and supported state fields. Missing or unreadable instances are denied. |
| `instance.deleted` | Capture scope before deletion; send only the ID after checking current ACLs. Internal scope never reaches the browser. |
| `instance.bulk` | An authorized affected scope must exist. Send an empty invalidation; never mixed-folder totals, actors or lists. |
| Definition/folder changes and Git sync | Compare only the user's readable workspace view. Send an empty `_resync` only when that view changes. No definition bodies, names, commit SHAs or rename targets are broadcast. |
| Job alerts and SLA | Authorize using the originating instance. Legacy/system alerts without an instance are limited to global-read subscriptions. Alert counts and resolution aggregates are omitted. |
| `condition.*` | Global-read clients, or a visible instance referencing the condition. Only an empty invalidation is sent. |
| `daily.started` | Business date only; no totals or commit SHA. |
| `settings.changed` | Empty invalidation for all authenticated subscriptions. Values never enter the event bus. |
| Agent, global variable and Git drift events | Global-read subscriptions only, with empty invalidations. Environment-filtered subscriptions do not receive these global notifications. |
| Unknown or malformed events | Denied by default, even for administrators. |

This contract restricts the push channel. Existing globally readable REST
resources remain governed by their own API policies; this change does not
claim multi-tenant isolation or complete enterprise certification.

## Distribution and failure handling

The API, scheduler and Git poller use the configured event bus. A receiving node
applies its own current access policy before delivery. NATS is a trusted internal
transport and must not be exposed to users or agents as a publishing endpoint.

Database or workspace lookup errors do not grant access. A missing scope is not
an unrestricted scope. A slow socket has a bounded write deadline. Notifications
remain best-effort and the queue can drop events under backpressure; reconnect
reloads authoritative state. This package does not introduce durable event replay.

Upgrade all control-plane nodes before claiming I04 protection. An older node
can still expose the previous web contract. No schema migration is required;
rolling back the binary also rolls back this access protection.

## Verification and troubleshooting

- `go test ./server/internal/api -run '^TestWebEvent' -count=1` tests payload
  isolation, projections, origin, workspace updates, queued-message revocation,
  two nodes, ACL/role changes, expiration and reconnection.
- The mandatory integration gate requires SQLite, PostgreSQL and real NATS,
  rejects missing evidence/Skip, and repeats the web matrix with the race detector.
- `cd app && npx playwright test` checks the three authentication modes and an
  actual browser reconnect after ACL replacement, including received frames.
- If a notification is absent, verify the current ACL set, source instance,
  optional environment filter and registered event policy. Unknown event types
  require an explicit policy and tests; do not add a generic pass-through.
- Global notifications suppressed for restricted users are intentional. A
  historical alert without a provable instance must not be assigned to a folder
  using a definition's current name.
- Keep logs free of tickets, session digests, settings values and event contents.
