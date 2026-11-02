# Safe first startup

Normal daemon startup creates no known administrative bearer and no `admin/admin`
account. An empty user table requires an explicit bootstrap password. Existing
accounts are preserved, including across development restarts; production still
rejects known weak legacy passwords until the operator rotates them.

## Guided local installation

With a trusted server binary, run:

```sh
regente-server -init-config ./server.env
regente-server -config-env ./server.env -check-config
regente-server -config-env ./server.env
```

The assistant asks for an environment name and port, then writes a **new** private
file with production profile, numeric loopback, no administrative bearer, no built-in
executor and a random 192-bit bootstrap password. It never prints that password.
Read `REGENTE_BOOTSTRAP_PASSWORD` privately, sign in as `admin`, then remove the
password entry. Restart does not require the bootstrap secret or replace accounts.
`-init-config` refuses an existing target, including symlinks. It never migrates a DB.

On Unix the file is mode 0600. On Windows its protected DACL grants only its creator
and SYSTEM; the installer runs the task as SYSTEM. Do not grant Users/Everyone read
access. `-config-env` checks file permissions before reading. The format is literal
`REGENTE_NAME=value`, not shell code: no interpolation, quoting or command execution.
Duplicate/invalid keys are rejected before applying values. Explicit process
environment overrides the file; explicit command flags override both. Avoid conflicting
inherited `REGENTE_` values, especially when inspecting generated configurations.

Linux installation creates this private configuration only when absent, preserves
existing operator configuration and prints the SSH tunnel access path. `regente-configure`
leaves an existing file untouched; a new destination invokes the same assistant.
Windows installation accepts `-Config PATH`, generates it if absent, validates it,
then registers the Scheduled Task. Secrets are not embedded in task arguments.

## Development and existing installations

An unconfigured `regente-server` refuses an empty account store. Supply a strong
`REGENTE_BOOTSTRAP_PASSWORD` through a private environment or use the assistant.
The daemon's administrative token default is empty. An explicitly supplied token
remains a development compatibility option, never a production credential.

For disposable, local fixtures only:

```sh
regente-server -insecure-dev-auth -addr 127.0.0.1:8080
# or the disposable CLI command:
regente dev daily
```

These explicit fixtures use `admin/admin`, requiring password change. Development
is restricted to numeric loopback; `0.0.0.0`, `:8080`, DNS aliases and public/private
network addresses are refused before opening the DB/workspace. `-insecure-dev-auth`
is rejected with production. Existing development services bound to network
interfaces must deliberately convert using the [production profile](production-profile.md);
the installer does not silently rewrite their network/security policy.

## Containers

A container's loopback is local to its network namespace. Publishing a Docker port
does not make a loopback listener reachable from the host. Do not solve this by
exposing a development fixture. For a host-local Linux container, use host networking
and a protected configuration owned by the container UID; persist `/data`:

```sh
# Prepare config with the trusted host binary; it is never baked into the image.
regente-server -init-config ./server.env
# Ensure server.env is readable only by the image UID (65532), and /data is writable.
docker run --rm --network host --mount type=bind,src="$PWD/server.env",dst=/run/regente.env,readonly \
  --mount type=volume,src=regente-data,dst=/data regente-server -config-env /run/regente.env
```

For a bridge network, Kubernetes or another machine, explicitly configure production
TLS (`REGENTE_NETWORK_BOUNDARY=tls`, numeric bind, matching HTTPS app origin and
certificate/key mounts). Supply bootstrap credentials via the platform secret store,
remove them after first login, and preserve the external audit key and DB/workspace.
The historical Knative manifest is not a current deployment recipe: its public
development bind is rejected. A platform-specific production boundary must be
reviewed rather than assuming a generic TLS/container recipe configures Knative ingress. See [production boundary](production-profile.md) and
[recovery set](dr-backup.md) before exposing a deployment.
