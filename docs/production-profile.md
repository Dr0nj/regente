# Production profile

The production profile is opt-in: set `REGENTE_PROFILE=production` (or
`-profile production`). Development remains the default for existing installs.
Production does not imply mandatory SSO: choose local, hybrid or oidc explicitly.
Use oidc when SSO must be required; provider unavailability never enables normal
password login. An explicitly configured emergency administrator is the exception.

## Configuration contract

Flags override environment variables. These requirements are checked before opening
the database, cloning a workspace, registering agents or starting the scheduler.

| Setting | Production requirement |
|---|---|
| `REGENTE_PROFILE` / `-profile` | `production` |
| `REGENTE_ENVIRONMENT` / `-environment` | Explicit scope, 1–64 letters/digits/dots/underscores/hyphens; first character alphanumeric |
| `REGENTE_TOKEN` / `-api-token` | Empty or unset; all legacy static admin tokens are forbidden |
| `REGENTE_DEMO_MODE` / `-demo-mode` | Off; environment booleans accept only 0 or 1 |
| `REGENTE_NETWORK_BOUNDARY` / `-network-boundary` | Explicit `loopback`, `proxy` or `tls` |
| `REGENTE_ADDR` / `-addr` | Explicit numeric IP and port, including brackets for IPv6 |
| `REGENTE_APP_URL` / `-app-url` | Origin without credentials, query, fragment or application subpath |
| `REGENTE_AUTH_MODE` / `-auth-mode` | `local` (default), `hybrid` or `oidc` |
| `REGENTE_CONTROL_PLANE_EXECUTION` / `-control-plane-execution` | `deny` (default), `http` or `http-ssh` |
| `REGENTE_SERVER_AGENT` / `-server-agent` | Off by default under deny; enabled by http/http-ssh unless explicitly disabled |
| `REGENTE_BOOTSTRAP_USER` | Initial administrator name, default admin; used only for an empty user table |
| `REGENTE_BOOTSTRAP_PASSWORD` | Required on an empty user table; 12–72 bytes, no surrounding whitespace |

Local passwords must satisfy that length rule at login, creation and reset. The
bootstrap never overwrites an existing account. Startup rejects active local
accounts with invalid hashes or known development passwords. Old hashes cannot
prove original password length: rotate existing local passwords before conversion.

### Network boundaries

| Boundary | Listen address | Public app URL | Additional requirements |
|---|---|---|---|
| loopback | Loopback IP only | HTTP localhost/loopback | Local access or SSH tunnel; no server TLS |
| proxy | Loopback IP only | HTTPS | Explicit loopback trusted proxies; TLS terminates at a colocated proxy |
| tls | Explicit numeric IP | HTTPS | Loadable certificate/key; optional client CA |

Trusting all proxy addresses is rejected. Proxy mode validates the configuration,
not the external proxy's certificate, DNS or firewall. Configure and verify those
before admitting traffic. For a proxy on another host, use the tls boundary and
explicit trusted proxy addresses with TLS between proxy and server.

Hybrid/oidc require an issuer, client ID, valid initial role and a callback at
`<app-origin>/api/auth/oidc/callback`. The issuer must use HTTPS; loopback mode
also accepts an HTTP loopback issuer for isolated rehearsals. Local mode rejects
leftover OIDC issuer/client/callback settings. OIDC client secrets use the existing
secret provider. See [authentication](authentication.md).

### Execution and environment scope

Each production database is bound to one environment. Jobs and machine credentials
must match it exactly. Empty environment is never a wildcard. Provision agents
with that environment and matching transport claims using user-session credentials.
The display-only `env_label` setting does not authorize routing.

Under deny, SERVER-AGENT is off and agentless SSH is blocked. The http policy
allows the embedded HTTP/REST executor; http-ssh also allows agentless SSH on the
server. External agents remain available under all three policies. This policy
applies to scheduled runs, Force, Run Now and retries. Explain reports
`CONFIGURATION_BLOCKED`; blocked orders retain their snapshots and stay WAITING.

