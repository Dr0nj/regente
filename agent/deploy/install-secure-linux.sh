#!/usr/bin/env bash
# Perfil homologado I13: célula HTTP dedicada, systemd/cgroup, sem comandos arbitrários.
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "Run as root."; exit 1; }
SERVER="${SERVER:?Set an HTTPS server origin}"
ID="${ID:?Set the provisioned machine ID}"
ENVIRONMENT="${ENVIRONMENT:?Set the exact provisioned environment}"
TOKEN_FILE="${TOKEN_FILE:?Set a protected machine credential file}"
TLS_CERT="${TLS_CERT:?Set the client certificate PEM path}"
TLS_KEY="${TLS_KEY:?Set the private key PEM path}"
TLS_CA="${TLS_CA:?Set the server CA PEM path}"
POLICY_FILE="${POLICY_FILE:?Set the execution policy JSON path}"
SECRETS_FILE="${SECRETS_FILE:?Set the runtime secrets JSON path}"
EGRESS_CIDRS="${EGRESS_CIDRS:?Set numeric IP/CIDRs for the server and authorized destinations}"
BINARY="${BINARY:-/usr/local/bin/regente-agent}"
case "$SERVER" in https://*) ;; *) echo "An HTTPS server origin is required."; exit 1 ;; esac
case "$ID" in *[!a-zA-Z0-9._-]*|'') echo "Invalid machine ID."; exit 1 ;; esac
case "$ENVIRONMENT" in *[!a-zA-Z0-9._-]*|'') echo "Invalid environment."; exit 1 ;; esac
[ -x "$BINARY" ] || { echo "Install the release agent binary first."; exit 1; }
command -v python3 >/dev/null || { echo "python3 is required for deployment validation."; exit 1; }
# Validação do contrato antes de qualquer mutação de serviço.
EGRESS_CIDRS="$(python3 - "$POLICY_FILE" "$ENVIRONMENT" "$EGRESS_CIDRS" "$SERVER" "$BINARY" <<'PY'
import ipaddress,json,sys,urllib.parse
url=urllib.parse.urlsplit(sys.argv[4])
assert url.scheme=="https" and url.hostname and not url.username and not url.password and not url.query and not url.fragment and url.path in ("","/") and not any(c.isspace() for c in sys.argv[4]),"Invalid HTTPS origin"
assert sys.argv[5].startswith("/") and not any(c.isspace() or c=="%" for c in sys.argv[5]),"Invalid binary path"
p=json.load(open(sys.argv[1]))
assert p["environment"]==sys.argv[2] and p["jobs"],"Policy environment/jobs mismatch"
for j in p["jobs"].values():
    assert j["types"] and set(j["types"])<= {"HTTP","REST"},"Secure cell supports HTTP/REST only"
cidrs=sys.argv[3].split()
assert cidrs,"Empty egress policy"
for cidr in cidrs:
    n=ipaddress.ip_network(cidr,strict=False)
    assert n.prefixlen>0 and not n.network_address.is_link_local and not n.network_address.is_multicast,"Invalid egress CIDR"
print(" ".join(cidrs))
PY
)"
for file in "$TOKEN_FILE" "$TLS_CERT" "$TLS_KEY" "$TLS_CA" "$POLICY_FILE" "$SECRETS_FILE"; do
 [ -f "$file" ] && [ ! -L "$file" ] || { echo "Protected input file missing or symlink."; exit 1; }
done
getent passwd regente-agent-http >/dev/null || useradd --system --home-dir /var/lib/regente-agent-http --shell /usr/sbin/nologin regente-agent-http
CONF=/etc/regente-agent-http
install -d -m 0700 -o regente-agent-http -g regente-agent-http "$CONF"
for spec in "token:$TOKEN_FILE" "client.crt:$TLS_CERT" "client.key:$TLS_KEY" "ca.crt:$TLS_CA" "policy.json:$POLICY_FILE" "secrets.json:$SECRETS_FILE"; do
 dest="${spec%%:*}";source="${spec#*:}"
 [ "$source" = "$CONF/$dest" ] || install -m 0600 -o regente-agent-http -g regente-agent-http "$source" "$CONF/$dest"
done
install -d -m 0700 -o regente-agent-http -g regente-agent-http /var/lib/regente-agent-http
UNIT=/etc/systemd/system/regente-agent-http.service
cat > "$UNIT" <<EOF
[Unit]
Description=Regente restricted HTTP execution cell
After=network-online.target
Wants=network-online.target

[Service]
User=regente-agent-http
Group=regente-agent-http
ExecStart=$BINARY -transport v2 -server $SERVER -id $ID -env $ENVIRONMENT -caps HTTP,REST -journal /var/lib/regente-agent-http/journal.db -token-file $CONF/token -tls-cert $CONF/client.crt -tls-key $CONF/client.key -tls-ca $CONF/ca.crt -execution-policy $CONF/policy.json -job-secrets-file $CONF/secrets.json
Restart=always
RestartSec=3
UMask=0077
NoNewPrivileges=yes
CapabilityBoundingSet=
AmbientCapabilities=
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
RestrictRealtime=yes
RestrictAddressFamilies=AF_INET AF_INET6
ReadOnlyPaths=$CONF
ReadWritePaths=/var/lib/regente-agent-http
TasksMax=128
CPUQuota=100%
MemoryMax=512M
MemorySwapMax=0
LimitNOFILE=1024
IPAddressDeny=any
IPAddressAllow=$EGRESS_CIDRS
IPAccounting=yes

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now regente-agent-http
echo "Restricted cell installed. Verify active mTLS, actual denied egress, cgroup limits and journal receipts before admitting jobs."
echo "Secrets and certificates: replace files atomically in $CONF; runtime cache TTL is zero."
