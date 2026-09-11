import json
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


if __name__ == "__main__":
    unittest.main()
