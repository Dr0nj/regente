"""Declarative single-server / single-worker installation, separate from the lab.

Secrets are supplied by the installer on stdin, never written to rendered output.
All commands execute in the worker pod, not on the Kubernetes node.
"""
import hashlib
import json

PG = "postgres:17.6-alpine@sha256:747d5ed1fdeeb124b880fbe3d7c6557d2c4064ae41d6b6297d417882effce4be"
CAPS = ["COMMAND", "SCRIPT", "HTTP", "EXECUTION_V2"]


def resource(c, kind, name, spec=None, **extra):
    api = "apps/v1" if kind in ("Deployment", "StatefulSet") else "v1"
    return dict(apiVersion=api, kind=kind,
                metadata=dict(name=name, namespace=c["namespace"],
                              labels={"app.kubernetes.io/part-of": "regente"}),
                **({"spec": spec} if spec is not None else {}), **extra)


def secret_env(name, key):
    return {"name": name, "valueFrom": {"secretKeyRef": {"name": "regente-install", "key": key}}}


def mount(name, path, readonly=False):
    return dict(name=name, mountPath=path, readOnly=readonly)


def secret_volume(name, secret):
    return dict(name=name, secret=dict(secretName=secret, defaultMode=0o440))


def restricted(uid):
    return dict(runAsUser=uid, runAsGroup=uid, runAsNonRoot=True,
                allowPrivilegeEscalation=False, readOnlyRootFilesystem=True,
                capabilities={"drop": ["ALL"]})


def resources(memory="512Mi"):
    return dict(requests=dict(cpu="100m", memory="128Mi"), limits=dict(cpu="2", memory=memory))


def workload(c, kind, name, container, volumes, uid, init=()):
    pod = dict(automountServiceAccountToken=False, terminationGracePeriodSeconds=60,
               securityContext=dict(fsGroup=uid, fsGroupChangePolicy="OnRootMismatch",
                                    seccompProfile=dict(type="RuntimeDefault")),
               containers=[container], volumes=volumes)
    if init:
        pod["initContainers"] = list(init)
    if c.get("registryConfigFile"):
        pod["imagePullSecrets"] = [dict(name="regente-registry")]
    spec = dict(replicas=1, selector=dict(matchLabels={"app": name}),
                template=dict(metadata=dict(labels={"app": name}), spec=pod))
    if kind == "StatefulSet":
        spec["serviceName"] = name
    else:
        spec["strategy"] = dict(type="Recreate")
    return resource(c, kind, name, spec)


def manifests(c, bootstrap=True, git_enabled=False):
    out = [dict(apiVersion="networking.k8s.io/v1", kind="NetworkPolicy",
                metadata=dict(name="postgres-server-only", namespace=c["namespace"]),
                spec=dict(podSelector=dict(matchLabels={"app": "postgres"}), policyTypes=["Ingress"],
                          ingress=[{"from": [{"podSelector": {"matchLabels": {"app": "regente-server"}}}],
                                    "ports": [{"protocol": "TCP", "port": 5432}]}]))]
    for name, size in (("postgres-data", "20Gi"), ("server-data", "10Gi"), ("agent-journal", "5Gi")):
        out.append(resource(c, "PersistentVolumeClaim", name,
                            dict(accessModes=["ReadWriteOnce"], storageClassName=c["storageClass"],
                                 resources=dict(requests=dict(storage=size)))))
    for name, port in (("postgres", 5432), ("regente-server", 8443)):
        out.append(resource(c, "Service", name, dict(selector={"app": name},
                         ports=[dict(name="tls", port=port, targetPort=port)])))
    out.append(resource(c, "ConfigMap", "postgres-access", data={"pg_hba.conf":
        "local all all trust\nhostnossl all all all reject\nhostssl all all all scram-sha-256\n"}))
    pg = dict(name="postgres", image=PG,
              args=["-c", "ssl=on", "-c", "ssl_cert_file=/tls/tls.crt", "-c", "ssl_key_file=/tls/tls.key",
                    "-c", "hba_file=/access/pg_hba.conf"],
              env=[dict(name="POSTGRES_USER", value="regente"), dict(name="POSTGRES_DB", value="regente"),
                   dict(name="PGDATA", value="/var/lib/postgresql/data/pgdata"), secret_env("POSTGRES_PASSWORD", "dbPassword")],
              securityContext=restricted(70), resources=resources(),
              readinessProbe=dict(exec=dict(command=["pg_isready", "-h", "127.0.0.1", "-U", "regente"]), periodSeconds=5),
              volumeMounts=[mount("data", "/var/lib/postgresql/data"), mount("tls", "/tls", True),
                            mount("access", "/access", True), mount("tmp", "/tmp"), mount("run", "/var/run/postgresql")])
    out.append(workload(c, "StatefulSet", "postgres", pg,
        [dict(name="data", persistentVolumeClaim=dict(claimName="postgres-data")),
         secret_volume("tls", "regente-postgres-tls"), dict(name="access", configMap=dict(name="postgres-access")),
         dict(name="tmp", emptyDir={}), dict(name="run", emptyDir={})], 70))
    env = dict(REGENTE_PROFILE="production", REGENTE_ENVIRONMENT=c["environment"],
               REGENTE_NETWORK_BOUNDARY="tls", REGENTE_ADDR="0.0.0.0:8443", REGENTE_APP_URL="https://" + c["domain"],
               REGENTE_AUTH_MODE="local", REGENTE_CONTROL_PLANE_EXECUTION="deny", REGENTE_SERVER_AGENT="0",
               REGENTE_EXECUTION_MODE="durable", REGENTE_DB_DRIVER="postgres",
               REGENTE_WORKSPACE="/data/workspace" if git_enabled else "/data/bootstrap-workspace",
               REGENTE_SESSIONS_DIR="/data/sessions", REGENTE_AUDIT_KEY="/data/audit.key", REGENTE_SPA_DIR="/app",
               REGENTE_TLS_CERT="/tls/tls.crt", REGENTE_TLS_KEY="/tls/tls.key", REGENTE_TLS_CLIENT_CA="/tls/ca.crt",
               REGENTE_GIT_SOURCE=c["gitSource"] if git_enabled else "", REGENTE_GIT_BRANCH=c.get("gitBranch", "main"))
    server = dict(name="server", image=c["serverImage"], workingDir="/data", securityContext=restricted(65532),
                  resources=resources("1Gi"), env=[dict(name=k, value=v) for k, v in env.items()] +
                  [secret_env("REGENTE_DB", "dbDSN"), secret_env("GITHUB_TOKEN", "gitToken")],
                  volumeMounts=[mount("data", "/data"), mount("tls", "/tls", True), mount("tmp", "/tmp")])
    if bootstrap:
        server["env"] += [secret_env("REGENTE_BOOTSTRAP_USER", "username"), secret_env("REGENTE_BOOTSTRAP_PASSWORD", "password")]
    for key, endpoint in (("startupProbe", "/livez"), ("livenessProbe", "/livez"), ("readinessProbe", "/readyz")):
        server[key] = dict(httpGet=dict(path=endpoint, port=8443, scheme="HTTPS"), periodSeconds=5,
                           timeoutSeconds=3, failureThreshold=60 if key == "startupProbe" else 6)
    out.append(workload(c, "Deployment", "regente-server", server,
        [dict(name="data", persistentVolumeClaim=dict(claimName="server-data")),
         secret_volume("tls", "regente-server-tls"), dict(name="tmp", emptyDir={})], 65532))
    return out


