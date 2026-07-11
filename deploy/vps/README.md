# Hospedagem enterprise do Regente num VPS (systemd + nginx + TLS + domínio)

> Como uma **empresa grande** colocaria o Regente no ar num link público: TLS terminado
> na **borda** (reverse proxy), **domínio real**, identidade por **SSO**, auditoria pra
> **SIEM**, agentes **outbound**. O control plane já traz as peças de identidade/auditoria/
> HA (ver "Endurecimento" abaixo) — esta pasta cobre a **borda** (proxy + TLS + domínio),
> tudo **systemd** (sem Docker). Para expor pra amigos com segurança, leia também "Etapa 2".

## Arquitetura

```
  usuários / amigos (browser)                 agentes (nas OUTRAS máquinas)
        │  https://regente.suaempresa.com            │  wss://regente.suaempresa.com/ws/agent
        │  (TLS)                                      │  (outbound — não abre porta no agente)
        ▼                                             ▼
  ┌──────────────────────────── VPS (systemd) ─────────────────────────────┐
  │  nginx :443  ── reverse proxy (TLS) ──►  regente-server 127.0.0.1:8080   │
  │  certbot.timer (renova o cert)           UI + API + WS single-origin      │
  │  ufw (80/443 abertos; 8080 só loopback)  estado em /var/lib/regente       │
  └──────────────────────────────────────────────────────────────────────────┘
```

O `:8080` **nunca** é exposto direto: só o nginx fala com ele, pelo loopback. O front
resolve `@origin`, então o mesmo build funciona em qualquer domínio sem rebuildar.

## Passo a passo

```bash
# 0) DNS: crie um registro A (e AAAA se tiver IPv6) do domínio -> IP do VPS.
#    Confirme:  dig +short regente.suaempresa.com

# 1) Server (Forma 2, com UI single-origin) — ver README raiz. Confirme local:
curl -sSI http://127.0.0.1:8080/health      # deve responder 200

# 2) nginx
sudo apt update && sudo apt install -y nginx
sudo cp deploy/vps/nginx-regente.conf /etc/nginx/conf.d/regente.conf
sudo sed -i 's/REGENTE_DOMAIN/regente.suaempresa.com/' /etc/nginx/conf.d/regente.conf
sudo nginx -t && sudo systemctl reload nginx

# 3) TLS (Let's Encrypt, via plugin nginx) — sobe o :443 e o redirect 80->443
sudo apt install -y certbot python3-certbot-nginx
sudo DOMAIN=regente.suaempresa.com EMAIL=ops@suaempresa.com ./deploy/vps/enable-tls.sh

# 4) Firewall: exponha só a borda; feche o 8080.
sudo ufw allow 80,443/tcp && sudo ufw enable
#   (o :8080 já está em 127.0.0.1 no proxy; se o REGENTE_ADDR for :8080 público,
#    troque p/ 127.0.0.1:8080 no server.env e reinicie o serviço.)
```

Pronto: `https://regente.suaempresa.com` (login inicial `admin`/`admin`, troca obrigatória).
A renovação do cert é automática (`systemctl status certbot.timer`).

> **Amarre o server ao loopback:** no `/etc/regente/server.env` use
> `REGENTE_ADDR=127.0.0.1:8080` (só o nginx alcança) e `sudo systemctl restart regente-server`.
> Assim nem por engano o control plane fica acessível sem passar pela borda TLS.

## Endurecimento "estilo empresa" (o que o Regente JÁ tem — é só ligar)

Estas peças normalmente são o que separa um "deploy de teste" de um "deploy corporativo".
No Regente elas já existem; ative pelo `/etc/regente/server.env` (ou flags):

