#!/usr/bin/env python3
"""I17 disposable Linux recovery rehearsal. Never connects to an installed deployment."""
import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import platform
import shlex
import shutil
import sqlite3
import ssl
import sys
import time

import integration as lab


CHECKS = ("older_snapshot_locally_valid", "older_snapshot_checkpoint_rejected",
          "current_checkpoint_verified", "missing_key_rejected", "draft_preserved",
          "definition_commit_preserved", "execution_identity_preserved",
          "agent_journal_preserved", "effect_not_repeated", "configuration_preserved", "condition_preserved")


def validate(report):
    if any(report.get(name) is not True for name in CHECKS):
        raise RuntimeError("Incomplete I17 recovery evidence")
    if report.get("effect_count") != 1 or report.get("snapshot_committed_order_loss") != 0:
        raise RuntimeError("Recovery lost a committed order or repeated its effect")
    if report.get("operational_acceptance") != "pending-independent-operator-and-pilot":
        raise RuntimeError("Synthetic evidence cannot accept an operational pilot")


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def sqlite_snapshot(source, target):
    # Includes WAL through the SQLite backup API; target is exclusively new.
    with Path(target).open("xb"):
        pass
    with closing(sqlite3.connect(Path(source).resolve().as_uri() + "?mode=ro", uri=True)) as src:
        with closing(sqlite3.connect(target)) as dst:
            src.backup(dst)
            if dst.execute("PRAGMA integrity_check").fetchone() != ("ok",):
                raise RuntimeError("Journal snapshot failed integrity check")


