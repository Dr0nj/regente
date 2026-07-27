# Deploy — portable serverless

Artifacts for running `regente-server` as a **scale-to-zero serverless container without vendor
lock-in**. The full strategy lives in
[`../docs/architecture-future.md`](../docs/architecture-future.md).

> Looking for the **normal install** (a VPS or your own machine, supervised by systemd)? That is
> in the [root README](../README.md#-installation) — it is the recommended path. This folder is
> for the serverless/container route.

## The idea in one line

Package it as an **OCI image** (an open standard), externalize the **state** (Postgres over the
wire protocol) and the **time trigger** (an external cron → `POST /api/scheduler/tick`). The same
image runs on Knative, Cloud Run, Fly, App Runner or Container Apps — you pick the cloud at
deployment time, not in the code.

## Files

| File | What it is for |
|---|---|
| `Dockerfile` | Multi-stage build of the server → a **distroless** image (no shell, no packages, no `git`). |
| `.dockerignore` | Keeps the build context lean. |
| `knative-service.yaml` | A Knative Service with `minScale: 0` (scale to zero) and `-scheduler=external`. |
| `cronjob.yaml` | The external time trigger (a k8s CronJob hitting `/tick`). |

> **A runtime without `git`.** Configuration is externalized in Git, but the storage layer
> (`internal/storage/git.go`) uses **go-git** (pure Go) — there is no `exec git`. That is why the
> final image is `gcr.io/distroless/static-debian12:nonroot`: static, non-root (uid 65532), with
> only `ca-certificates` (for HTTPS clones). No shell, no package manager, no `git` on the PATH —
> a minimal attack surface.

## Build

```bash
# from the repository root
docker build -f deploy/Dockerfile -t ghcr.io/dr0nj/regente-server:latest .
docker push ghcr.io/dr0nj/regente-server:latest
```

## Run (Knative / any k8s)

```bash
kubectl create secret generic regente-secrets \
  --from-literal=REGENTE_DB='postgres://user:pw@host:5432/regente' \
  --from-literal=REGENTE_TOKEN='<a-strong-token>'

kubectl apply -f deploy/knative-service.yaml
kubectl apply -f deploy/cronjob.yaml
```

## Lock-in-free equivalents

| Piece | Open / portable | Managed (only the manifest changes) |
|---|---|---|
| Runtime | Knative (CNCF) | Cloud Run · App Runner · Container Apps · Fly |
| State | Postgres (wire protocol) | Neon · Supabase · Cloud SQL · RDS |
| Trigger | k8s CronJob | Cloud Scheduler · EventBridge · GitHub Actions cron |
| Agent bus | NATS (CNCF) / SSE | managed NATS |
| Logs/artifacts | S3-compatible API | R2 · MinIO · S3 · GCS |

> The classic mode is still available: without `-scheduler=external` the server runs its internal
> ticker (the usual daemon). Both modes share the same binary.

## Local container validation (2026-06-18)

The serverless path was exercised end to end with Docker (the image from this folder plus
Postgres 16 in a container, on the same network):

```bash
docker network create regente-net
docker run -d --name regente-pg --network regente-net \
  -e POSTGRES_USER=regente -e POSTGRES_PASSWORD=regente -e POSTGRES_DB=regente_test \
  postgres:16-alpine
docker build -f deploy/Dockerfile -t regente-server:local .
docker run -d --name regente-app --network regente-net -p 8090:8080 regente-server:local \
  -db-driver postgres -db 'postgres://regente:regente@regente-pg:5432/regente_test?sslmode=disable' \
  -addr :8080 -scheduler=external
```

Result: clean boot (it cloned the workspace **inside the container**), `/health`→200, leader
election through the advisory lock, and `POST /api/scheduler/tick` (the external trigger)
materialized the daily — `5 instances created`, persisted in Postgres — **idempotently** (repeated
ticks do not duplicate).

**Re-validated on the distroless image (2026-06-18):** after migrating storage to go-git, the same
sequence runs on `gcr.io/distroless/static-debian12:nonroot` (**with no `git` on the PATH**, no
shell). Boot cloned the workspace through go-git (`main@923f091`), `/health`→200, and the tick ran
fetch+reset before the daily (`synced workspace to 923f091` → `5 instances created`),
idempotently. A **non-root** image, ~33 MB (down from ~55 MB with Alpine+git).
