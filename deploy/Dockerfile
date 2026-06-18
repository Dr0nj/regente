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

# Imagem final mínima. NÃO usar distroless/static aqui: o GitOps externaliza a
# config no Git e a camada de storage (`internal/storage/git.go`) faz `exec git`
# (clone/fetch/reset/commit) — sem o binário `git` no PATH o boot dá log.Fatal
# em EnsureClone. Alpine + git + ca-certificates é o mínimo que faz o caminho
# serverless documentado (Knative/Cloud Run/Fly) realmente subir. Roda non-root.
# (Alternativa que preserva distroless: git-sync sidecar/init populando um volume
#  + server com `-git-source ""` lendo defs do disco — ver deploy/README.md.)
FROM alpine:3.20
RUN apk add --no-cache git ca-certificates \
 && addgroup -S regente && adduser -S -G regente -h /data regente \
 && mkdir -p /data && chown regente:regente /data
COPY --from=build /out/regente-server /usr/local/bin/regente-server
WORKDIR /data
# Porta default do server; o runtime serverless costuma injetar $PORT — o
# entrypoint respeita -addr; veja knative-service.yaml para o mapeamento.
EXPOSE 8080
USER regente
ENTRYPOINT ["regente-server"]