| Camada enterprise | Como ligar no Regente |
|---|---|
| **SSO / OIDC** (nada de `admin/admin`) | `REGENTE_AUTH_MODE=oidc` + `REGENTE_OIDC_ISSUER/CLIENT_ID/CLIENT_SECRET/REDIRECT_URL`. 1º login federado provisiona o user (role default configurável). |
| **RBAC + ACL por folder** | Settings → Usuários: papéis `operator`/`viewer`; ACL de leitura/escrita por folder. |
| **Auditoria → SIEM** | `REGENTE_AUDIT_SIEM_URL=https://siem/...` (login, writes viram eventos JSON POSTados). |
| **Segredos fora da DB** | `REGENTE_SECRET_GITHUB_TOKEN=...` (secrets provider; não persiste PAT em claro). |
| **mTLS server ↔ agente** | server com `-tls-client-ca` exige cert de cliente do agente (defesa em profundidade). |
| **HA / escala** | `REGENTE_DB_DRIVER=postgres` + N nós com o MESMO DB → leader election (só o líder roda daily/dispatch). Backup/DR em [`../../docs/dr-backup.md`](../../docs/dr-backup.md). |
| **Agentes NAT-friendly** | conexão **outbound** (WS/SSE/long-poll) — o agente nunca abre porta; atravessa firewall corporativo. |
| **Token de API forte** | `REGENTE_TOKEN` é admin-equivalente (bypassa login) — **gere um valor forte**; nunca deixe `dev-token`/`change-me`. |

> **Alternativa sem proxy:** o server termina TLS sozinho (`REGENTE_TLS_CERT`/`REGENTE_TLS_KEY`)
> se você não quiser nginx na frente. O reverse proxy é o padrão enterprise (headers de
> borda, HSTS, hospedar vários serviços, desacoplar a gestão de cert), por isso é o default aqui.

## Etapa 2 — convidar amigos com segurança (agente sandbox — V5)

O Regente **executa comandos**. Quem executa é o **agente** — e os jobs rodam **onde o agente
está**. Então **não rode um agente comum no VPS de produção** pra uma demo aberta, senão os
jobs `COMMAND` dos amigos rodam no seu host. Use o **agente sandbox**: um container Docker
isolado (`--cap-drop ALL`, `no-new-privileges`, limites de CPU/RAM/PID, sem mounts do host),
supervisionado por systemd:

```bash
# Requer Docker no VPS (o Dockerfile builda o Go dentro do Docker — sem Go no host).
sudo apt install -y docker.io    # ou o Docker Engine oficial
# gere um token de agente em Settings → Agentes (ou use o REGENTE_TOKEN), e:
sudo AGENT_TOKEN=rgta_xxx ./sandbox-agent.sh
#   → sobe o container 'regente-sandbox' + o serviço regente-agent-sandbox (Restart=always)
#   → journalctl -u regente-agent-sandbox -f   |   docker logs -f regente-sandbox
```

Depois, em **Settings → Usuários**, crie uma conta por amigo (`operator` cria/roda jobs,
`viewer` só observa — **nunca** compartilhe o admin) e decida o `git-write-mode`
(`direct` = commit direto no workspace, `pr-required` = cada mudança vira PR pra você aprovar).
Jobs dos amigos ficam presos no container; `sudo systemctl stop regente-agent-sandbox` derruba.

> Rede do sandbox fica **ligada** (jobs HTTP funcionam). Pra cortar a saída de rede, adicione
> `--network none` ao `ExecStart` de `/etc/systemd/system/regente-agent-sandbox.service` e
> `systemctl daemon-reload && systemctl restart regente-agent-sandbox`.

## Config guiada (V3)

Em vez de editar o `server.env` à mão, rode o assistente (pergunta token forte, GitHub
PAT/repo e domínio, e escreve o env):

```bash
sudo regente-configure     # instalado pelo install-linux.sh; precisa de terminal (não via pipe)
```

## Atualizar / derrubar / subir

```bash
sudo systemctl restart regente-server     # aplica novo binário/config (migrations rodam no boot)
sudo systemctl stop regente-server        # derruba (estado persiste em /var/lib/regente)
journalctl -u regente-server -f           # logs
```

Estado (jobs, users, config, PAT) sobrevive a reboot/upgrade — ver R4 em
[`../../docs/dr-backup.md`](../../docs/dr-backup.md). Para upgrade sem downtime com Postgres/HA,
ver [`../../server/deploy/rolling-upgrade.sh`](../../server/deploy/rolling-upgrade.sh).
