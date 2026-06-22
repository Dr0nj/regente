# Deploy — serverless portátil

Artefatos para rodar o `regente-server` como **container serverless scale-to-zero
sem lock-in de fornecedor**. A estratégia completa está em
[`../docs/arquitetura-futuro.md`](../docs/arquitetura-futuro.md).

## Ideia em uma linha

Empacote como **imagem OCI** (padrão aberto), externalize o **estado** (Postgres
por wire protocol) e o **gatilho de tempo** (cron externo → `POST /api/scheduler/tick`).
A mesma imagem roda em Knative, Cloud Run, Fly, App Runner ou Container Apps —
você escolhe a nuvem no deploy, não no código.

## Arquivos

| Arquivo | Para quê |
|---|---|
| `Dockerfile` | Build multi-stage do server → imagem **distroless** (sem shell/pacotes/`git`). |
| `.dockerignore` | Contexto de build enxuto. |
| `knative-service.yaml` | Service Knative com `minScale: 0` (scale-to-zero) e `-scheduler=external`. |
| `cronjob.yaml` | Gatilho de tempo externo (k8s CronJob batendo no `/tick`). |

> **Runtime sem `git`.** A config é externalizada no Git, mas a camada de storage
> (`internal/storage/git.go`) usa **go-git** (Go puro) — não há `exec git`. Por
> isso a imagem final é `gcr.io/distroless/static-debian12:nonroot`: estática,
> non-root (uid 65532), só com `ca-certificates` (clone HTTPS). Sem shell, sem
> gerenciador de pacotes, sem `git` no PATH — superfície de ataque mínima.

## Build

```bash
# a partir da raiz do repo
docker build -f deploy/Dockerfile -t ghcr.io/dr0nj/regente-server:latest .
docker push ghcr.io/dr0nj/regente-server:latest
```

## Rodar (Knative / qualquer k8s)

```bash
kubectl create secret generic regente-secrets \
  --from-literal=REGENTE_DB='postgres://user:pw@host:5432/regente' \
  --from-literal=REGENTE_TOKEN='<token-forte>'

kubectl apply -f deploy/knative-service.yaml
kubectl apply -f deploy/cronjob.yaml
```

## Equivalências sem lock-in

| Peça | Aberto / portátil | Gerenciado (troca só o manifesto) |
|---|---|---|
| Runtime | Knative (CNCF) | Cloud Run · App Runner · Container Apps · Fly |
| Estado | Postgres (wire protocol) | Neon · Supabase · Cloud SQL · RDS |
| Gatilho | k8s CronJob | Cloud Scheduler · EventBridge · GitHub Actions cron |
| Barramento de agentes (Fase 2) | NATS (CNCF) / SSE | NATS gerenciado |
| Logs/artefatos | API S3-compatível | R2 · MinIO · S3 · GCS |

> Modo clássico ainda disponível: sem `-scheduler=external` o server roda o
> ticker interno (daemon de sempre). Os dois modos compartilham o mesmo binário.

## Validação local em container (2026-06-18)

Caminho serverless exercitado ponta-a-ponta com Docker (imagem desta pasta +
Postgres 16 em container, mesma rede):

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

Resultado: boot OK (clone do workspace **dentro do container**), `/health`→200,
leader election via advisory lock, e `POST /api/scheduler/tick` (gatilho externo)
materializou a daily — `5 instances created`, persistidas no Postgres — de forma
**idempotente** (ticks repetidos não duplicam).

**Re-validado em imagem distroless (2026-06-18):** após migrar a storage para
go-git, a mesma sequência roda em `gcr.io/distroless/static-debian12:nonroot`
(**sem `git` no PATH**, sem shell). Boot clonou o workspace via go-git
(`main@923f091`), `/health`→200, e o tick rodou o fetch+reset antes da daily
(`synced workspace to 923f091` → `5 instances created`), idempotente. Imagem
**non-root**, ~33 MB (era ~55 MB com Alpine+git).
