#!/usr/bin/env bash
# enable-tls.sh — emite o cert TLS (Lets Encrypt via plugin nginx) e liga o HTTPS.
#
#   sudo DOMAIN=regente.suaempresa.com EMAIL=ops@suaempresa.com ./enable-tls.sh
#
# O certbot --nginx obtém o certificado, converte o server{} do nginx-regente.conf
# para :443 TLS e adiciona o redirect 80->443, PRESERVANDO as locations (WS/SSE/proxy).
# A renovação passa a ser automática (certbot.timer + reload do nginx).
#
# Pré-requisitos:
#   - nginx instalado com o nginx-regente.conf em /etc/nginx/conf.d/ (server_name = seu domínio)
#   - DNS do domínio apontando (A/AAAA) pro IP deste VPS
#   - portas 80 e 443 abertas no firewall/security group
set -euo pipefail

DOMAIN="${DOMAIN:?set DOMAIN=your.domain.com}"
EMAIL="${EMAIL:?set EMAIL=you@yourdomain.com for certificate expiry notices}"
[ "$(id -u)" = 0 ] || { echo "run as root (sudo)"; exit 1; }
command -v nginx   >/dev/null || { echo "install nginx first:  apt install -y nginx"; exit 1; }
command -v certbot >/dev/null || { echo "install certbot:  apt install -y certbot python3-certbot-nginx"; exit 1; }

nginx -t
certbot --nginx -d "$DOMAIN" --email "$EMAIL" --agree-tos --non-interactive --redirect
echo ""
echo "HTTPS is live at https://$DOMAIN — renewal is automatic (certbot.timer)."
echo "Check it:  curl -sSI https://$DOMAIN | head -1     (should answer 200/302)"
echo "Point your agents at wss://$DOMAIN/ws/agent (or the SSE/long-poll transport)."
