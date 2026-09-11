# Agent authentication and upgrades

All external agent transports require a dedicated credential bound to an agent ID,
environment and capability set. Human sessions and the server API bearer cannot
authenticate machines. Expired, revoked and unbound legacy tokens are rejected.

In **Settings > Agents**, issue a token with the verified identity and an expiry,
then copy its one-time secret into the protected `REGENTE_TOKEN` service environment.
Configure `-id`, `-env` and `-caps` to match the provisioned scope and restart the agent.
Verify presence and a harmless job's dispatch, output and completion.

**Upgrading to schema 24 requires reissuing all schema-23 agent tokens and upgrading
both server and agent binaries.** Stop old processes and take a verified backup first.
Legacy token metadata remains listed as `requires_reissue`; its old secret is retired.
The previous I02a compatibility statement (tokens remaining valid) applies only to
the earlier schema-23 release, not this upgrade.

Use Authorization headers; query-string credentials are no longer accepted.
All three transports—WS, long-poll and SSE—share identity, attribution and active
revocation checks. Do not restore administrative/shared-token authentication as a
workaround for an agent that cannot reconnect.

See [Agent identity and credential lifecycle](agent-identity.md) for the upgrade
procedure, API contract, expiry, rotation grace periods, active revocation and
the boundary between current-instance attribution and future attempt fencing.
