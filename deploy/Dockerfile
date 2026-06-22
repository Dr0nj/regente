# syntax=docker/dockerfile:1
# Build do regente-server como imagem OCI portátil (roda em qualquer runtime de
# container: Knative, Cloud Run, Fly Machines, App Runner, Container Apps, ECS…).
# Build a partir da raiz do repo:  docker build -f deploy/Dockerfile -t regente-server .

FROM golang:1.25 AS build
WORKDIR /src
# Cache de deps: copia só o go.mod/go.sum do módulo server primeiro.
COPY server/go.mod server/go.sum ./server/
RUN cd server && go mod download
COPY server/ ./server/
RUN cd server && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/regente-server .
# Diretório de trabalho do server (workspace GitOps + DB SQLite quando usada),
# pré-criado para ser COPYado com a dona correta na imagem distroless (que não
# tem shell para `mkdir`/`chown`). 65532 = uid/gid do usuário `nonroot`.
RUN mkdir -p /data && chown 65532:65532 /data

# Imagem final distroless: SEM shell, SEM gerenciador de pacotes e — agora que a
# camada de storage usa go-git (Go puro, ver internal/storage/git.go) — SEM
# necessidade do binário `git` no PATH. Inclui ca-certificates (clone HTTPS) e o
# usuário não-root `nonroot` (uid 65532). Binário estático (CGO_ENABLED=0).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/regente-server /usr/local/bin/regente-server
COPY --from=build --chown=65532:65532 /data /data
WORKDIR /data
# Porta default do server; o runtime serverless costuma injetar $PORT — o
# entrypoint respeita -addr; veja knative-service.yaml para o mapeamento.
EXPOSE 8080
USER nonroot
ENTRYPOINT ["regente-server"]
