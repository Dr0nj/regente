import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import integration


class EvidenceGateTest(unittest.TestCase):
    def test_rejects_skip(self):
        with self.assertRaisesRegex(RuntimeError, "skipped"):
            integration.validate_test_events(json.dumps({"Action": "skip", "Package": "db", "Test": "pg"}))

    def test_rejects_empty_or_partial_success(self):
        for output in ("", json.dumps({"Action": "pass", "Package": "db", "Test": "TestMigrationSafety/sqlite"})):
            with self.assertRaisesRegex(RuntimeError, "Missing required"):
                integration.validate_test_events(output)


@unittest.skipIf(os.name == "nt", "Shell safety tests run in the mandatory Linux gate")
class RecoveryScriptsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("REGENTE_")}

    def run_script(self, name, *args, **env):
        shell = "bash" if name == "update.sh" else "sh"
        return subprocess.run([shell, str(integration.ROOT / "server/deploy" / name), *map(str, args)],
                              env=dict(self.env, **env), capture_output=True, text=True, timeout=10)

    def test_sqlite_restore_new_target_and_refuse_existing_or_sidecars(self):
        src = self.root / "snapshot.db"
        src.write_bytes(b"synthetic snapshot")
        target = self.root / "restored.db"
        result = self.run_script("restore.sh", src, REGENTE_DB=str(target))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(target.read_bytes(), src.read_bytes())
        for suffix in ("", "-wal", "-shm"):
            with self.subTest(suffix=suffix):
                dest = self.root / ("new" + suffix + ".db")
                protected = Path(str(dest) + suffix)
                protected.write_bytes(b"must survive")
                result = self.run_script("restore.sh", src, REGENTE_DB=str(dest))
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(protected.read_bytes(), b"must survive")
                if suffix:
                    self.assertFalse(dest.exists())

    def test_postgres_restore_flags_and_error_propagation(self):
        fake = self.root / "pg_restore"
        fake.write_text('#!/bin/sh\nprintf "%s\\n" "$@"\nexit 7\n')
        fake.chmod(0o700)
        src = self.root / "snapshot.dump"
        src.touch()
        result = self.run_script("restore.sh", src, REGENTE_DB="postgresql:///disposable",
                                 REGENTE_DB_DRIVER="postgres", PATH=str(self.root) + os.pathsep + self.env["PATH"])
        self.assertEqual(result.returncode, 7)
        self.assertIn("--exit-on-error", result.stdout)
        self.assertIn("--single-transaction", result.stdout)
        self.assertNotIn("--clean", result.stdout)

    def test_drain_requires_disposable_ack_and_identical_binaries(self):
        result = self.run_script("rolling-upgrade.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("REGENTE_DISPOSABLE_DB", result.stderr)
        old, new = self.root / "old", self.root / "new"
        old.write_text("#!/bin/sh\nexit 42\n")
        new.write_text("#!/bin/sh\nexit 43\n")
        old.chmod(0o700)
        new.chmod(0o700)
        result = self.run_script("rolling-upgrade.sh", REGENTE_DISPOSABLE_DB="1",
                                 REGENTE_OLD_BIN=str(old), REGENTE_NEW_BIN=str(new))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refusing mixed binaries", result.stderr)

    def test_update_help_does_not_promise_binary_only_rollback(self):
        result = self.run_script("update.sh", "--help")
        self.assertEqual(result.returncode, 0)
        self.assertIn("DATABASE-ONLY", result.stdout)
        self.assertIn("does NOT undo migrations", result.stdout)
        self.assertNotIn("downgrade included", result.stdout)

    def test_shell_syntax(self):
        for name in ("backup.sh", "restore.sh", "update.sh", "rolling-upgrade.sh"):
            result = subprocess.run(
                ["bash" if name == "update.sh" else "sh", "-n", str(integration.ROOT / "server/deploy" / name)],
                capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