def worker(c, credential_digest):
    args = ["-transport", "v2", "-server", "https://regente-server:8443", "-id", c["agentId"],
            "-env", c["environment"], "-caps", ",".join(CAPS), "-journal", "/state/agent.db",
            "-token-file", "/credentials/token", "-tls-cert", "/credentials/tls.crt",
            "-tls-key", "/credentials/tls.key", "-tls-ca", "/credentials/ca.crt"]
    agent = dict(name="agent", image=c["agentImage"], args=args, securityContext=restricted(10001),
                 resources=resources(), volumeMounts=[mount("state", "/state"), mount("credentials", "/credentials", True),
                                                      mount("tmp", "/tmp")])
    copy = dict(name="credentials", image=c["agentImage"], command=["/bin/sh", "-ec",
        "cp /source/* /credentials/; chmod 600 /credentials/*"], securityContext=restricted(10001),
        resources=resources(), volumeMounts=[mount("source", "/source", True), mount("credentials", "/credentials")])
    check = dict(name="journal-preflight", image=c["agentImage"],
                 args=["-check-journal", "-id", c["agentId"], "-journal", "/state/agent.db"],
                 securityContext=restricted(10001), resources=resources(), volumeMounts=[mount("state", "/state")])
    obj = workload(c, "StatefulSet", "regente-agent", agent,
        [dict(name="state", persistentVolumeClaim=dict(claimName="agent-journal")),
         secret_volume("source", "regente-agent-credentials"), dict(name="credentials", emptyDir=dict(medium="Memory")),
         dict(name="tmp", emptyDir={})], 10001, [copy, check])
    obj["spec"]["template"]["metadata"]["annotations"] = {"regente.io/credential-sha256": credential_digest}
    return obj


def ingress(c):
    ns, domain = c["namespace"], c["domain"]
    return [
        dict(apiVersion="cert-manager.io/v1", kind="Certificate", metadata=dict(name="regente-public", namespace=ns),
             spec=dict(secretName="regente-public-tls", dnsNames=[domain],
                       issuerRef=dict(name=c["publicIssuer"], kind="ClusterIssuer"))),
        dict(apiVersion="traefik.io/v1alpha1", kind="ServersTransport", metadata=dict(name="regente", namespace=ns),
             spec=dict(serverName="regente-server", rootCAsSecrets=["regente-trust"])),
        dict(apiVersion="traefik.io/v1alpha1", kind="IngressRoute", metadata=dict(name="regente", namespace=ns),
             spec=dict(entryPoints=["websecure"], tls=dict(secretName="regente-public-tls"), routes=[
                 dict(match="Host(`" + domain + "`)", kind="Rule", services=[
                     dict(name="regente-server", port=8443, scheme="https", serversTransport="regente")])]))]


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()
