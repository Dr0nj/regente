# Execution security

The supported I13 profile uses dedicated Linux/systemd execution cells:
certificate-bound machine credentials, protocol 2 journals, local policy and
runtime secrets, and host cgroup controls. It is opt-in; existing development
agents and unrelated adapters retain their previous behavior.

## Certificate-bound machine transport

Set server TLS certificate/key and REGENTE_TLS_CLIENT_CA. The HTTPS listener
verifies client certificates when supplied; browsers and probes do not require
one. Every machine route requires a valid client certificate AND its scoped bearer
when a client CA is configured. Proxy certificate headers are never identity.

Provision agent ID, exact environment/capabilities, expiry and certificateSHA256:
the lowercase SHA256 of the DER leaf certificate. Settings > Agents includes
issuance and a separate replacement fingerprint for rotation.

```bash
openssl x509 -in client.crt -outform DER | sha256sum
regente-agent -transport v2 -server https://regente.example \
  -id http-prod-01 -env prod -caps HTTP,REST \
  -journal /var/lib/regente-agent-http/journal.db \
  -token-file /etc/regente-agent-http/token \
  -tls-cert /etc/regente-agent-http/client.crt \
  -tls-key /etc/regente-agent-http/client.key \
  -tls-ca /etc/regente-agent-http/ca.crt \
  -execution-policy /etc/regente-agent-http/policy.json \
  -job-secrets-file /etc/regente-agent-http/secrets.json
```

Provision HTTP, REST and EXECUTION_V2: the CLI adds EXECUTION_V2 automatically.
The server reloads TLS files per handshake and revalidates the current CA,
certificate expiry and credential binding per request and before dispatch.
Open WS/SSE/poll clients revalidate every second. A failed DB/CA read denies access.

POST /api/agents/tokens/{id}/rotate takes expiry, graceSeconds (0–3600) and the
replacement certificateSHA256. Publish PEM files atomically and replace the token
file within the grace window. V2 reloads token/cert/key/server CA per request,
without old TLS keep-alive. A temporary file mismatch denies authentication and
retains the journal until files agree. WS/SSE certificate changes take effect on
reconnect; restart legacy agents for server CA/bearer rotation. Production uses V2.

DELETE /api/agents/tokens/{id} revokes that credential and binding; revoke every
credential deliberately issued with the same certificate if retiring its identity.
This is not an OCSP/CRL service. CA removal or certificate expiry also invalidate
open channels. Revocation does not prove an already started effect stopped:
drain/reconcile through the [durable runbook](durable-execution.md).

For nginx browser TLS termination, use a separate agent TLS origin or TCP
passthrough to the server. TLS terminated at a proxy and a forwarded header do not
satisfy this contract.

## Runtime secrets and local authorization

This adapter uses protected JSON files managed by the host operator. No Vault/AWS
manager or remote availability claim is included. Each execution reloads policy
and secrets: TTL=0; outage never serves stale values. In-flight executions retain
their resolved values. Files must be regular, not symlinks, at most 1 MiB, and mode
0600 on Linux; each resolved value must contain 8–4096 bytes. Windows development tests do not validate Unix permissions.

policy.json:

```json
{
  "environment": "prod",
  "jobs": {
    "submit-report": {
      "types": ["HTTP"],
      "secrets": ["reports.authorization"],
      "destinations": [
        {"host": "reports.example", "port": "443", "networks": ["192.0.2.10/32"]}
      ]
    }
  }
}
```

secrets.json (placeholder; never commit a real value):

```json
{"values": {"reports.authorization": "Bearer replace-this-outside-git"}}
```

The Git/YAML definition contains a reference:

```yaml
actionConfig:
  url: https://reports.example/submit
  method: POST
  headers:
    Authorization:
      secretRef: reports.authorization
```

Job ID/environment come from the frozen definition and must match local policy.
References need an explicit job allowlist. Invalid policy, unauthorized reference
or unavailable secret fails before external effects. Git, snapshots, outboxes and
journal envelopes retain references; only a copied actionConfig receives values
in memory after durable start authorization. Results are redacted before storage.

Secret references are supported only for HTTP/REST in this profile. COMMAND,
SCRIPT and SSH cannot resolve them. Do not mix untrusted commands with a secrets
cell. Literal credentials already pasted into definitions/history are not
retroactively scrubbed: migrate and rotate them. Redaction covers exact resolved
values; it cannot prevent a malicious endpoint encoding/extracting credentials.
Use trusted destinations. Control-plane PAT/OIDC/webhook consumers are separate
and may need restart to adopt new provider values.

## Egress and host isolation

