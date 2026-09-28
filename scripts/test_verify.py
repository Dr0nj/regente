import contextlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import verify


class VerificationTests(unittest.TestCase):
    def quiet(self):
        return contextlib.redirect_stdout(io.StringIO())

    def test_completion_requires_every_gate_once_and_passed(self):
        good = [{"gate": gate, "status": "passed"} for gate in verify.FULL]
        verify.validate_completion(verify.FULL, good)
        bad = [good[:-1], good + good[:1], list(reversed(good))]
        for status in ("skipped", "running", "failed"):
            bad.append([dict(row, status=status) if i == 0 else row for i, row in enumerate(good)])
        for rows in bad:
            with self.assertRaisesRegex(RuntimeError, "Required gate"):
                verify.validate_completion(verify.FULL, rows)

    def test_executor_rejects_omitted_gate_even_when_other_gates_pass(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(verify, "ROOT", Path(tmp)):
            runner = verify.Verifier("full", verify.FULL[:-1])
            with mock.patch.object(runner, "preflight"), mock.patch.object(runner, "gate"), self.quiet(), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(runner.execute(), 1)
            report = json.loads((runner.output / "verify.json").read_text())
            self.assertEqual(report["status"], "failed")
            self.assertIn("Required gate", report["error"])

    def test_failure_stops_later_gates(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(verify, "ROOT", Path(tmp)):
            runner = verify.Verifier("quick")
            with mock.patch.object(runner, "preflight"), mock.patch.object(runner, "gate", side_effect=RuntimeError("fixture failed")) as gate, self.quiet(), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(runner.execute(), 1)
                self.assertEqual(gate.call_count, 1)
            self.assertEqual(runner.report["gates"], [{"gate": "runner", "status": "failed"}])

    def test_full_rejects_unsupported_host_and_missing_dependency(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(verify, "ROOT", Path(tmp)):
            runner = verify.Verifier("full")
            with mock.patch.object(verify.platform, "system", return_value="Windows"), mock.patch.object(runner, "command") as command:
                with self.assertRaisesRegex(RuntimeError, "Linux/amd64"):
                    runner.preflight()
                command.assert_not_called()
            with mock.patch.object(verify.platform, "system", return_value="Linux"), mock.patch.object(verify.platform, "machine", return_value="x86_64"), mock.patch.object(verify.shutil, "which", return_value=None), mock.patch.object(runner, "command") as command:
                with self.assertRaisesRegex(RuntimeError, "Missing mandatory dependency"):
                    runner.preflight()
                command.assert_not_called()

    def test_nonzero_command_is_failure(self):
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(verify, "ROOT", Path(tmp)):
            runner = verify.Verifier("quick")
            with mock.patch.object(verify.subprocess, "run", return_value=subprocess.CompletedProcess([], 7)), self.quiet(), contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaisesRegex(RuntimeError, r"Command failed \(7\)"):
                    runner.command(["fixture"], cwd=tmp)

    def test_plan_is_not_execution_or_success(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(verify.main(["--quick", "--plan"]), 0)
        report = json.loads(output.getvalue())
        self.assertEqual(report["status"], "not_run")
        self.assertNotIn("browser", report["gates"])
        self.assertIn("real browser", report["omitted"])

    def test_browser_requires_real_mandatory_scenarios(self):
        def fixture():
            return {"stats": {"expected": 4, "skipped": 0, "unexpected": 0, "flaky": 0},
                    "suites": [{"specs": [{"title": title, "tests": [{"status": "expected", "results": [{"status": "passed"}]}]} for title in verify.REQUIRED_BROWSER]}]}
        verify.validate_browser(fixture())
        for key in ("skipped", "unexpected", "flaky"):
            report = fixture()
            report["stats"][key] = 1
            with self.assertRaises(RuntimeError):
                verify.validate_browser(report)
        report = fixture()
        report["suites"][0]["specs"].pop()
        with self.assertRaisesRegex(RuntimeError, "Missing mandatory"):
            verify.validate_browser(report)
        with self.assertRaises(RuntimeError):
            verify.validate_browser({})

    def test_demo_requires_effects_and_isolation(self):
        report = {"status": "passed", "gitFixture": False, "nativeSmoke": False,
                  "publicTunnel": False, "authenticatedPresence": True,
                  "commandCompleted": True, "humanTokenRejected": True,
                  "mismatchedClaimsRejected": True, "revokedTokenRejected": True}
        verify.validate_demo(report, False)
        for key, value in (("commandCompleted", False), ("publicTunnel", True),
                           ("gitFixture", True), ("nativeSmoke", True), ("status", "skipped")):
            with self.assertRaises(RuntimeError):
                verify.validate_demo(dict(report, **{key: value}), False)
        with self.assertRaises(RuntimeError):
            verify.validate_demo({}, False)

    def test_environment_drops_inherited_product_credentials(self):
        with mock.patch.dict(verify.os.environ, {"PATH": "fixture", "REGENTE_TOKEN": "secret",
                             "VITE_REGENTE_SERVER_URL": "private", "OTEL_ENDPOINT": "private",
                             "PLAYWRIGHT_BASE_URL": "private", "GITHUB_TOKEN": "secret",
                             "GH_TOKEN": "secret"}, clear=True):
            self.assertEqual(verify.clean_env(), {"PATH": "fixture"})


if __name__ == "__main__":
    unittest.main()
