# Kubernetes laboratory — external agent

Later stages: [isolated HTTP cell](HTTP-CELL.md), [SSH bridge](SSH-BRIDGE.md),
and [gradual migration, rollback and origin retirement](MIGRATION.md).

This is the first Kubernetes deployment stage: one backend serving the SPA,
PostgreSQL on its own PVC, and a Linux agent **outside** Kubernetes. The external
Docker container simulates an execution host. It does not qualify EC2, systemd,
cloud storage, HA, or production capacity. No AWS account is needed.

The backend uses the production profile, local human authentication, durable v2
execution, and `control-plane-execution=deny`. Agent TLS terminates at Regente;
the machine credential is bound to its client certificate. The backend and agent
run as non-root with read-only root filesystems. Each has persistent state.

## Create an isolated lab

Run from the repository root. Prerequisites: Docker with Linux containers, kind,
kubectl, Python 3.10+ and the Python requirements below. The observed environment
used kind 0.32.0 and Kubernetes 1.36.1. Reserve localhost port 8443. Do not reuse an
existing cluster or namespace containing someone else's data.

```sh
python -m pip install -r deploy/kubernetes/requirements.txt
docker build -f deploy/kubernetes/Dockerfile.server --build-arg VERSION=kubernetes-lab -t regente-kubernetes-server:9311778 .
docker build -f deploy/kubernetes/Dockerfile.agent --build-arg VERSION=kubernetes-lab -t regente-kubernetes-agent:9311778 agent
kind create cluster --config deploy/kubernetes/kind.yaml --image kindest/node:v1.36.1
kind load docker-image regente-kubernetes-server:9311778 regente-kubernetes-agent:9311778 --name regente-kubernetes
python deploy/kubernetes/lab.py prepare
python deploy/kubernetes/lab.py deploy
kubectl --context kind-regente-kubernetes -n regente-kubernetes-lab rollout status deployment/regente-server --timeout=120s
python deploy/kubernetes/lab.py agent
python deploy/kubernetes/lab.py finalize
```

The local image tags are fixed aliases referenced by `lab.py`; they identify the
initial laboratory lineage, not an assertion that later builds have that Git SHA.
Record the checked-out commit and resulting image IDs when reproducing a run.
The base images are pinned by digest. `render` prints manifests without secrets.

Wait for HTTPS `/readyz` as well as deployment readiness before using the API:
the NodePort can converge shortly after the pod. `finalize` first verifies login,
then removes the bootstrap password from the server environment. It remains in
the protected local state and Kubernetes Secret for this disposable laboratory.

Open **https://localhost:8443**. The account is `admin`; its generated password is
in `.integration/kubernetes/private.json`. The local CA is
`.integration/kubernetes/ca.crt`. Configure trust deliberately for your test client;
do not disable TLS verification or install a global trust exception. The automated
checks trust only that CA. Browser trust/UI interaction is a separate check.

All credentials, evidence and backups stay in the Git-ignored
`.integration/kubernetes/` directory. Protect that directory with the host's user
ACLs; POSIX chmod alone does not implement Windows ACLs. Do not upload its contents
as CI artifacts. The client/server certificates last seven days, and the agent
token lasts one day. This bootstrap does not implement certificate renewal. Its
CA private key is discarded; use a new isolated lab for another certificate cycle.

## Short validation and recovery

```sh
kubectl --context kind-regente-kubernetes -n regente-kubernetes-lab exec deployment/regente-server -- /usr/local/bin/regente-server -check-config
python deploy/kubernetes/verify.py
python deploy/kubernetes/recover.py
```

`verify.py` runs once on a fresh lab: ten successful commands, a known failure and
explicit retry, cancellation, backend replacement, and abrupt agent loss. Effects
are counted in the external agent's journal volume. It deliberately leaves the
last abrupt-loss execution UNCERTAIN: inspect its effects and stopped processes,
then resolve it through the audited execution-resolution API before new work.
Do not clear the journal or repeat the test blindly. An existing report prevents
accidental repeat execution. A graceful restart can produce a known failure;
SIGKILL is required for the intended crash scenario.