Restricted HTTP requires exact host/port and approved CIDRs for every DNS answer.
Dialing uses the verified IP, without a second lookup. Unspecified, multicast and
link-local targets (including metadata 169.254.169.254) are always denied.
Egress CIDRs containing known metadata addresses are rejected by the installer.
Known metadata addresses 100.100.100.200 and fd00:ec2::254 are also denied.
Private/loopback targets require explicit authorization. Proxy environment
variables are ignored and redirects rejected. A redirect after an initial effect
may leave an unknown outcome when completion cannot be proven.

Internal control-plane HTTP/SSH can use the same execution-policy and
job-secrets-file flags. Restricted SSH requires strictHostKey=yes and an explicit
knownHostsPath, pins destination IP and disables local config/proxy/jump/agent
forwarding/local hooks. Remote commands still need a separate remote trust boundary.
Adapters without guarded dialing are denied by local policy: database, transfer,
cloud/K8S/WASM are not homologated in this profile.

Download install-secure-linux.sh and the agent binary from the same release.
Requires Linux/systemd with working cgroup limits and cgroup BPF IP filtering:

```bash
sudo SERVER=https://regente.example ID=http-prod-01 ENVIRONMENT=prod \
  TOKEN_FILE=/secure/token TLS_CERT=/secure/client.crt TLS_KEY=/secure/client.key \
  TLS_CA=/secure/server-ca.crt POLICY_FILE=/secure/policy.json \
  SECRETS_FILE=/secure/secrets.json \
  EGRESS_CIDRS='192.0.2.20/32 192.0.2.10/32' \
  bash install-secure-linux.sh
```

Allow DNS resolver IPs when using names: no implicit DNS exception exists.
The installer validates an HTTP/REST-only policy and creates a dedicated non-root
user, 0700 config/0600 files, dedicated writable journal state, no capabilities/new
privileges, private devices/tmp, protected system/home/kernel/cgroups, 128 tasks,
512 MiB RAM, no swap, one CPU quota and deny-all IPs with explicit CIDR exceptions.

Check systemctl show, logs, real allowed/denied connectivity and durable receipts
before admitting jobs. Settings alone do not prove kernel IP enforcement. A host
without working cgroup BPF is unsupported; require a verified namespace/firewall
boundary before claiming equivalence. Release smoke tests an independently
reachable forbidden target inside an isolated cgroup.

For trusted COMMAND/SCRIPT jobs, use the same installer with CELL_KIND=command,
a separate machine ID/certificate/token and a policy containing only COMMAND/SCRIPT
and no secrets. It creates regente-agent-command with a separate UID, protected
configuration and writable journal directory, and the same cgroup/egress controls.
The HTTP cell credentials/secrets directory is not readable by this UID.
The writable state volume is /var/lib/regente-agent-command; set job cwd and mount additional
volumes deliberately and review their ownership. Child processes
omit inherited Regente/cloud credentials, but use the agent OS identity and may
read its files. This is not a hostile-code sandbox. Use separate VMs/containers
and principals for mutually untrusted jobs; no host/Docker sockets/shared secrets.
The default HTTP cell denies command job types; the command cell denies HTTP and
job secret references. All commands sharing a cell belong to one trust domain.

## Upgrade and evidence

Schema 31 adds only the non-secret certificate fingerprint. Drain/upgrade all
nodes; supported range [31,31]. Unbound tokens work without an agent CA; provision
or rotate bindings before enabling it. Back up host policy/credentials separately:
DB backups contain references, not external secret values. Rollback requires the
matching previous DB backup and binary.

Mandatory SQLite/PostgreSQL tests use the real agent, hot TLS/token/secret
rotation, outage/authorization, canary redaction, egress and open-channel
invalidation. Installed systemd smoke adds non-root identity, cgroup limits,
actual denied host egress, separate command UID/volumes, absent inherited secrets
and revocation. See [verification](verification.md).

Metadata address references: [AWS IMDS](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-service.html) and [Alibaba Cloud metadata](https://www.alibabacloud.com/help/en/ecs/user-guide/view-instance-metadata).

## Verified laboratory

[Full CI 37012869940](https://github.com/Dr0nj/regente/actions/runs/37012869940) passed at e5bb974bbbc97b1d8790818992b15b6eb2760514 with all eight gates, 280 mandatory integration tests/subtests and eight browser scenarios without skips or flaky results. The [recorded evidence](evidence/i13-e5bb974.json) includes real SQLite/PostgreSQL agent execution and installed Ubuntu/systemd HTTP and COMMAND cells with actual denied egress. This qualifies the documented profile in a synthetic laboratory, not an installed production pilot.
