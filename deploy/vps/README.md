# Enterprise-style hosting on a VPS (systemd + nginx + TLS + a domain)

> How a **large company** would put Regente on a public link: TLS terminated at the **edge** (a
> reverse proxy), a **real domain**, identity through **SSO**, audit forwarded to a **SIEM**, and
> **outbound** agents. The control plane already ships the identity, audit and HA pieces (see
> "Hardening" below) — this folder covers the **edge** (proxy + TLS + domain), all through
> **systemd** (no Docker). If you plan to invite other people in, also read "Step 2".

## Architecture

```
  users / guests (browser)                     agents (on the OTHER machines)
        │  https://regente.yourcompany.com           │  wss://regente.yourcompany.com/ws/agent
        │  (TLS)                                     │  (outbound — opens no port on the agent)
        ▼                                            ▼
  ┌──────────────────────────── VPS (systemd) ─────────────────────────────┐
  │  nginx :443  ── reverse proxy (TLS) ──►  regente-server 127.0.0.1:8080  │
  │  certbot.timer (renews the cert)         UI + API + WS, single-origin   │
  │  ufw (80/443 open; 8080 loopback only)   state in /var/lib/regente      │
  └────────────────────────────────────────────────────────────────────────┘
```

Port `:8080` is **never** exposed directly: only nginx talks to it, over the loopback. The
frontend resolves `@origin`, so the same build works on any domain without a rebuild.

## Step by step

> **Where these files are.** `install-linux.sh` copies this folder to
> **`/var/lib/regente/deploy/vps`**, so the commands below work on a machine installed with the
> one-liner — which downloads the bundle into a temporary directory and deletes it on exit, so
> there is no checkout to copy from. Running from a checkout instead? Set `VPS=deploy/vps`.

```bash
VPS=/var/lib/regente/deploy/vps          # from a checkout: VPS=deploy/vps
DOMAIN=regente.yourcompany.com

# 0) Server up (option 2, with the single-origin UI) — see the root README:
curl -sSI http://127.0.0.1:8080/health      # must answer 200

# 1) DNS — do this FIRST, and prove it. Create an A record (and AAAA if you have
#    IPv6) pointing the domain at THIS machine, then confirm it resolves here:
hostname -I                                 # this machine's addresses
getent hosts "$DOMAIN"                      # must answer with one of them
```

**Do not skip the check in step 1.** Let's Encrypt validates by fetching
`http://$DOMAIN/.well-known/…`, so if the name still resolves to the registrar's parking page the
certificate fails — and the error never mentions DNS, which sends you hunting in the wrong place.
A freshly bought domain does not start blank: it already points somewhere.

```bash
# 2) nginx
sudo apt update && sudo apt install -y nginx
sudo cp "$VPS/nginx-regente.conf" /etc/nginx/conf.d/regente.conf
sudo sed -i "s/REGENTE_DOMAIN/$DOMAIN/" /etc/nginx/conf.d/regente.conf
sudo nginx -t && sudo systemctl reload nginx
curl -sSI "http://$DOMAIN/" | head -1       # 200 = the whole path already works, before any TLS

# 3) TLS (Let's Encrypt, through the nginx plugin) — brings up :443 and the 80->443 redirect
sudo apt install -y certbot python3-certbot-nginx
sudo DOMAIN="$DOMAIN" EMAIL=ops@yourcompany.com bash "$VPS/enable-tls.sh"

# 4) Close the back door: the proxy reaches the server over the loopback, so the
#    control plane should not be listening on a public address at all.
sudo sed -i 's|^REGENTE_ADDR=.*|REGENTE_ADDR=127.0.0.1:8080|' /etc/regente/server.env
sudo systemctl restart regente-server

# 5) Firewall: expose only the edge. Allow SSH in the SAME command — enabling ufw
#    without it locks you out of the machine. (OpenSSH is port 22; if your sshd
#    listens elsewhere, allow that port instead.)
sudo ufw allow OpenSSH && sudo ufw allow 80,443/tcp && sudo ufw enable
```

> **More than one name?** If you also created `www` (a CNAME to the apex is the usual shape), put
> both in `server_name` **before** running certbot — it reads that directive to decide which names
> go into the certificate.

Done: `https://regente.yourcompany.com` (initial login `admin`/`admin`, which you must change).
Certificate renewal is automatic (`systemctl status certbot.timer`).

> **Why step 4 matters:** with `REGENTE_ADDR=127.0.0.1:8080` only nginx can reach the control
> plane, so it can never be talked to without going through the TLS edge — not even by accident,
> and not even if the firewall is later opened by someone else.

