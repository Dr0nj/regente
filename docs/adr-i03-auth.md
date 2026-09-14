# I03: selectable authentication and browser sessions

Status: implementation contract, 2026-09-14.

Regente supports individuals and enterprises. `local` (default) supports password
login, `hybrid` enables both local and OIDC login, and `oidc` explicitly requires
SSO. Provider failure never changes the selected policy. Enterprise features are
opt-in; secure identity and session handling apply to every mode.

External identities use issuer and subject. Names, email and provider groups do
not grant existing account access or roles. New accounts receive the explicitly
configured initial role (viewer by default). Administrators explicitly link an
unmapped identity and manage access; changes are recorded transactionally.

OIDC uses coreos/go-oidc and golang.org/x/oauth2 with signature, issuer, audience,
expiry, nonce, state and PKCE S256 validation. Single-use login transactions and
event tickets live in the shared database. Browser sessions use HttpOnly cookies,
server-side revocation and CSRF tokens. CLI password login retains bearer output;
SSO browser sessions never appear in URLs or browser storage.

Federated sessions expire after five minutes, bounded also by ID token expiry.
Reauthentication requests fresh IdP login. This bounds upstream deprovisioning
without keeping refresh/access tokens in the database. Local administrative
revocation applies on the next request and within one second on event sockets.
Group-to-role mapping is deliberately explicit/manual; no group claim elevates
permissions automatically. A separately configured emergency account may obtain
a fifteen-minute session and must already have a non-default local password.

Migration 25 preserves accounts, invalidates legacy human sessions, and records
old federated accounts as requiring assisted linking. There is no automatic name
matching. Stop old servers, back up, upgrade all nodes, then sign in again.

References: https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc and
https://pkg.go.dev/golang.org/x/oauth2 (PKCE and nonce caller responsibilities).
