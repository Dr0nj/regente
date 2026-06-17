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

# Imagem final mínima e sem shell (superfície de ataque reduzida).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/regente-server /regente-server
# Porta default do server; o runtime serverless costuma injetar $PORT — o
# entrypoint respeita -addr; veja knative-service.yaml para o mapeamento.
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/regente-server"]