## Enterprise hardening (what Regente ALREADY has — you just switch it on)

These are usually the pieces that separate a "test deployment" from a "corporate deployment". In
Regente they already exist; enable them through `/etc/regente/server.env` (or flags):

| Enterprise layer | How to enable it |
|---|---|
| **SSO / OIDC** (no more `admin/admin`) | `REGENTE_AUTH_MODE=oidc` + `REGENTE_OIDC_ISSUER/CLIENT_ID/CLIENT_SECRET/REDIRECT_URL`. The first federated login provisions the user (the default role is configurable). |
| **RBAC + per-folder ACL** | Settings → Users: the `operator`/`viewer` roles; read/write ACLs per folder. |
| **Audit → SIEM** | `REGENTE_AUDIT_SIEM_URL=https://siem/...` (logins and writes become JSON events that get POSTed). |
| **Secrets outside the DB** | `REGENTE_SECRET_GITHUB_TOKEN=...` (the secrets provider; no PAT persisted in plaintext). |
| **mTLS between server and agent** | a server started with `-tls-client-ca` requires a client certificate from the agent (defence in depth). |
| **HA / scale** | `REGENTE_DB_DRIVER=postgres` + N nodes on the SAME DB → leader election (only the leader runs the daily and dispatch). Backup/DR in [`../../docs/dr-backup.md`](../../docs/dr-backup.md). |
| **NAT-friendly agents** | an **outbound** connection (WS/SSE/long-poll) — the agent never opens a port; it crosses corporate firewalls. |
| **A strong API token** | `REGENTE_TOKEN` is admin-equivalent (it bypasses login) — **generate a strong value**; never leave `dev-token`/`change-me`. |

> **Alternative without a proxy:** the server can terminate TLS itself
> (`REGENTE_TLS_CERT`/`REGENTE_TLS_KEY`) if you would rather not run nginx in front. A reverse
> proxy is the enterprise standard (edge headers, HSTS, hosting several services, decoupling
> certificate management), which is why it is the default here.

## Step 2 — inviting other people safely (the sandbox agent)

Regente **executes commands**. The **agent** is what executes them — and jobs run **wherever the
agent is**. So **do not run an ordinary agent on your production VPS** for an open demo, or your
guests' `COMMAND` jobs will run on your host. Use the **sandbox agent**: an isolated Docker
container (`--cap-drop ALL`, `no-new-privileges`, CPU/RAM/PID limits, no host mounts), supervised
by systemd:

```bash
# Requires Docker on the VPS (the Dockerfile builds the Go inside Docker — no Go on the host).
sudo apt install -y docker.io    # or the official Docker Engine
# create an agent token in Settings → Agents (or use REGENTE_TOKEN), then:
sudo AGENT_TOKEN=rgta_xxx ./sandbox-agent.sh
#   → starts the 'regente-sandbox' container + the regente-agent-sandbox service (Restart=always)
#   → journalctl -u regente-agent-sandbox -f   |   docker logs -f regente-sandbox
```

Then, in **Settings → Users**, create one account per guest (`operator` creates and runs jobs,
`viewer` only observes — **never** share the admin account) and decide the `git-write-mode`
(`direct` = commits straight into the workspace, `pr-required` = every change becomes a PR for you
to approve). Guest jobs stay inside the container;
`sudo systemctl stop regente-agent-sandbox` shuts it down.

> The sandbox's network stays **on** (HTTP jobs work). To cut outbound network access, add
> `--network none` to the `ExecStart` line of
> `/etc/systemd/system/regente-agent-sandbox.service` and run
> `systemctl daemon-reload && systemctl restart regente-agent-sandbox`.

## Guided setup

Instead of editing `server.env` by hand, run the assistant (it asks for a strong token, the
GitHub PAT/repo and the domain, then writes the env file):

```bash
sudo regente-configure     # installed by install-linux.sh; needs a terminal (not through a pipe)
```

## Update / stop / start

```bash
sudo systemctl restart regente-server     # applies a new binary/config (migrations run at boot)
sudo systemctl stop regente-server        # stops it (state persists in /var/lib/regente)
journalctl -u regente-server -f           # logs
```

State (jobs, users, configuration, the PAT) survives reboots and upgrades — see R4 in
[`../../docs/dr-backup.md`](../../docs/dr-backup.md). For a zero-downtime upgrade with
Postgres/HA, see
[`../../server/deploy/rolling-upgrade.sh`](../../server/deploy/rolling-upgrade.sh).
