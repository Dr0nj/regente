# Kubernetes-02: HTTP cell and external-agent coexistence

This is a qualified **local kind laboratory**, using the existing HTTP/REST
executor. It does not add SSH/SSM, cloud infrastructure, HA or per-execution pods.
HTTP targets Services inside Kubernetes; COMMAND targets the external Docker
agent. Each uses its own machine ID, certificate, scoped token and durable journal.

## Workload and isolation

`http_cell.py` creates a one-replica StatefulSet with `OnDelete` updates and a
local-path PVC for SQLite WAL/fsync. The init container refuses ordinals other
than zero. Do not autoscale, force-delete the pod or duplicate its identity.
Drain and reconcile before ordinary deletion/upgrade; wait for kubelet-confirmed
termination before replacement. The local volume survives pod replacement on the
same node; node loss requires recovery, not an automatic second owner.

The agent runs as UID/GID10001, with read-only root filesystem, all capabilities
dropped, `RuntimeDefault` seccomp, CPU/memory limits and no ServiceAccount token.
There are no host mounts or Docker sockets. Journal storage is persistent;
temporary files and private credentials use bounded memory volumes. Kubernetes
Secrets are copied by init into regular0600 files because the existing security
reader deliberately refuses projected symlinks. The runtime credentials mount
is read-only. Rotate job secrets by draining, updating the source Secret, waiting
for the destination to adopt its value, and normally replacing the pod. This is
not a hot secret-reload guarantee.

The existing kindnet installation has no verified NetworkPolicy enforcement.
An init container therefore installs an OUTPUT firewall in the pod network
namespace, before starting the agent: only cluster DNS53, backend Service8443
and fixture Service8443 are allowed; other IPv4 egress is rejected and IPv6
egress dropped. Init has NET_ADMIN/NET_RAW; the long-running agent has neither.
Init failure prevents startup. Service IP changes require a drained manifest
update; do not reuse stale allowlists. Policy adds exact job/environment/type,
host/port/CIDR authorization and secret-reference allowlists. Job TLS uses the
laboratory CA via `SSL_CERT_FILE`; machine transport separately uses mTLS.

This profile is not identical to the systemd installer: per-pod PID limits,
cluster admission policy and node security remain operator responsibilities.
It does not claim to sandbox hostile jobs. Only HTTP/REST capabilities are issued;
commands and arbitrary adapters must stay in a separate cell.

## Reproduce in a new instance

Use the prerequisites and image build instructions in [README](README.md).
Build the web stage as `regente-kubernetes-web-check:latest` for the synthetic
Node TLS fixture; no application dependencies execute in that fixture.

```powershell
$env:REGENTE_LAB_INSTANCE='regente-kubernetes-phase02'
$env:REGENTE_LAB_NODE_PORT='30445'
$env:REGENTE_LAB_URL='https://127.0.0.1:10443'
docker build -t regente-kubernetes-firewall:phase02 -f deploy/kubernetes/Dockerfile.firewall deploy/kubernetes
kind load docker-image regente-kubernetes-firewall:phase02 regente-kubernetes-web-check:latest regente-kubernetes-agent:9311778 --name regente-kubernetes
python deploy/kubernetes/http_cell.py prepare
```

Expose the chosen NodePort with a loopback-only TCP proxy or port-forward and
wait for `/readyz`. Discover the node address from Kubernetes instead of copying
an earlier Docker address. Keep TLS end-to-end. Then:

```powershell
python deploy/kubernetes/lab.py agent
python deploy/kubernetes/http_cell.py deploy
python deploy/kubernetes/lab.py finalize
python deploy/kubernetes/verify_http_cell.py
```

`prepare` refuses an existing state directory; `deploy` refuses another identity
issuance; verification refuses overwriting an existing report. Inspect failures
and durable effects before resuming. Never delete a journal to obtain a clean test.
Private `.integration/<instance>/` contains real synthetic credentials and must
remain ignored. Token lifetime is one day; certificates seven days. No cloud
credential provider, certificate-renewal controller or production SLA is supplied.

The short qualification verifies real TLS Service DNS, explicit routing alongside
external COMMAND, redaction, host/port/CIDR/metadata denials, a live forbidden
destination accessible from another pod but unreachable directly from the agent,
drained secret rotation and same journal inode after replacement. For interruption,
the fixture fsyncs an independent effect record, the node runtime sends SIGKILL to that exact agent container, and ordinary
kubelet-confirmed deletion removes the pod. The
replacement must preserve the execution as uncertain without replay. Resolution
is audited only after the fixture confirms completion and the old pod is gone.

Sources: [init containers share the pod network](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/),
[StatefulSet storage and identity](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/),
[force deletion breaks at-most-one assumptions](https://kubernetes.io/docs/tasks/run-application/force-delete-stateful-set-pod/).

The first interruption rehearsal used an in-pod STOP signal, which did not
enforce an abrupt crash; graceful termination returned failed. That failed harness
report is retained privately. The corrected runtime-level crash was tested as a
separate, identified order; prior approved checks were not rerun or erased.
