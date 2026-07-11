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

DOMAIN="${DOMAIN:?defina DOMAIN=seu.dominio.com}"
EMAIL="${EMAIL:?defina EMAIL=voce@dominio.com para avisos de expiracao do certificado}"
[ "$(id -u)" = 0 ] || { echo "rode como root (sudo)"; exit 1; }
command -v nginx   >/dev/null || { echo "instale o nginx primeiro:  apt install -y nginx"; exit 1; }
command -v certbot >/dev/null || { echo "instale o certbot:  apt install -y certbot python3-certbot-nginx"; exit 1; }

nginx -t
certbot --nginx -d "$DOMAIN" --email "$EMAIL" --agree-tos --non-interactive --redirect
echo ""
echo "HTTPS ativo em https://$DOMAIN — renovação automática (certbot.timer)."
echo "Confirme:  curl -sSI https://$DOMAIN | head -1     (deve responder 200/302)"
echo "Aponte os agentes p/ wss://$DOMAIN/ws/agent (ou o transporte SSE/long-poll)."
