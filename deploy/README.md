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
| `Dockerfile` | Build multi-stage do server → imagem distroless mínima. |
| `.dockerignore` | Contexto de build enxuto. |
| `knative-service.yaml` | Service Knative com `minScale: 0` (scale-to-zero) e `-scheduler=external`. |
| `cronjob.yaml` | Gatilho de tempo externo (k8s CronJob batendo no `/tick`). |

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
