import subprocess
import tempfile
import unittest
from unittest import mock

from ci_scope import changed_paths, needs_capacity


class ScopeTests(unittest.TestCase):
    def test_non_runtime_changes_do_not_run_soak(self):
        self.assertFalse(needs_capacity([
            ".github/workflows/ci.yml", "README.md", "docs/site/index.html",
            "app/src/main.tsx", "scripts/ci_scope.py", "scripts/verify.py",
            "deploy/demo/host-demo.ps1"]))

    def test_runtime_workload_dependencies_and_unknown_paths_require_soak(self):
        for path in ("server/internal/scheduler/durable.go", "agent/main.go",
                     "server/go.sum", "go.work", "scripts/capacity.py",
                     "scripts/integration.py", "new-runtime/config.json"):
            with self.subTest(path=path):
                self.assertTrue(needs_capacity(["README.md", path]))

    def test_missing_base_is_not_an_empty_diff(self):
        for base in ("", "0" * 40):
            with self.assertRaises(ValueError):
                changed_paths(base, "a" * 40)

    def test_full_push_diff_and_unusual_filenames(self):
        # Both endpoints are passed to Git; do not inspect only the final commit.
        with mock.patch("ci_scope.subprocess.run", return_value=subprocess.CompletedProcess(
                [], 0, b"docs/a b.md\0agent/main.go\0")) as run:
            self.assertEqual(changed_paths("a" * 40, "b" * 40),
                             ["docs/a b.md", "agent/main.go"])
            self.assertEqual(run.call_args.args[0][-2:], ["a" * 40, "b" * 40])
            self.assertIn("--no-renames", run.call_args.args[0])


if __name__ == "__main__":
    unittest.main()
