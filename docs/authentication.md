# Authentication: local, hybrid or SSO

Regente supports personal installations and enterprise deployments with the same
product. Choose with `-auth-mode` or `REGENTE_AUTH_MODE`:

| Mode | Username/password | OIDC SSO | Provider unavailable |
| --- | --- | --- | --- |
| `local` (default) | Enabled | Disabled | Local login continues |
| `hybrid` | Enabled | Enabled | Local login continues; SSO unavailable |
| `oidc` | Disabled | Required | Normal login remains blocked |

To retain the behavior of older installations configured with `oidc`, explicitly
select `hybrid` during upgrade. Configure every node with the same policy.

## Local installation

Run the server normally. On a new database, sign in as `admin` / `admin` and change
the initial password. Create accounts from **Users**. No external identity
provider or enterprise infrastructure is required.

## Enable both methods

Register an Authorization Code client with PKCE S256 and an exact redirect URI:

```sh
REGENTE_AUTH_MODE=hybrid
REGENTE_OIDC_ISSUER=https://id.example/realms/regente
REGENTE_OIDC_CLIENT_ID=regente
REGENTE_OIDC_REDIRECT_URL=https://regente.example/api/auth/oidc/callback
REGENTE_APP_URL=https://regente.example
REGENTE_OIDC_DEFAULT_ROLE=viewer
```

Supply `REGENTE_OIDC_CLIENT_SECRET` through the service's protected environment or
the existing secrets provider (`oidc_client_secret`), never Git or URLs. Restart
after changing configuration. The login page shows SSO and username/password.
Set `REGENTE_AUTH_MODE=oidc` to require SSO instead.

Discovery, authorization, token and JWKS endpoints require HTTPS; HTTP loopback
supports development. Discovery failure preserves the policy. Fix the provider
and restart to restore SSO. Validation covers signature, issuer, audience,
authorized party, expiry, subject, nonce and PKCE. Shared-database state is
single-use and expires after five minutes.

## Identity and deprovisioning

Accounts use **issuer + subject**, never display name or email. A provider user
named `admin` cannot inherit local admin access. New accounts get the configured
initial role (viewer by default); changing the default does not change existing
accounts. Provider groups do not automatically grant roles. Manage roles and
folder ACLs explicitly in **Users**.

To connect an existing account, choose **Link SSO**, enter the exact issuer and
subject verified at the provider, and confirm ownership. Existing mappings cannot
be overwritten. Linking and access changes are recorded transactionally in
`identity_audit`. **Disable** blocks both login methods and revokes all sessions;
enabling the account again does not revive them.

Provider-side deprovisioning is bounded by the five-minute federated session
lifetime (or ID token expiry, if earlier). No refresh token or silent extension
is retained: subsequent login requests fresh IdP authentication. This is not
instantaneous synchronization; provider policy governs acceptance of new logins.

## Sessions and integrations

Browser sessions use HttpOnly, SameSite=Lax cookies. HTTPS adds Secure and the
`__Host-` prefix; forwarded TLS is accepted only from configured trusted proxies.
Set `REGENTE_APP_URL` to the public frontend origin. Cross-origin browser access
is limited to that origin; use same-site hosting so cookies can be sent.

Local sessions last seven days; SSO sessions at most five minutes. Logout,
password change/reset and disabling a user revoke server-side sessions. Only
SHA-256 session hashes are stored. No browser session appears in localStorage
or URL fragments.

CLI/API clients retain `POST /api/auth/login` with `{username,password}` returning
`{token,user}` in local/hybrid mode. Send `Authorization: Bearer` on subsequent
requests. Browser callers add `browser:true`; JSON contains an empty token and
the server sets the cookie. Cookie mutations require `X-CSRF-Token`, returned by
login or `GET /api/auth/me`. Browser cookies cannot be used as API bearers. Query
string tokens are no longer accepted.

For events, call authenticated `POST /api/auth/event-ticket` and connect to
`/ws/web?ticket=...` within 30 seconds. Each ticket can be used once; get a new one
on reconnect. Sessions are checked before delivery and every second. Folder
filtering remains tracked in I04.

## Optional emergency access

Before requiring SSO, create a dedicated local administrator, change its initial
password, and store it under your recovery procedure. Configure
`REGENTE_AUTH_EMERGENCY_USER` with that username on each node. This enables the
separate **Emergency access** choice. Normal password login stays blocked in
`oidc` mode. The account must be an administrator with its password changed;
password `admin` is refused. Sessions last 15 minutes and login is audited as
`emergency`. Do not reuse an SSO-linked account. An empty configuration disables
emergency access; removing it also invalidates existing emergency sessions.

API recovery adds `emergency:true` to the login request. The static administrative
token cannot bypass SSO-only mode. Exercise recovery before an outage.

## Upgrade from schema 24

Back up and stop old servers. Upgrade all nodes together; old binaries refuse
schema 25. Accounts, passwords, definitions and machine credentials are preserved.
**Human sessions are revoked:** users and human API integrations must sign in again.

Legacy federated accounts have no proven issuer/subject association and are marked
**SSO linking required**. Verify and link each identity explicitly before resuming
access; never infer it from names/email. Rehearse on a database copy. Rollback
requires a compatible binary or verified backup with reconciliation; destructive
downgrade is not supported.

## Verification

Go tests cover signed-token negative cases, identity collisions, linking, CSRF,
transport separation, revocation and upgrades. Mandatory integration exercises
SQLite/PostgreSQL, Keycloak and two nodes without accepting skipped tests.
Playwright uses a real server for all three policies, initial password change,
session reload and logout.
