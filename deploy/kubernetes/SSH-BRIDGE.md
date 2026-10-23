# Kubernetes-03: remote SSH bridge

The local kind laboratory runs `SSH_REMOTE` on a dedicated v2 bridge pod. The
external Linux container has sshd and pre-provisioned scripts, without a Regente
agent. Existing `SSH` jobs still execute server-side; production control-plane
execution remains denied. COMMAND agents and HTTP cells can coexist independently.

## Contract and security

Select the bridge through `agentId` and capability `SSH_REMOTE`. `host` is the
remote destination, not the bridge. Required actionConfig fields are `host`,
`user`, `command`, `keyPath`, `knownHostsPath` and `strictHostKey: "yes"`; `port`
defaults to 22. Paths refer to protected files on the bridge. The native Go SSH
client uses key authentication and mandatory known_hosts verification; it does
not load SSH config, proxy commands, local shells or agent forwarding.

Local execution policy must authorize the exact job/environment/type, destination
host/port/CIDR, SSH user, command and both credential paths. Example job policy:

```json
{
  "types": ["SSH_REMOTE"],
  "destinations": [{"host": "ssh-target", "port": "2222", "networks": ["192.0.2.10/32"]}],
  "ssh": {
    "user": "runner", "commands": ["sh /jobs/success.sh"],
    "keyPath": "/private/ssh_key", "knownHostsPath": "/private/known_hosts"
  }
}
```

Use the actual destination CIDR, never the documentation address above. DNS answers
are checked and the connection pins an allowed address; metadata targets are denied.
Key and known_hosts files must be regular protected files (0600 on Linux), not
projected Secret symlinks. Init copies them to private memory storage. The bridge
inherits the [HTTP cell](HTTP-CELL.md) single-owner journal, read-only/non-root
runtime, capability restrictions and fail-closed egress firewall. Its allowed
remote port is 2222. Do not scale the identity, force-delete it or reuse its journal
concurrently. Node loss, HA and hostile-code isolation are not qualified.

## Completion, cancellation and recovery

stdout/stderr are streamed into bounded durable output. Confirmed remote exit 0
succeeds; confirmed exit 7 fails normally. Authentication, host-key and policy
failures before submission cannot produce a remote job effect. After submission,
lost acknowledgement/connection, timeout or cancellation without a terminal
receipt preserves UNCERTAIN. Closing the SSH client does **not** prove that the
remote process stopped. Automatic retries remain suppressed while uncertain.
Inspect the destination independently, establish completion/effects and fence any
remaining process before audited resolution or a deliberate new execution.
No exactly-once, remote kill, file-transfer or lost-output recovery is promised.
The new remote-unknown journal result preserves this rule even after cancellation;
legacy executor cancellation behavior is unchanged.

## Reproduce

Use [baseline prerequisites](README.md), an existing two-node kind lab and a fresh
instance name/state. Build and load current sources (image aliases are not SHAs):

```powershell
$env:REGENTE_LAB_INSTANCE='regente-kubernetes-phase03'
$env:REGENTE_LAB_NODE_PORT='30446'
$env:REGENTE_LAB_URL='https://127.0.0.1:11443'
docker build -t regente-kubernetes-server:phase03 -f deploy/kubernetes/Dockerfile.server .
docker build -t regente-kubernetes-agent:phase03 -f deploy/kubernetes/Dockerfile.agent agent
docker build -t regente-kubernetes-ssh-target:phase03 -f deploy/kubernetes/Dockerfile.ssh-target deploy/kubernetes
kind load docker-image regente-kubernetes-server:phase03 regente-kubernetes-agent:phase03 regente-kubernetes-firewall:phase02 --name regente-kubernetes
python deploy/kubernetes/ssh_bridge.py prepare
```

Build the firewall image from HTTP-CELL.md if absent. Establish the loopback TCP
proxy to the chosen NodePort, preserving TLS, and wait for backend readiness. Then:

```powershell
python deploy/kubernetes/ssh_bridge.py external-agent
python deploy/kubernetes/ssh_bridge.py deploy
python deploy/kubernetes/ssh_bridge.py finalize
python deploy/kubernetes/verify_ssh_bridge.py
```

Reports and credentials stay ignored under `.integration/<instance>`. Never reset
journals or erase failed reports to repeat a test. The short qualification checks
success/output/exit7, coexistence, policy denials, real egress denial, revoked keys,
wrong host keys, cancellation, deadline and runtime-level SIGKILL. Each interruption
produced one effect, no replay despite configured retries, and audited resolution
only after an independent remote completion marker. No 30-minute local soak was
added; runtime changes still select the required capacity gate in CI.

Optional real editor qualification after jobs finish: build `Dockerfile.git-fixture`
as `regente-kubernetes-git-fixture:phase03`, then run `ssh_design.py`. It copies
synthetic definitions into a separate seed and serves them read-only over git://
on the private Docker lab network. It enables a new Git workspace and `/data`
working directory for durable drafts; original workspace remains preserved.
This unauthenticated fixture is lab-only, contains no secrets and has no published
host port or GitHub connection. go-git file transport needs git-upload-pack, absent
from the distroless image; the separate daemon avoids adding git to the backend.
Run `ssh-browser.cjs` in the disposable Chromium image described in README, trusting
only the lab CA, with LAB_STATE and LAB_BROWSER_URL. It verifies type selection,
bridge targeting, trust fields and draft save. It does not publish the draft.

Rotate keys by draining the bridge, updating the destination authorized_keys and
source Secret/known_hosts, then normally replacing the pod. Refresh hostAliases,
policy and firewall together if the destination IP changes. Keep token/certificate
expiry visible (lab token one day, certificates seven days). Production credential
rotation, fleet migration, SSM/AWS and per-execution pods remain later work.

Sources: [Go SSH session receipts](https://pkg.go.dev/golang.org/x/crypto/ssh#Session.Wait),
[known_hosts validation](https://pkg.go.dev/golang.org/x/crypto/ssh/knownhosts#New).
[Sanitized qualification evidence](../../docs/evidence/kubernetes-03-local.json).
