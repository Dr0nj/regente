#!/usr/bin/env python3
"""Offline admission check for trusted server/agent binaries and a restored schema.
Does not open a DB, start services or certify a rolling upgrade/recovery set.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def check(server, agent, schema):
    errors = []
    if server.get("component") != "server" or agent.get("component") != "agent":
        errors.append("invalid component descriptor")
    if not server.get("build") or server.get("build") != agent.get("build"):
        errors.append("mixed or unknown builds are not qualified")
    if server.get("protocol") != 2 or agent.get("protocol") != server.get("protocol"):
        errors.append("durable protocol mismatch")
    if server.get("journal") != 1 or agent.get("journal") != server.get("journal"):
        errors.append("journal mismatch")
    lo, hi = server.get("schemaMin"), server.get("schemaMax")
    if type(lo) is not int or type(hi) is not int or not lo <= schema <= hi:
        errors.append("schema outside runtime range; migrate a restored copy first")
    return errors


def inspect(binary):
    path = Path(binary).resolve(strict=True)
    value = json.loads(subprocess.check_output([str(path), "-compatibility"], timeout=10, text=True))
    return value, hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server", required=True)
    parser.add_argument("--agent", required=True)
    parser.add_argument("--schema", type=int, required=True, help="Verified schema of the isolated migrated/restore target")
    args = parser.parse_args()
    try:
        server, sh = inspect(args.server)
        agent, ah = inspect(args.agent)
        errors = check(server, agent, args.schema)
        print(json.dumps({"status": "rejected" if errors else "compatible", "server": server, "agent": agent,
                          "schema": args.schema, "serverSHA256": sh, "agentSHA256": ah, "errors": errors,
                          "scope": "offline same-build runtime admission; matching labels require trusted artifacts; no rolling qualification"}, indent=2))
        return bool(errors)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        parser.exit(1, "Compatibility inspection failed: " + str(error) + "\n")

if __name__ == "__main__":
    raise SystemExit(main())