`recover.py` requires drained executions. It stops the source backend and agent,
exports PostgreSQL, workspace/audit key and the stopped journal, then resumes the
source. It restores into `regente-kubernetes-lab-restore` with separate PVCs and no
agent. It checks HTTPS/login and instance count through loopback port 8843, then
scales the restored backend to zero. Backup files include secrets. A pre-existing
backup directory prevents an accidental repeat. Never attach the original agent
to both databases or replay restored effects without reconciliation.

For an image update, drain work and take a backup first. Load the new image with
`kind load docker-image`, use `kubectl set image`, then verify the deployment's
`status.observedGeneration` matches `metadata.generation`, its new pod is ready,
and HTTPS/login/history work. To roll back a **compatible, same-schema** image,
set the original image explicitly and repeat those checks. Image rollback does
not reverse a database migration. A cross-schema recovery needs a separately
qualified binary/database restore procedure.

## Observed qualification — 21 October 2026

Base runtime: v0.2.50, schema 32, Linux amd64. Ten commands produced ten unique
effects; dispatch from durable order creation to start had p95 **1.483 s**, maximum
**1.492 s**. Backend replacement reconciled the same execution without repetition.
An abrupt agent stop preserved uncertainty without repeating the effect; an
operator resolved it after checking the process had stopped. Isolated restore
recovered **15 instances in 49.665 s**. A same-schema image update and rollback
preserved all 15 instances. PostgreSQL unavailability produced readiness HTTP 503
and recovered. Missing machine certificates, revoked tokens, and machine access
to credential administration were rejected; idle credential rotation accepted the
replacement and rejected the old token.

Qualification was completed on 22 October in a fresh namespace, with separate
PostgreSQL/workspace PVCs, PKI, agent identity and journal. The corrected full
`verify.py` run passed. Chromium 153 verified HTTPS login without TLS bypass,
viewer/operator/admin read/write/admin permissions, scoped reads and automatic
WebSocket reconnection after ACL change. Token rotation with zero grace during a
running command rejected the old token, completed the same execution once and did
not restart the agent. Making the stopped agent's journal unwritable caused a
nonzero startup refusal; its queued job ran once after restoring permissions.
This is an unwritable-volume test, not a host disk exhaustion benchmark.

The original baseline report and an initial readiness failure (before any job)
remain preserved as historical evidence. The fresh run supplies complete harness
evidence; no failed report was relabeled. No 30-minute soak was run. These local
observations do not qualify AWS, HA, production capacity or cross-schema rollback.

## Repeat qualification in a fresh namespace

Keep the original lab. Set `REGENTE_LAB_INSTANCE=regente-kubernetes-qual01`,
`REGENTE_LAB_NODE_PORT=30444` and `REGENTE_LAB_URL=https://127.0.0.1:9443` for every
Python command. The instance name determines the isolated namespace, local state,
external agent and volumes. Prepare/deploy/agent/finalize as above. Do not reuse a
name whose state or evidence already exists. The API refuses a different CA in an
existing namespace. A new namespace still shares the local cluster and Docker
host; this is not tenant isolation.

Provide a stable loopback TCP mapping from port 9443 to the control-plane node's
30444, preserving TLS end to end. A `kubectl port-forward` to a pod or Service exits
when its selected backend pod is replaced, so it cannot carry the backend-loss
rehearsal without supervision. The observed run used a disposable Node TCP proxy:

```sh
docker run -d --name regente-kubernetes-qual01-proxy --network kind --read-only --cap-drop ALL --security-opt no-new-privileges --memory 128m --cpus 0.25 -p 127.0.0.1:9443:9443 regente-kubernetes-web-check node -e 'const net=require("node:net");net.createServer(s=>{const t=net.connect(30444,"regente-kubernetes-control-plane");s.pipe(t).pipe(s);s.on("error",()=>t.destroy());t.on("error",()=>s.destroy());}).listen(9443,"0.0.0.0")'
python deploy/kubernetes/verify.py
python deploy/kubernetes/qualify.py
python deploy/kubernetes/recover.py
```

