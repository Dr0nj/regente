#!/usr/bin/env python3
"""Verification profiles with explicit coverage and fail-closed completion."""
import argparse
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
FULL = ("runner", "server", "agent", "web", "docs", "browser", "integration", "demo")
QUICK = ("runner", "server", "agent", "web", "docs")
OMITTED_QUICK = ("staticcheck (both Go modules)", "clean npm install",
                 "real browser", "PostgreSQL/NATS/OIDC and recovery",
                 "PowerShell/Docker demo")
CI_EXTRA = ("other Node minima", "native Windows PowerShell demo")
REQUIRED_BROWSER = {"local: login policy and browser session",
                    "hybrid: login policy and browser session",
                    "oidc: login policy and browser session",
                    "I04: scoped frames and automatic reconnect after ACL change",
                    "I06: frozen business timezone survives settings changes in Monitoring"}


def plan(mode):
    return FULL if mode == "full" else QUICK


def validate_completion(required, results):
    ids = [r["gate"] for r in results]
    if ids != list(required) or any(r["status"] != "passed" for r in results):
        raise RuntimeError("Required gate missing, duplicated, out of order or not passed")


def validate_browser(report):
    stats = report.get("stats", {})
    if stats.get("expected", 0) < len(REQUIRED_BROWSER) or any(
            stats.get(key, 0) for key in ("skipped", "unexpected", "flaky")):
        raise RuntimeError("Browser evidence is incomplete, failed, flaky or skipped")
    passed = set()

    def walk(suites):
        for suite in suites:
            for spec in suite.get("specs", []):
                tests = spec.get("tests", [])
                if tests and all(test.get("status") == "expected"
                                 and test.get("results")
                                 and test["results"][-1].get("status") == "passed"
                                 for test in tests):
                    passed.add(spec["title"])
            walk(suite.get("suites", []))
    walk(report.get("suites", []))
    if not REQUIRED_BROWSER <= passed:
        raise RuntimeError("Missing mandatory browser scenarios: " +
                           ", ".join(sorted(REQUIRED_BROWSER - passed)))


def validate_demo(report, git_fixture):
    keys = ("authenticatedPresence", "commandCompleted", "humanTokenRejected",
            "mismatchedClaimsRejected", "revokedTokenRejected")
    if (report.get("status") != "passed" or any(report.get(k) is not True for k in keys)
            or report.get("gitFixture") is not git_fixture
            or report.get("nativeSmoke") is not False or report.get("publicTunnel") is not False):
        raise RuntimeError("Missing or failed mandatory demo evidence")


def clean_env():
    return {k: v for k, v in os.environ.items()
            if not k.startswith(("REGENTE_", "VITE_", "OTEL_", "PLAYWRIGHT_"))
            and k not in ("GH_TOKEN", "GITHUB_TOKEN")}


