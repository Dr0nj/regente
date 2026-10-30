# Kubernetes Job execution cell

Kubernetes-05 adds a durable `K8S` / `K8S_JOB` executor to the v2 agent. Qualification is local to the dedicated kind cluster; this is not an AWS, HA or production fleet certification.

## Contract and trust boundary

Advertise the **exact** job type in both the machine credential and `-caps`. Schema aliases do not imply scheduler capability aliases. Principal scope is immutable. The laboratory uses `K8S` throughout.

Only `image`, `command` and `namespace` are accepted in a durable job. All three must match the local execution policy. Images require a sha256 digest, and commands are exact allowlist entries. The existing legacy schema remains readable, but v2 rejects job-supplied `token`, `apiServer`, `name`, `insecureTLS`, mounts and arbitrary manifests. A missing policy never enables unrestricted Kubernetes execution.

The policy's `kubernetes` object supplies `apiServer` (HTTPS), absolute `caFile` and `tokenFile`, `namespace`, `serviceAccount`, `images`, `commands`, `firewallImage` by digest, `cpu` in millicores, `memory` in MiB, `maxSeconds` and `retentionSeconds` (3600–518400). An optional trusted `nodeSelector` constrains placement. Its normal destination allowlist must permit the API hostname, port and IP range. The projected bearer token is read for every request; do not copy it into an immutable private-file snapshot. Neither token nor API response bodies are emitted in errors.

The controller has namespace-scoped Job create/get/delete, pod get/list/patch and pod log read access. It cannot read Secrets, exec into pods or create service-account tokens through this Role. RBAC does not constrain individual Job fields: the controller, its policy, approved images, admission chain and namespace administrators are trusted. A compromised controller with Job-create permission is outside this boundary. Use a dedicated namespace and quota.

The application container runs as UID/GID 10001, with no capabilities, no privilege escalation, read-only root, RuntimeDefault seccomp and a 16 MiB tmpfs at `/tmp`. It receives neither the controller credential nor a workload ServiceAccount token. CPU/memory requests equal limits. The approved firewall init runs as root with NET_ADMIN/NET_RAW to deny egress except loopback, including IPv6. This init requires an explicit security exception: the pod is **not** compliant with Pod Security Restricted. A CNI admission/enforcement design for production remains separate; the local firewall is tested at runtime.

## Durable state and recovery

Before the only create request, the agent writes a binding into its SQLite journal. The name is derived from execution ID and fence; the binding freezes the policy/definition fingerprint, Job UID, first observed pod UID and observation deadline. A lost create response is reconciled with GET. A missing Job after intent, changed identity, multiple pods or missing process receipt yields `UNCERTAIN`; none authorizes another POST.

After controller restart, observation resumes only for a journal entry interrupted by that restart, with started ACK, an existing binding and a server receipt still authorizing the same current execution. Explicit uncertainty never resumes automatically. The existing COMMAND/SSH behavior is preserved. A shutdown can defer external observation without inventing a process result.

Jobs use parallelism/completions 1, backoffLimit 0, restartPolicy Never and podReplacementPolicy Failed. These settings do **not** guarantee exactly-once execution: Kubernetes may create replacement pods. The adapter detects observed duplication and refuses to claim an unambiguous result. External business operations still require idempotency or audited reconciliation.

The pod deadline equals the configured timeout; the outer Job deadline is 30 seconds later and observation stops after timeout + 60 seconds. Pod deadlines include initialization. Using only a Job deadline can delete the pod before logs/receipt collection; the original failed experiment is retained in qualification evidence. Cancellation reduces the observed pod deadline with a UID precondition and waits for the same pod's main-container termination receipt. A lost node or missing receipt remains uncertain. Cancellation does not undo effects. After start, Regente displays NOTOK with an attempt state of `cancelled`; before start it displays CANCELLED.

Completion requires a terminal Job, exactly one matching terminal pod and main-container termination with restartCount 0. Up to 1 MiB of combined stdout/stderr is fetched at completion; this implementation does not stream application logs while the pod is running. An unavailable log response is uncertain. NodeLost/Failed phase alone is not proof of stopped execution.

## Retention

No TTL controller deletes evidence before Regente acknowledges it. The hourly agent cleanup considers only completed bindings whose journal result has a durable ACK older than the policy retention. It validates the original identity and deletes the Job with a UID precondition; absence is confirmed on a later pass. The binding remains as a tombstone. Uncertain results are not automatically cleaned. This is not an archival log store: an operator, Job deadline or cluster garbage collector can still remove pods. Export evidence before administrative cleanup. If policy/credentials are unavailable or the retained definition has been compacted after a long outage, cleanup conservatively leaves objects for inspection.

## Local qualification

Use `job_cell.py` with `REGENTE_LAB_INSTANCE=regente-kubernetes-phase05`, `REGENTE_LAB_NODE_PORT=30448` and `REGENTE_LAB_URL=https://127.0.0.1:13443`. `cluster`, `prepare`, `deploy` and `finalize` are one-time provisioning steps, not recovery commands. Build the agent image first, using `Dockerfile.agent`, and retain the prior phase03 server and phase02 firewall images. The pinned kind configuration has separate controller and workload nodes, so worker fault tests do not touch previous labs.

`verify_job_cell.py` records order IDs before observation and refuses to blindly repeat fault sequences. Inspect `.integration/regente-kubernetes-phase05/job-cell-verification.json` before continuing an interrupted run. The private directory contains credentials: never publish it. Wait for HTTPS readiness after bootstrap or Docker restart. Do not force-delete uncertain pods or recreate the cluster to obtain a green report.

Sources: [Job semantics](https://kubernetes.io/docs/concepts/workloads/controllers/job/), [pod lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/), [ServiceAccounts](https://kubernetes.io/docs/concepts/security/service-accounts/), [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/).
