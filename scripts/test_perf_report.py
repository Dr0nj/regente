import json
from pathlib import Path
import tempfile
import unittest

from perf_report import summarize, summarize_samples


class EvidenceSummaryTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.path = self.root / 'day-samples.jsonl'

    def sample(self, seconds, cpu, exported=0):
        return {'seconds': seconds, 'serverCPUSeconds': cpu, 'serverRSS': 100,
                'collectorRSS': 40, 'metrics': {'regente_audit_exported_records_total': exported},
                'hostCPUJiffies': {k: seconds for k in ('user', 'nice', 'system', 'idle', 'iowait', 'irq', 'softirq', 'steal', 'guest', 'guest_nice')}}

    def write(self, *samples):
        self.path.write_text(''.join(json.dumps(s) + '\n' for s in samples), encoding='utf-8')

    def test_partial_append_does_not_turn_into_pass(self):
        self.write(self.sample(0, 10), self.sample(5, 11, 20))
        with self.path.open('ab') as stream:
            stream.write(b'{"seconds":')
        result = summarize(self.root)
        self.assertEqual(result['state'], 'no_final_report')
        self.assertFalse(result['qualificationGrantedByAnalyzer'])
        profile = result['profiles'][0]
        self.assertTrue(profile['partialTrailingLine'])
        self.assertEqual(profile['serverAverageCores'], .2)
        self.assertEqual(profile['hostStealPercent'], 12.5)
        self.assertEqual(profile['counters']['regente_audit_exported_records_total']['perSecond'], 4)

    def test_io_peak_reports_interval_and_related_backlog_without_guest_double_count(self):
        first, last = self.sample(0, 1), self.sample(5, 2)
        first['hostCPUJiffies'] = {k: 0 for k in first['hostCPUJiffies']}
        last['hostCPUJiffies'] = dict(first['hostCPUJiffies'], user=10, idle=40, iowait=50, guest=10)
        last['metrics'].update(regente_execution_oldest_outbox_seconds=6.378, regente_scheduler_completed_tick_age_seconds=6.86)
        self.write(first, last)
        self.assertEqual(summarize_samples(self.path)['peakHostIOWaitInterval'], {
            'percent': 50, 'fromSeconds': 0, 'toSeconds': 5,
            'oldestOutboxSeconds': 6.378, 'completedTickAgeSeconds': 6.86})
        last['hostCPUJiffies']['iowait'] = -1
        self.write(first, last)
        with self.assertRaises(ValueError): summarize_samples(self.path)

    def test_io_counter_reset_does_not_create_a_negative_peak(self):
        first, last = self.sample(0, 1), self.sample(5, 2)
        first['hostCPUJiffies']['iowait'] = 100
        self.write(first, last)
        self.assertIsNone(summarize_samples(self.path)['peakHostIOWaitInterval'])

    def test_complete_corruption_is_not_ignored(self):
        self.path.write_bytes(b'{broken}\n')
        with self.assertRaises(ValueError):
            summarize_samples(self.path)

    def test_resets_do_not_produce_negative_or_false_rates(self):
        self.write(self.sample(0, 10, 100), self.sample(5, 2, 5))
        result = summarize_samples(self.path)
        self.assertEqual(result['serverCPUCounterResets'], 1)
        self.assertIsNone(result['serverAverageCores'])
        counter = result['counters']['regente_audit_exported_records_total']
        self.assertEqual(counter['resets'], 1)
        self.assertIsNone(counter['perSecond'])

    def test_missing_counter_and_sampling_gap_remain_visible(self):
        middle = self.sample(5, 11)
        middle['metrics'] = {}
        self.write(self.sample(0, 10, 2), middle, self.sample(90, 12, 4))
        result = summarize_samples(self.path)
        self.assertEqual(result['maxSampleGapSeconds'], 85)
        self.assertIsNone(result['counters']['regente_audit_exported_records_total']['perSecond'])

    def test_nonfinite_and_out_of_order_samples_fail(self):
        for second in (float('nan'), float('inf'), -1, 0):
            with self.subTest(second=second):
                self.write(self.sample(0, 1), self.sample(second, 2))
                with self.assertRaises(ValueError):
                    summarize_samples(self.path)

    def test_failed_final_report_is_preserved(self):
        self.write(self.sample(0, 10))
        (self.root / 'capacity.json').write_text(json.dumps({'status': 'failed', 'error': 'daily gate',
            'perfDay': {'qualified': False, 'measurementSeconds': 20}}), encoding='utf-8')
        result = summarize(self.root)
        self.assertEqual(result['runnerReport']['status'], 'failed')
        self.assertFalse(result['reportedOutcomes']['perfDay']['qualified'])
        self.assertFalse(result['qualificationGrantedByAnalyzer'])

    def test_isolated_tier_is_not_daily_qualification(self):
        self.write(self.sample(0, 1))
        (self.root / 'capacity.json').write_text(json.dumps({'status': 'passed',
            'tierRegression': {'qualified': True, 'measurementSeconds': 300}}), encoding='utf-8')
        result = summarize(self.root)
        self.assertTrue(result['reportedOutcomes']['tierRegression']['qualified'])
        self.assertNotIn('perfDay', result['reportedOutcomes'])
        self.assertFalse(result['qualificationGrantedByAnalyzer'])

    def test_initial_reconciliation_sentinel_is_not_zero_age(self):
        sample = self.sample(0, 1)
        name = 'regente_scheduler_reconciliation_age_seconds'
        sample['metrics'][name] = -1
        self.write(sample)
        result = summarize_samples(self.path)
        self.assertEqual(result['unavailableGaugeSamples'][name], 1)
        self.assertNotIn(name, result['gauges'])


if __name__ == '__main__':
    unittest.main()