class Verifier:
    def __init__(self, mode, gates=None):
        self.mode = mode
        self.gates = tuple(plan(mode) if gates is None else gates)
        self.output = ROOT / ".integration" / ("verify-" + uuid.uuid4().hex[:12]) / "evidence"
        self.output.mkdir(parents=True)
        self.env = clean_env()
        self.report = {"mode": mode, "status": "running", "platform": platform.platform(),
                       "required_gates": list(plan(mode)), "gates": [],
                       "omitted": list(OMITTED_QUICK) if mode == "quick" else [],
                       "ci_matrix_additions": list(CI_EXTRA),
                       "release_install_smoke": "separate release workflow, not part of verification"}
        self.sequence = 0

    def command(self, args, cwd=ROOT, env=None, timeout=600):
        self.sequence += 1
        log = self.output / f"{self.sequence:02d}-{Path(args[0]).stem}.log"
        actual = list(map(str, args))
        # npm/npx são .cmd no Windows; resolver o executável sem shell/concatenação.
        actual[0] = shutil.which(actual[0]) or actual[0]
        print("+ " + " ".join(map(str, args)), flush=True)
        with log.open("w", encoding="utf-8") as stream:
            result = subprocess.run(actual, cwd=cwd, env=dict(self.env, **(env or {})),
                                    stdout=stream, stderr=subprocess.STDOUT,
                                    text=True, timeout=timeout)
        if result.returncode:
            print(log.read_text(encoding="utf-8", errors="replace")[-6000:], file=sys.stderr)
            raise RuntimeError(f"Command failed ({result.returncode}); log: {log.name}")
        return log

    def preflight(self):
        if self.mode == "full" and (platform.system() != "Linux"
                                   or platform.machine() not in ("x86_64", "amd64")):
            raise RuntimeError("Full profile requires Linux/amd64; use CI or a Linux VM. No gates skipped.")
        needed = ["go", "node", "npm", "git"]
        if self.mode == "full":
            needed += ["npx", "docker", "openssl", "pwsh", "bash", "tar"]
        for name in needed:
            if not shutil.which(name):
                raise RuntimeError("Missing mandatory dependency: " + name)
        self.command(["go", "version"])
        self.command(["node", "--version"])
        self.command(["git", "rev-parse", "HEAD"])
        if self.mode == "quick" and not (ROOT / "app/node_modules").is_dir():
            raise RuntimeError("Quick requires installed app dependencies: cd app && npm ci")
        if self.mode == "full":
            for name in (".env", ".env.local", ".env.production", ".env.production.local"):
                if (ROOT / "app" / name).exists():
                    raise RuntimeError("Full requires a clean app environment; found app/" + name)
            self.command(["docker", "compose", "version"])
            self.command(["docker", "info"])
        self.report["sha"] = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=ROOT, env=self.env, text=True).strip()
        self.report["dirty"] = bool(subprocess.check_output(
            ["git", "status", "--porcelain"], cwd=ROOT, env=self.env, text=True).strip())

    def gate(self, name):
        if name == "runner":
            self.command([sys.executable, "-m", "unittest", "discover", "-s", "scripts",
                          "-p", "test_verify.py", "-v"])
        elif name in ("server", "agent"):
            cwd = ROOT / name
            self.command(["go", "build", "./..."], cwd)
            self.command(["go", "vet", "./..."], cwd)
            if self.mode == "full":
                self.command(["go", "run", "honnef.co/go/tools/cmd/staticcheck@2025.1.1", "./..."], cwd)
            self.command(["go", "test", "-count=1", "./..."], cwd)
        elif name == "web":
            if self.mode == "full":
                self.command(["npm", "ci", "--engine-strict"], ROOT / "app")
            self.command(["npm", "run", "lint"], ROOT / "app")
            self.command(["npm", "run", "build"], ROOT / "app", {"VITE_REGENTE_SERVER_URL": "@origin"})
        elif name == "docs":
            self.command(["node", "scripts/check-doc-recipes.cjs"])
            self.command(["go", "test", "./cmd/docsite", "-count=1"], ROOT / "server")
            self.command(["go", "run", "./cmd/docsite", "-repo", "..",
                          "-out", "../docs/site", "-check"], ROOT / "server")
        elif name == "browser":
            # A instalação explícita do browser/deps é pré-requisito documentado.
            self.command(["node", "-e",
                          "const fs=require('node:fs');const p=require('@playwright/test').chromium.executablePath();"
                          "if(!fs.existsSync(p)){console.error('Run: npx playwright install --with-deps chromium');process.exit(1)}"],
                         ROOT / "app")
            self.command(["go", "-C", "server", "build", "-o", "../.integration/server-i03", "."])
            report = self.output / "browser.json"
            self.command(["npx", "--no-install", "playwright", "test", "--reporter=json"],
                         ROOT / "app", {"PLAYWRIGHT_JSON_OUTPUT_FILE": str(report)})
            validate_browser(json.loads(report.read_text(encoding="utf-8")))
        elif name == "integration":
            self.command([sys.executable, "-m", "unittest", "discover", "-s", "scripts",
                          "-p", "test_integration_runner.py"])
            log = self.command([sys.executable, "scripts/integration.py"], timeout=1200)
            paths = [line.removeprefix("Evidence: ") for line in
                     log.read_text(encoding="utf-8").splitlines() if line.startswith("Evidence: ")]
            if not paths:
                raise RuntimeError("Integration exited without mandatory evidence")
            evidence = Path(paths[-1]).resolve()
            if not evidence.is_relative_to((ROOT / ".integration").resolve()):
                raise RuntimeError("Unexpected integration evidence location")
            result = json.loads((evidence / "baseline.json").read_text(encoding="utf-8"))
            if result.get("status") != "passed" or result.get("sha") != self.report["sha"]:
                raise RuntimeError("Integration evidence failed or belongs to another revision")
            self.report["integration_evidence"] = str(evidence.relative_to(ROOT))
        elif name == "demo":
            self.command(["pwsh", "-NoProfile", "-File", "scripts/test-demo-guards.ps1"])
            for git_fixture in (False, True):
                report = self.output / ("demo-git.json" if git_fixture else "demo-offline.json")
                args = ["pwsh", "-NoProfile", "-File", "deploy/demo/host-demo.ps1",
                        "-Smoke", "-Port", "0", "-EvidencePath", str(report)]
                if git_fixture:
                    args.append("-GitFixture")
                self.command(args, timeout=900)
                validate_demo(json.loads(report.read_text(encoding="utf-8-sig")), git_fixture)
        else:
            raise RuntimeError("Unknown required gate: " + name)

    def execute(self):
        started = time.monotonic()
        try:
            self.preflight()
            for name in self.gates:
                print(f"GATE {name}", flush=True)
                entry = {"gate": name, "status": "running"}
                self.report["gates"].append(entry)
                try:
                    self.gate(name)
                except Exception:
                    entry["status"] = "failed"
                    raise
                entry["status"] = "passed"
            validate_completion(plan(self.mode), self.report["gates"])
            self.report["status"] = "passed"
        except Exception as error:
            self.report["status"] = "failed"
            self.report["error"] = str(error)
            print("FAIL: " + str(error), file=sys.stderr, flush=True)
        finally:
            self.report["seconds"] = round(time.monotonic() - started, 3)
            (self.output / "verify.json").write_text(json.dumps(self.report, indent=2) + "\n", encoding="utf-8")
            print("Evidence: " + str(self.output), flush=True)
        if self.report["status"] != "passed":
            return 1
        print(f"PASS: {self.mode} profile; all declared gates passed.", flush=True)
        if self.mode == "quick":
            print("NOT RUN: " + "; ".join(OMITTED_QUICK), flush=True)
        print("Additional CI matrix: " + "; ".join(CI_EXTRA), flush=True)
        print("Release/install smoke remains a separate workflow; no production qualification.", flush=True)
        return 0


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--quick", action="store_true", help="Fast local checks; explicitly omits external gates (default)")
    modes.add_argument("--full", action="store_true", help="Mandatory Linux/amd64 verification with real dependencies")
    parser.add_argument("--plan", action="store_true", help="Print coverage without running checks (not a pass)")
    args = parser.parse_args(argv)
    mode = "full" if args.full else "quick"
    if args.plan:
        print(json.dumps({"mode": mode, "status": "not_run", "gates": plan(mode),
                          "omitted": OMITTED_QUICK if mode == "quick" else [],
                          "additional_ci_matrix": CI_EXTRA,
                          "release": "separate workflow"}, indent=2))
        return 0
    return Verifier(mode).execute()


if __name__ == "__main__":
    sys.exit(main())
