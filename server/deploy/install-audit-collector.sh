#!/usr/bin/env bash
# Instala somente o coletor; nunca recebe a chave privada de assinatura.
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "Run as root" >&2; exit 1; }
[ "$#" = 5 ] || { echo "Usage: install-audit-collector.sh PUBLIC_KEY TOKEN_FILE TLS_CERT TLS_KEY ADDRESS" >&2; exit 2; }
public="$1"; token="$2"; cert="$3"; key="$4"; address="$5"
[[ "$address" =~ ^[a-zA-Z0-9.-]+:[0-9]+$ ]] || { echo "Invalid listen address" >&2; exit 2; }
for path in "$public" "$token" "$cert" "$key"; do [ -s "$path" ] || { echo "Required collector file unavailable" >&2; exit 2; }; done
[ -x /usr/local/bin/regente-server ] || { echo "Install regente-server first" >&2; exit 2; }
id regente-audit >/dev/null 2>&1 || useradd --system --home-dir /var/lib/regente-audit --shell /usr/sbin/nologin regente-audit
install -d -m 0700 -o regente-audit -g regente-audit /var/lib/regente-audit /etc/regente-audit
install -m 0600 -o regente-audit -g regente-audit "$public" /etc/regente-audit/public.key
install -m 0600 -o regente-audit -g regente-audit "$token" /etc/regente-audit/token
install -m 0644 -o regente-audit -g regente-audit "$cert" /etc/regente-audit/tls.crt
install -m 0600 -o regente-audit -g regente-audit "$key" /etc/regente-audit/tls.key
script_dir="$(cd "$(dirname "$0")" && pwd)"
sed "s|__ADDR__|$address|g" "$script_dir/regente-audit-collector.service" > /etc/systemd/system/regente-audit-collector.service
systemctl daemon-reload
systemctl enable --now regente-audit-collector