Build the `web` target as `regente-kubernetes-web-check` before that proxy.
`qualify.py` inspects and resolves the synthetic crash, rotates the actual agent's
token during a command, then tests refusal/recovery of an unwritable journal. It
preserves evidence, refuses a second run and restores journal permissions in a
finally block. On failure, inspect the report and durable state before retrying.
The one-day replacement token is installed atomically; credentials are never
printed. Keep `token.next` and `credential-next-id.json` if installation fails.

For the browser and role matrix, build `Dockerfile.browser` after the `web` target.
Run `browser-check.cjs` in that disposable container on Docker's `kind` network,
with `NODE_PATH=/app/node_modules`, `LAB_STATE=/state`,
`LAB_BROWSER_URL=https://regente-kubernetes.local:30444`, the isolated state mounted
at `/state` and the script mounted read-only. Map that hostname to the discovered
control-plane IP. Before running Node, initialize the container's NSS database and
import only the laboratory CA:

```sh
mkdir -p /root/.local/share/pki/nssdb
certutil -N --empty-password -d sql:/root/.local/share/pki/nssdb
certutil -A -d sql:/root/.local/share/pki/nssdb -n regente-lab-ca -t 'C,,' -i /state/ca.crt
node /check.cjs
```

Use `--shm-size 256m` for the browser container. The trust database lives only in
that disposable container; see [Chromium certificate management](https://chromium.googlesource.com/chromium/src/+/main/docs/linux/cert_management.md).
The script creates synthetic viewer/operator accounts, records its JSON report
and screenshot privately, and verifies that event URLs use tickets rather than
bearer tokens. `browser-accounts.json` contains credentials and must remain ignored.


## Operational boundaries

- One backend and one external agent identity; do not scale either identity by
  copying its journal. PVCs use kind's `standard` local-path provisioner. They
  survive pod replacement, not node/cluster deletion.
- PostgreSQL uses an isolated lab network without database TLS. No production
  ingress, NetworkPolicy enforcement, cloud secret provider or HA is supplied.
- The external agent resolves `regente-kubernetes.local` through a discovered node
  IP in Docker `ExtraHosts`. After restarting Docker, check that mapping against
  the control-plane node IP before resuming jobs. Preserve its credential and
  journal volumes during any container recreation; never issue a second identity
  just to repair routing.
- To resume after Docker restart, wait for both Kubernetes nodes and the backend,
  then `docker start regente-kubernetes-external-agent`. Do not assume process
  startup means ports and Services are ready.
- On Windows, Docker Desktop can fail on an inaccessible stale Unix socket under
  `%LOCALAPPDATA%/Docker/run`. The [upstream report](https://github.com/docker/desktop-feedback/issues/554)
  documents stopping Docker, preserving/renaming the transient `run` and
  `docker-secrets-engine` directories, and starting Docker again. This recovered
  the observed lab without resetting volumes. Inspect paths first; do not factory
  reset Docker or unregister WSL to repair a transient socket.
- Cleanup is intentionally manual: inventory and back up this lab's namespaces,
  cluster, external container and two named volumes before deleting anything.
  Never use broad Docker prune commands. Existing clusters are unrelated.

The [HTTP cell extension](HTTP-CELL.md) adds an in-cluster HTTP agent with its own
identity, persistent journal and verified egress, alongside the external agent.
Future stages are tracked in the [canonical roadmap](../../docs/roadmap.md):
remote execution bridge, migration and per-execution pods.

The [SSH bridge extension](SSH-BRIDGE.md) runs SSH_REMOTE in a dedicated pod against a Linux host without a Regente agent.
