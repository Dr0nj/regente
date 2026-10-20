import copy
import unittest

from ci_release_gate import REQUIRED, validate


class CIReleaseGateTests(unittest.TestCase):
    def setUp(self):
        self.sha = "a" * 40
        self.repo = "Dr0nj/regente"
        self.run = dict(id=123, head_sha=self.sha, event="push", head_branch="main",
                        status="completed", conclusion="success",
                        path=".github/workflows/ci.yml", run_attempt=2,
                        repository={"full_name": self.repo},
                        head_repository={"full_name": self.repo})
        self.jobs = [dict(name=name, status="completed", conclusion="success")
                     for name in sorted(REQUIRED)]
        self.jobs.append(dict(name="verification / measured capacity and developer soak",
                              status="completed", conclusion="success"))

    def check(self, run=None, jobs=None):
        return validate(self.run if run is None else run,
                        self.jobs if jobs is None else jobs, self.repo, self.sha, 123)

    def test_exact_revision_and_all_gates(self):
        self.assertEqual(self.check()["sourceSha"], self.sha)
        self.assertEqual(self.check()["runAttempt"], 2)

    def test_capacity_skip_requires_successful_policy(self):
        self.jobs[-1]["conclusion"] = "skipped"
        self.assertEqual(self.check()["capacityResult"], "skipped")
        next(j for j in self.jobs if j["name"] == "capacity policy")["conclusion"] = "skipped"
        with self.assertRaises(ValueError):
            self.check()

    def test_foreign_revision_workflow_event_and_outcome_rejected(self):
        for key, value in (
            ("id", 456), ("head_sha", "b" * 40), ("event", "pull_request"),
            ("head_branch", "codex/feature"), ("status", "in_progress"),
            ("conclusion", "failure"), ("conclusion", "cancelled"),
            ("path", ".github/workflows/other.yml"),
            ("repository", {"full_name": "other/repo"}),
            ("head_repository", {"full_name": "other/repo"}),
        ):
            with self.subTest(key=key, value=value), self.assertRaises(ValueError):
                self.check(run=dict(self.run, **{key: value}))

    def test_skipped_missing_duplicate_failed_and_pending_jobs_rejected(self):
        for jobs in ([], self.jobs[:-1], self.jobs + self.jobs[:1]):
            with self.assertRaises(ValueError):
                self.check(jobs=jobs)
        for state in ("skipped", "failure", "cancelled", None):
            jobs = copy.deepcopy(self.jobs)
            jobs[0]["conclusion"] = state
            with self.assertRaises(ValueError):
                self.check(jobs=jobs)
        jobs = copy.deepcopy(self.jobs)
        jobs[0]["status"] = "in_progress"
        with self.assertRaises(ValueError):
            self.check(jobs=jobs)


if __name__ == "__main__":
    unittest.main()