def rehearse(env, tls_dir, driver="sqlite", dsn=None, server_bin=None, agent_bin=None):
    if platform.system() != "Linux":
        raise RuntimeError("I17 rehearsal requires Linux; the ephemeral TLS CA is process-local through SSL_CERT_FILE")
    started = time.monotonic()
    server_bin = str(server_bin or lab.RUN / "server")
    agent_bin = str(agent_bin or lab.RUN / "agent")
    root = lab.RUN / ("i17-" + driver)
    root.mkdir()
    source, remote, worker, independent = [root / p for p in ("source", "git", "worker", "independent")]
    for folder in (source, remote, worker, independent):
        folder.mkdir()
    pg = lab.COMPOSE + ["exec", "-T", "postgres"]
    dbname = "regente_i17_source"
    if driver == "postgres":
        if not dsn:
            raise RuntimeError("Disposable integration DSN required")
        lab.command(pg + ["createdb", "-U", "regente", dbname])
        database = dsn.replace("/regente_lab?", "/" + dbname + "?")
    else:
        database = str(source / "state.db")
    key = source / "signing.key"
    common = [server_bin, "-db-driver", driver, "-db", database, "-audit-key", str(key)]
    lab.command(common + ["-audit-verify"], env=env, name="i17-init-" + driver)
    token_file = independent / "collector-token"
    token_file.write_text("synthetic-i17-independent-collector-token", encoding="utf-8")
    token_file.chmod(0o600)
    collector_addr = "127.0.0.1:" + str(lab.free_port())
    collector_file = independent / "journal.jsonl"
    collector = lab.start_process("i17-collector-" + driver, [server_bin, "-addr", collector_addr,
        "-audit-collector-file", str(collector_file), "-audit-collector-public-key", str(key) + ".pub",
        "-audit-token-file", str(token_file), "-tls-cert", str(tls_dir / "lab.crt"),
        "-tls-key", str(tls_dir / "lab.key")], independent, env)
    effect = worker / "effect.txt"
    effect_command = "printf 'effect\\n' >> " + shlex.quote(str(effect))
    definitions = remote / "definitions/recovery"
    definitions.mkdir(parents=True)
    definition = {"id": "i17-effect", "team": "recovery", "jobType": "COMMAND", "confirm": True,
                  "agentId": "i17-worker", "schedule": {"enabled": True},
                  "conditionsOutAdd": ["i17-complete"], "params": {"command": effect_command}}
    (definitions / "effect.yaml").write_text(json.dumps(definition), encoding="utf-8")
    lab.command(["git", "init", "-b", "main", str(remote)], env=env)
    lab.command(["git", "-C", str(remote), "add", "."], env=env)
    lab.command(["git", "-C", str(remote), "-c", "user.name=Synthetic Lab", "-c",
                 "user.email=lab@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "recovery fixture"], env=env)
    git_sha = lab.command(["git", "-C", str(remote), "rev-parse", "HEAD"])
    address = "127.0.0.1:" + str(lab.free_port())
    base = "http://" + address

    def launch(target, cwd, signing_key, name, active=False):
        args = [server_bin, "-db-driver", driver, "-db", target, "-audit-key", str(signing_key),
                "-addr", address, "-workspace", str(cwd / "workspace"),
                "-git-source", str(remote), "-git-branch", "main", "-github-repo", "",
                "-git-poll-interval", "0", "-execution-mode", "durable", "-api-token", lab.ADMIN,
                "-auth-mode", "local", "-server-agent=false", "-selfmon=false", "-tick-ms", "200",
                "-scheduler", "internal" if active else "external", "-design-session-gc-tick-min", "0"]
        if active:
            args += ["-audit-siem-url", "https://" + collector_addr, "-audit-token-file", str(token_file)]
        proc = lab.start_process(name, args, cwd, env)
        lab.eventually(name, lambda: lab.request(base + "/health"))
        return proc

    def snapshot(label):
        if driver == "sqlite":
            path = root / (label + ".db")
            lab.command([server_bin, "-db", database, "-backup", str(path)], env=env,
                        name="i17-backup-" + driver + "-" + label)
            return str(path)
        path = "/tmp/i17-" + label + ".dump"
        lab.command(pg + ["pg_dump", "-U", "regente", "-d", dbname, "-Fc", "-f", path],
                    name="i17-backup-" + driver + "-" + label)
        return path

    def restore(snapshot_path, label):
        target_dir = root / label
        target_dir.mkdir()
        if driver == "sqlite":
            target = str(target_dir / "state.db")
            with open(snapshot_path, "rb") as src, open(target, "xb") as dst:
                shutil.copyfileobj(src, dst)
        else:
            name = "regente_i17_" + label
            lab.command(pg + ["createdb", "-U", "regente", name])
            lab.command(pg + ["pg_restore", "-U", "regente", "-d", name,
                              "--exit-on-error", "--single-transaction", "--no-owner", snapshot_path],
                        name="i17-restore-" + label)
            target = dsn.replace("/regente_lab?", "/" + name + "?")
        return target_dir, target

    server = launch(database, source, key, "i17-source-" + driver, active=True)
    issued = lab.request(base + "/api/agents/tokens", "POST", {"agentId": "i17-worker", "environment": "",
        "capabilities": ["COMMAND", "EXECUTION_V2"],
        "expiresAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + 3600))})
    config = source / "configuration.json"
    config.write_text(json.dumps({"executionMode": "durable", "agentId": "i17-worker", "agentToken": issued["token"],
        "definitionCommit": git_sha, "gitSource": str(remote), "serverAgent": False}), encoding="utf-8")
    config.chmod(0o600)
    lab.request(base + "/api/settings", "PUT", {"env_label": "i17-synthetic-recovery"})
    session = lab.request(base + "/api/design/sessions", "POST", {"folders": ["recovery"]})
    draft_endpoint = "/api/design/sessions/" + session["id"]
    draft = dict(definition, id="unpublished-i17", label="Unpublished recovery fixture", schedule={"enabled": False})
    lab.request(base + draft_endpoint + "/definitions", "POST", draft,
                extra_headers={"If-Match": '"' + str(session["revision"]) + '"'})
    draft_expected = lab.request(base + draft_endpoint + "/definitions")
    rows = lab.eventually("i17 daily", lambda: lab.request(base + "/api/instances"))
    instance = next(row["id"] for row in rows if row["definitionId"] == "i17-effect")
    lab.eventually("i17 initial ACK", lambda: lab.request(base + "/api/audit/security/status")["pending"] == 0)
    lab.stop_process(server)
    old_snapshot = snapshot("before_effect")
    old_snapshot_at = time.time()
    server = launch(database, source, key, "i17-source-resumed-" + driver, active=True)
    agent = lab.start_process("i17-agent-" + driver, [agent_bin, "-transport", "v2", "-server", base,
        "-id", "i17-worker", "-caps", "COMMAND", "-token", issued["token"],
        "-journal", str(worker / "journal.db"), "-concurrency", "1"], worker, env)
    lab.request(base + "/api/instances/" + instance + "/confirm", "POST")
    lab.eventually("i17 effect completed", lambda: lab.request(base + "/api/instances/" + instance)["status"] == "OK")
    attempts = lab.request(base + "/api/instances/" + instance + "/executions")["attempts"]
    if len(attempts) != 1 or effect.read_text().splitlines() != ["effect"]:
        raise RuntimeError("Synthetic source did not complete exactly one effect")
    execution_id = attempts[0]["executionId"]
    lab.eventually("i17 output condition", lambda: any(c["name"] == "i17-complete" for c in lab.request(base + "/api/conditions")))
    # Wait for the real journal receipt, not only the server's terminal state.
    def journal_confirmed():
        with closing(sqlite3.connect(worker / "journal.db")) as db:
            row = db.execute("SELECT state FROM journal_entries WHERE execution_id=?", (execution_id,)).fetchone()
            return row and row[0] == "acked"
    lab.eventually("i17 journal receipt", journal_confirmed)
    lab.stop_process(agent)
    lab.eventually("i17 final ACK", lambda: lab.request(base + "/api/audit/security/status")["pending"] == 0)
    lab.stop_process(server)
    lab.stop_process(collector)
    current_snapshot = snapshot("after_effect")
    current_snapshot_at = time.time()
    records = [json.loads(line) for line in collector_file.read_text().splitlines()]
    if not records or [r["seq"] for r in records] != list(range(1, len(records) + 1)):
        raise RuntimeError("Independent collector has a gap or duplicate")
    checkpoint = independent / "checkpoint.json"
    checkpoint.write_text(json.dumps({k: records[-1][k] for k in ("stream", "seq", "hash")}), encoding="utf-8")
    checkpoint_hash = sha(checkpoint)
    old_dir, old_db = restore(old_snapshot, "old_target")
    old_args = [server_bin, "-db-driver", driver, "-db", old_db, "-audit-key", str(key), "-audit-verify"]
    lab.command(old_args, env=env, name="i17-old-local-valid-" + driver)
    rejected = lab.command(old_args + ["-audit-checkpoint", str(checkpoint)], env=env,
                           expected_exit=1, name="i17-old-checkpoint-rejected-" + driver)
    if "checkpoint" not in rejected.lower():
        raise RuntimeError("Old snapshot failed for an unrelated reason")
    recovery_start = time.monotonic()
    restored, target = restore(current_snapshot, "current_target")
    recovered_key = restored / "signing.key"
    shutil.copy2(key, recovered_key)
    shutil.copytree(source / "workspace", restored / "workspace")
    # No sessions cache is copied: unpublished content must come from the DB.
    restored_journal = restored / "agent-journal.db"
    sqlite_snapshot(worker / "journal.db", restored_journal)
    shutil.copy2(config, restored / "configuration.json")
    common_restored = [server_bin, "-db-driver", driver, "-db", target, "-audit-key", str(recovered_key)]
    lab.command(common_restored + ["-migrate-only"], env=env, name="i17-restored-schema-" + driver)
    lab.command(common_restored + ["-audit-verify", "-audit-checkpoint", str(checkpoint)], env=env,
                name="i17-current-checkpoint-" + driver)
    missing = lab.command([server_bin, "-db-driver", driver, "-db", target, "-audit-key", str(restored / "absent.key"),
        "-audit-verify"], env=env, expected_exit=1, name="i17-missing-key-" + driver)
    if "audit" not in missing.lower():
        raise RuntimeError("Missing key failed for an unrelated reason")
    server = launch(target, restored, recovered_key, "i17-restored-" + driver)
    restored_attempts = lab.request(base + "/api/instances/" + instance + "/executions")["attempts"]
    with closing(sqlite3.connect(restored_journal)) as journal:
        entry = journal.execute("SELECT envelope,state FROM journal_entries WHERE execution_id=?", (execution_id,)).fetchone()
        identity = journal.execute("SELECT agent_id FROM journal_meta").fetchone()
    report = {
        "backend": driver, "older_snapshot_locally_valid": True, "older_snapshot_checkpoint_rejected": True,
        "current_checkpoint_verified": True, "missing_key_rejected": True,
        "draft_preserved": lab.request(base + draft_endpoint + "/definitions") == draft_expected and lab.request(base + draft_endpoint)["dirty"] is True,
        "definition_commit_preserved": lab.command(["git", "-C", str(restored / "workspace"), "rev-parse", "HEAD"]) == git_sha,
        "execution_identity_preserved": len(restored_attempts) == 1 and restored_attempts[0]["executionId"] == execution_id,
        "agent_journal_preserved": bool(entry and entry[1] == "acked" and json.loads(entry[0])["executionId"] == execution_id and identity == ("i17-worker",)),
        "effect_not_repeated": effect.read_text().splitlines() == ["effect"], "effect_count": len(effect.read_text().splitlines()),
        "configuration_preserved": sha(key) == sha(recovered_key) and sha(config) == sha(restored / "configuration.json") and lab.request(base + "/api/settings")["env_label"] == "i17-synthetic-recovery",
        "condition_preserved": any(c["name"] == "i17-complete" for c in lab.request(base + "/api/conditions")),
        "snapshot_committed_order_loss": 0 if lab.request(base + "/api/instances/" + instance)["status"] == "OK" else 1,
        "restore_and_inspection_seconds": round(time.monotonic() - recovery_start, 3),
        "older_snapshot_age_at_final_snapshot_seconds": round(current_snapshot_at - old_snapshot_at, 3),
        "post_old_snapshot_effects": 1, "checkpoint_sha256": checkpoint_hash,
        "synthetic_definition_commit": git_sha, "server_binary_sha256": sha(server_bin), "agent_binary_sha256": sha(agent_bin),
        "operational_acceptance": "pending-independent-operator-and-pilot",
        "limits": ["one synthetic reversible local file effect; no production workload",
                   "restore timer excludes detection, provisioning and human reconciliation",
                   "dispatch isolation is procedural: no agents, callers or external trigger; external scheduler alone is not a lock",
                   "collector is a separate process on the same lab host; independent custody is not qualified",
                   "current snapshot loses zero committed fixture orders; not a disaster RPO SLA"],
        "seconds": round(time.monotonic() - started, 3)}
    validate(report)
    lab.stop_process(server)
    if sha(checkpoint) != checkpoint_hash or effect.read_text().splitlines() != ["effect"]:
        raise RuntimeError("Recovery modified independent evidence")
    (lab.EVIDENCE / ("i17-recovery-" + driver + ".json")).write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    lab.REPORT.setdefault("i17_recovery", {})[driver] = report
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server-bin", type=Path, required=True)
    parser.add_argument("--agent-bin", type=Path, required=True)
    parser.add_argument("--openssl", default="openssl")
    args = parser.parse_args()
    lab.EVIDENCE.mkdir(parents=True)
    lab.REPORT.update(profile="i17-linux-sqlite-recovery", platform=platform.platform())
    started = time.monotonic()
    try:
        if platform.system() != "Linux":
            raise RuntimeError("I17 rehearsal requires Linux; no trust-store changes or TLS bypass are performed")
        lab.REPORT["sha"] = lab.command(["git", "rev-parse", "HEAD"])
        lab.REPORT["dirty"] = bool(lab.command(["git", "status", "--porcelain"]))
        tls_dir = lab.RUN / "tls"
        tls_dir.mkdir()
        lab.command([args.openssl, "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=regente-lab",
            "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1", "-keyout", str(tls_dir / "lab.key"),
            "-out", str(tls_dir / "lab.crt")], name="i17-certificate")
        env = {k: v for k, v in os.environ.items() if not k.startswith(("REGENTE_", "OTEL_", "GIT_", "AWS_"))
               and k not in ("GH_TOKEN", "GITHUB_TOKEN", "SSH_AUTH_SOCK")}
        env.update(SSL_CERT_FILE=str(tls_dir / "lab.crt"), GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull)
        lab.TLS_CONTEXT = ssl.create_default_context(cafile=str(tls_dir / "lab.crt"))
        rehearse(env, tls_dir, server_bin=args.server_bin.resolve(), agent_bin=args.agent_bin.resolve())
        lab.REPORT["status"] = "passed"
    except Exception as error:
        lab.REPORT.update(status="failed", error=str(error))
        print(str(error), file=sys.stderr)
    finally:
        for proc, _ in list(lab.PROCESSES):
            try:
                lab.stop_process(proc)
            except Exception as error:
                lab.REPORT.update(status="failed", cleanup_error=str(error))
        lab.REPORT["seconds"] = round(time.monotonic() - started, 3)
        (lab.EVIDENCE / "i17-native.json").write_text(json.dumps(lab.REPORT, indent=2) + "\n", encoding="utf-8")
        print("Evidence: " + str(lab.EVIDENCE))
    return 0 if lab.REPORT["status"] == "passed" else 1


if __name__ == "__main__":
    sys.exit(main())
