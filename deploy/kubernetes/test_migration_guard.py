import copy
import unittest
from migration_guard import require_drained, require_retirable


class MigrationGuardTests(unittest.TestCase):
    def setUp(self):
        self.inventory = dict(
            complete=True,
            producersFenced=True,
            jobs=["a"],
            definitions=[],
            instances=[
                dict(
                    id="old",
                    definitionId="a",
                    status="OK",
                    agentId="old",
                    pinnedAgent="old",
                    attempts=[dict(state="succeeded")],
                )
            ],
        )
        self.journal = dict(
            identity="old", stopped=True, pendingEntries=0, pendingChunks=0
        )

    def test_completed_batch_and_acknowledged_journal(self):
        require_retirable(self.inventory, ["a"], "old", self.journal)

    def test_every_nonterminal_state_blocks_cutover_and_rollback(self):
        for status in ("WAITING", "HELD", "RUNNING", "UNCERTAIN", "UNKNOWN"):
            with self.subTest(status=status), self.assertRaises(ValueError):
                self.inventory["instances"][0]["status"] = status
                require_drained(self.inventory, ["a"])

    def test_terminal_ui_does_not_hide_uncertain_attempt(self):
        self.inventory["instances"][0]["attempts"][0]["state"] = "uncertain"
        with self.assertRaises(ValueError):
            require_drained(self.inventory, ["a"])

    def test_incomplete_or_unfenced_inventory(self):
        for field in ("complete", "producersFenced"):
            value = copy.deepcopy(self.inventory)
            value[field] = False
            with self.assertRaises(ValueError):
                require_drained(value, ["a"])
        with self.assertRaises(ValueError):
            require_drained(self.inventory, ["absent"])

    def test_retirement_checks_other_jobs_and_old_pinned_snapshots(self):
        self.inventory["instances"].append(
            dict(
                id="other",
                definitionId="other",
                status="HELD",
                pinnedAgent="old",
                attempts=[],
            )
        )
        with self.assertRaises(ValueError):
            require_retirable(self.inventory, ["a"], "old", self.journal)

    def test_remaining_definition_blocks_retirement(self):
        self.inventory["definitions"] = [dict(id="other", agentId="old")]
        with self.assertRaises(ValueError):
            require_retirable(self.inventory, ["a"], "old", self.journal)

    def test_journal_receipts_and_stopped_owner_required(self):
        for field, value in (
            ("pendingEntries", 1),
            ("pendingChunks", 1),
            ("stopped", False),
            ("identity", "other"),
        ):
            journal = {**self.journal, field: value}
            with self.subTest(field=field), self.assertRaises(ValueError):
                require_retirable(self.inventory, ["a"], "old", journal)

    def test_unpinned_local_jobs_cannot_be_assumed_independent(self):
        self.inventory["definitions"] = [dict(id="any-agent", jobType="COMMAND")]
        with self.assertRaises(ValueError):
            require_retirable(self.inventory, ["a"], "old", self.journal)


if __name__ == "__main__":
    unittest.main()
