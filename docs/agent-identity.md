# Agent identity and credential lifecycle

Schema 24 binds every external agent credential to an immutable machine principal:
`agentId`, `environment` and `capabilities`. WebSocket, HTTP long-poll and SSE use
the same policy. Human sessions and the administrative bearer cannot authenticate
as agents; machine credentials cannot access the human administration API.

## Upgrade from schema 23

1. Stop every server and agent, and take a verified database backup. Protect old
   backups: they contain the previous plaintext credentials.
2. Upgrade the server and agent binaries together. Run the server with
   `-migrate-only` first. Schema 24 retires old token values and retains their
   labels, IDs and timestamps as `requires_reissue` records. There is no automatic
   association based on a label, hostname or an agent's self-reported identity.
3. In **Settings > Agents**, issue one credential per verified agent identity.
   Choose its exact ID, environment, capabilities and validity (at most 365 days).
   The reserved embedded identity `SERVER-AGENT` and `web-*` IDs cannot be issued.
4. Copy the secret once into `REGENTE_TOKEN` in the agent service's protected
   environment file. Configure `-id`, `-env` and `-caps` to match the issued scope.
   Restart the service and verify presence and a synthetic job before resuming work.

All three transports require `Authorization: Bearer <agent-token>`. Secrets in
URL query parameters are rejected. Current agents send the header automatically.
An old WS agent must be upgraded even if a new credential has been issued.
Do not put secrets in shell history, command-line arguments, screenshots or logs.
Linux service installers keep the environment file at mode 0600.

An empty environment authorizes **only unlabeled jobs** for external agents.
It is not a wildcard. A labeled job requires the same exact environment, including
case. Capabilities must match the issued set (order is irrelevant); a pinned job
still requires an authorized capability. A changed scope requires a new agent ID
and deliberate updates to definitions and service configuration. The embedded
HTTP/REST server agent remains a separate trusted in-process executor.

## Administrative API

Use an administrator session/bearer with these endpoints; request bodies are JSON:

```text
POST /api/agents/tokens
{
  "label": "worker-01 credential",
  "agentId": "worker-01",
  "environment": "production",
  "capabilities": ["COMMAND", "SCRIPT"],
  "expiresAt": "<future RFC3339 timestamp, within 365 days>"
}

GET /api/agents/tokens
POST /api/agents/tokens/{id}/rotate
{ "expiresAt": "<future RFC3339 timestamp>", "graceSeconds": 300 }

DELETE /api/agents/tokens/{id}
```

Issue and rotate return the new secret once with `Cache-Control: no-store`.
The database stores a SHA-256 digest of a cryptographically random 256-bit token;
listing returns only a short identification prefix and metadata, never the secret
or digest. Expiry is mandatory. Revoked and expired credentials remain visible
for administration. Issuing another credential for an existing ID requires exactly
the same scope; it does not modify the existing principal or revoke other tokens.

Rotation atomically issues a new token and shortens the old token's remaining
validity to the grace period (0–3600 seconds). The old token can be rotated only
once. The UI uses a five-minute grace period; the API allows zero for immediate
replacement. Update the protected service environment and restart the agent within
the grace period. If the new secret was lost, revoke its record and issue another
credential for the same principal. Revoking a token affects that credential; revoke
all active credentials listed for an agent to disable that identity completely.

## Enforcement and evidence

Each node checks the shared database on incoming agent requests/messages and before
dispatch delivery. Active WS/poll/SSE clients also revalidate every second, with a
one-second database query budget; a failed check closes access. The laboratory
target is removal of active connections within five seconds of revocation,
rotation without grace, or expiry. Nodes do not depend on NATS notifications for
this decision. Cluster nodes must share the same PostgreSQL identity store.
Fleet presence on other nodes can lag by the existing presence interval/TTL.

The scheduler records the selected agent before exposing a dispatch to the local
channel or NATS. Result and output handlers require the current `RUNNING` instance
to be assigned to that principal. Unauthorized WS messages close the connection;
HTTP rejects them with 403. Revocation prevents further access but does not undo
external effects or guarantee cancellation of a command already running.

This is current-instance attribution, **not execution-attempt fencing or durable
delivery**. Isolation from late messages of an earlier attempt using the same
agent and instance ID remains part of I08–I10. NATS routing still lacks a durable
delivery acknowledgment. Do not infer exactly-once effects from this change.

`TestMachineIdentity` runs against SQLite and PostgreSQL: claim tampering, cross-agent
result/output, digest-only storage, immutable scope, expiry, rotation overlap and
active revocation on two nodes for WS, long-poll and SSE. Migration tests cover
v22/v23 retirement and rejection by the previous runtime. The mandatory integration
runner also revokes an agent connected to one real server process through the
other node and measures the observed removal time. See the
[integration runbook](integration-baseline.md) for reproduction and evidence.
