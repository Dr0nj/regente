"""Real installer qualification in dedicated namespaces on an existing kind cluster.
Does not touch the user's workspace, VPS, DNS or pre-existing lab workloads.
Keeps state/effects for inspection. Never removes namespaces or PVCs automatically.
"""
import argparse
import json
from pathlib import Path
import secrets
import install
import install_resources as model


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--context", required=True)
    p.add_argument("--server-image", required=True)
    p.add_argument("--agent-image", required=True)
    p.add_argument("--fixture-image", required=True)
    p.add_argument("--state-dir", required=True)
    p.add_argument("--namespace", default="regente-onboarding-check")
    a = p.parse_args()
    c = dict(context=a.context, namespace=a.namespace, environment="onboarding-check",
             domain="regente.invalid", storageClass="standard", serverImage=a.server_image,
             agentImage=a.agent_image, gitSource="git://git-fixture.regente-onboarding-fixture.svc/",
             gitBranch="main", agentId="onboarding-worker-01")
    i = install.Installer(c, a.state_dir, local=True)
    fixture_ns = "regente-onboarding-fixture"
    existing = json.loads(i.kube("get", "namespace", fixture_ns, "--ignore-not-found", "-o", "json") or b"{}")
    if existing and existing.get("metadata", {}).get("labels", {}).get("regente.io/test") != "onboarding":
        raise RuntimeError("Fixture namespace exists and is not owned by this qualification")
    i.apply(dict(apiVersion="v1", kind="Namespace", metadata=dict(name=fixture_ns, labels={"regente.io/test": "onboarding"})))
    fc = {**c, "namespace": fixture_ns}
    condition = "DRON-TESTE-DEP-0001-TO-DRON-TESTE-DEP-0003"
    defs = {}
    for identifier in ("DRON-TESTE-DEP-0001", "DRON-TESTE-DEP-0003"):
        definition = dict(id=identifier, label=identifier, team="example", jobType="COMMAND",
                          schedule=dict(enabled=False), environment=c["environment"], agentId=c["agentId"],
                          params=dict(command="echo set-ok-successor"))
        if identifier.endswith("0001"):
            definition.update(conditionsIn=["manual-test-gate-never-set"], conditionsOutAdd=[condition])
        else:
            definition.update(conditionsIn=[condition])
        defs[identifier + ".yaml"] = json.dumps(definition)
    i.apply(model.resource(fc, "ConfigMap", "git-definitions", data=defs))
    i.apply(model.resource(fc, "Service", "git-fixture", dict(selector={"app": "git-fixture"}, ports=[dict(port=9418)])))
    fixture = dict(name="git", image=a.fixture_image, command=["/bin/sh", "-ec",
        "mkdir -p /repo/definitions/example; cd /repo; git init -b main; cp /seed/* definitions/example/; "
        "git -c safe.directory=/repo add .; git -c safe.directory=/repo diff --cached --quiet || git -c safe.directory=/repo -c user.name=Fixture -c user.email=fixture@example.invalid commit -m Fixture; "
        "exec git -c safe.directory=/repo daemon --reuseaddr --export-all --base-path=/repo --listen=0.0.0.0 /repo"],
        securityContext=model.restricted(10001), resources=model.resources("128Mi"),
        readinessProbe=dict(tcpSocket=dict(port=9418), periodSeconds=2),
        volumeMounts=[model.mount("repo", "/repo"), model.mount("tmp", "/tmp"), model.mount("seed", "/seed", True)])
    i.apply(model.workload(fc, "Deployment", "git-fixture", fixture,
                          [dict(name="repo", emptyDir={}), dict(name="tmp", emptyDir={}), dict(name="seed", configMap=dict(name="git-definitions"))], 10001))
    install.run(["kubectl", "--context", a.context, "-n", fixture_ns, "rollout", "status", "deployment/git-fixture", "--timeout=120s"])
    # Test-generated credentials stay solely in the protected installer state.
    install.getpass.getpass = lambda prompt: secrets.token_urlsafe(32) if "password" in prompt else ""
    i.install()
    before = {k: i.state[k] for k in ("installId", "agentTokenId", "agentToken", "smokeInstance")}
    i.install()
    assert before == {k: i.state[k] for k in before}, "Reconcile changed identity or repeated smoke order"
    i.report["checks"]["second_install_preserves_identity_and_effect"] = True
    i.kube("rollout", "restart", "statefulset/regente-agent")
    i.kube("rollout", "status", "statefulset/regente-agent", "--timeout=180s")
    i.kube("rollout", "restart", "deployment/regente-server")
    i.kube("rollout", "status", "deployment/regente-server", "--timeout=180s")
    with i.connection():
        i.wait("restarted agent online", lambda: any(x["id"] == c["agentId"] and x["online"] for x in i.api("/api/agents")))
        i.smoke()
        i.design()
    i.report["checks"]["server_agent_restart_preserves_effect_and_design"] = True
    with i.connection():
        if "setOKTest" not in i.state:
            parent = i.api("/api/definitions/DRON-TESTE-DEP-0001/force", {})["instanceId"]
            child = i.api("/api/definitions/DRON-TESTE-DEP-0003/force", {})["instanceId"]
            i.state["setOKTest"] = dict(parent=parent, child=child)
            i.save()
            explanation = i.api("/api/instances/" + child + "/explain")
            assert any(b["kind"] == "WAIT_CONDITION" for b in explanation["blockers"])
            i.api("/api/instances/" + parent + "/set-ok", {})
        child = i.state["setOKTest"]["child"]
        i.wait("Set OK releases successor", lambda: i.api("/api/instances/" + child)["status"] == "OK")
        i.report["checks"]["set_ok_releases_condition_successor"] = i.state["setOKTest"]
        old_token = i.state["agentTokenId"]
        i.rotate_agent = True
        i.enroll()
        assert i.state["agentTokenId"] != old_token
        i.smoke()
        i.report["checks"]["credential_rotation_preserves_journal_and_effect"] = True
    i.save_report()
    print(json.dumps(i.report, indent=2))


if __name__ == "__main__":
    main()