This is an execution boundary, not a multi-tenant API visibility filter. It does
not provide the secrets lifecycle, process isolation or full mTLS hardening planned
separately. Do not mix old binaries or differently configured nodes into the same
production database or distributed bus.

## Clean installation

Keep the service stopped and the port inaccessible while configuring a new install.
The existing installer initially supplies development settings; replace them before
first production startup. For local access through an SSH tunnel:

```dotenv
REGENTE_PROFILE=production
REGENTE_ENVIRONMENT=prod
REGENTE_NETWORK_BOUNDARY=loopback
REGENTE_ADDR=127.0.0.1:8080
REGENTE_APP_URL=http://127.0.0.1:8080
REGENTE_AUTH_MODE=local
REGENTE_TOKEN=
REGENTE_DEMO_MODE=0
REGENTE_SERVER_AGENT=0
REGENTE_CONTROL_PLANE_EXECUTION=deny
```

Supply a unique bootstrap password through a protected service environment, never
as a command-line argument. Start the service, log in, then remove
`REGENTE_BOOTSTRAP_PASSWORD` from the environment. Restarts use the persisted
account. Changing the bootstrap variable later does not reset its password.
For mandatory SSO, configure oidc and your IdP first; arrange an authorized initial
administrator or explicitly select the bootstrap account as the emergency user.

Run `regente-server -check-config` with the same environment as the service.
It validates static configuration and TLS files without touching the database or
starting services. Success does not validate accounts, connectivity or permissions.
A normal startup performs database binding and credential checks before services.
Backup and migrate-only remain maintenance operations and do not start services.

## Convert an existing installation

1. Back up the database and configuration; stop scheduling new work and drain all
   RUNNING instances. Stop every node sharing the database. Conversion refuses a
   database with running instances; do not race an old node against conversion.
2. While still in development, rotate active local passwords and replace admin
   scripts using REGENTE_TOKEN with user sessions. Provision scoped machine tokens
   under new agent IDs where the old principal's environment is empty or different.
3. Publish definitions with the intended environment and executor. Existing orders
   keep frozen snapshots: finish, cancel or deliberately replace incompatible orders;
   editing a live definition does not rewrite their execution identity.
4. Set the explicit production configuration, clear REGENTE_TOKEN, disable demo and
   choose the execution policy. Remove incompatible OIDC settings in local mode.
   For public access, configure proxy or tls and verify the external boundary.
5. Run check-config, then start one node and inspect startup diagnostics. The first
   conversion binds the database to the environment and revokes existing sessions.
   Log in again, provision/reconnect agents and inspect Explain before releasing work.
6. Start remaining nodes with the same profile/environment and intended policy.
   Verify a restart and an authorized scoped job before reopening scheduling.

The binding survives restart and backup/restore. Omitting the profile or changing
its environment refuses startup; it is not editable through the settings API.
Rollback is an explicit recovery operation using the pre-conversion backup and
configuration, after draining/stopping the cluster. Older binaries do not enforce
this marker, so do not use a binary downgrade as a policy bypass.

## Validation evidence

The configuration matrix, real-binary clean startup/conversion/restart, password
bootstrap, scheduler bypass rejection and scoped agent dispatch run in server tests.
The integration gate additionally runs production identity checks on SQLite and
Postgres. The release smoke exercises conversion and upgrade of the installed
systemd service. See [verification](verification.md) for commands and gate limits.

## Durable execution upgrade

Production selects REGENTE_EXECUTION_MODE=durable automatically and rejects legacy mode. Protocol 2 agents require a persistent local journal and EXECUTION_V2 in their exact provisioned capabilities. Schema 28 and the new agent/server binaries must be upgraded together after draining legacy executions. See the [durable execution runbook](durable-execution.md) for backup, rollback, uncertainty and operator resolution.
