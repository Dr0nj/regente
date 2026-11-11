#!/usr/bin/env python3
"""Read-only, streaming summary of capacity evidence; never grants qualification."""
import argparse
import json
import math
from pathlib import Path
import sys


GAUGES = (
    'regente_audit_pending', 'regente_audit_oldest_pending_seconds',
    'regente_execution_oldest_eligible_wait_seconds',
    'regente_execution_oldest_outbox_seconds', 'regente_execution_uncertain',
    'regente_scheduler_candidate_queue', 'regente_scheduler_reconciliation_age_seconds',
    'regente_scheduler_completed_tick_age_seconds',
)
COUNTERS = (
    'regente_audit_exported_records_total',
    'regente_scheduler_candidate_evaluations_total',
    'regente_execution_receipt_replays_total',
    'regente_audit_admission_rejections_total',
    'regente_scheduler_ticks_total{outcome="completed"}',
    'regente_scheduler_ticks_total{outcome="failed"}',
)


def number(value, label):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value < 0:
        raise ValueError(f'Invalid nonnegative finite number: {label}')
    return value


def host_cpu_delta(before, after):
    keys = ('user', 'nice', 'system', 'idle', 'iowait', 'irq', 'softirq', 'steal')
    if not all(k in before and k in after for k in keys):
        return None
    delta = {k: number(after[k], k) - number(before[k], k) for k in keys}
    if any(v < 0 for v in delta.values()) or not sum(delta.values()):
        return None
    total = sum(delta.values())
    return {k: delta[k] / total * 100 for k in ('iowait', 'steal')}


def summarize_samples(path):
    # Limita a leitura ao tamanho observado: append concorrente não prolonga a
    # análise indefinidamente. Só a última linha incompleta pode ser ignorada.
    limit = path.stat().st_size
    first = previous = last = None
    count = 0
    partial = False
    max_gap = 0
    cpu_delta = 0
    cpu_resets = 0
    io_peak = None
    peaks = {'serverRSS': 0, 'collectorRSS': 0}
    gauges = {}
    unavailable = {}
    counters = {}
    with path.open('rb') as stream:
        while stream.tell() < limit:
            line = stream.readline(limit - stream.tell())
            if not line.endswith(b'\n'):
                partial = True
                break
            sample = json.loads(line)
            seconds = number(sample['seconds'], 'seconds')
            cpu = number(sample['serverCPUSeconds'], 'serverCPUSeconds')
            metrics = sample['metrics']
            if not isinstance(metrics, dict):
                raise ValueError('Metrics must be an object')
            for name in peaks:
                peaks[name] = max(peaks[name], number(sample[name], name))
            if previous is not None:
                gap = seconds - previous['seconds']
                if gap <= 0:
                    raise ValueError('Sample timestamps must increase strictly')
                max_gap = max(max_gap, gap)
                host_delta = host_cpu_delta(previous.get('hostCPUJiffies', {}), sample.get('hostCPUJiffies', {}))
                if host_delta is not None and (io_peak is None or host_delta['iowait'] > io_peak['percent']):
                    io_peak = {'percent': host_delta['iowait'], 'fromSeconds': previous['seconds'], 'toSeconds': seconds,
                               'oldestOutboxSeconds': metrics.get('regente_execution_oldest_outbox_seconds'),
                               'completedTickAgeSeconds': metrics.get('regente_scheduler_completed_tick_age_seconds')}

                delta = cpu - previous['serverCPUSeconds']
                cpu_resets += delta < 0
                if delta >= 0:
                    cpu_delta += delta
            for name in GAUGES:
                if name not in metrics:
                    continue
                if name in ('regente_scheduler_reconciliation_age_seconds', 'regente_scheduler_completed_tick_age_seconds') and metrics[name] == -1:
                    unavailable[name] = unavailable.get(name, 0) + 1
                    continue
                value = number(metrics[name], name)
                entry = gauges.setdefault(name, {'first': value, 'last': value, 'max': value, 'observations': 0})
                entry.update(last=value, max=max(entry['max'], value), observations=entry['observations'] + 1)
            for name in COUNTERS:
                if name not in metrics:
                    continue
                value = number(metrics[name], name)
                entry = counters.setdefault(name, {'first': value, 'last': value, 'delta': 0, 'resets': 0, 'observations': 0})
                if entry['observations']:
                    delta = value - entry['last']
                    entry['resets'] += delta < 0
                    if delta >= 0:
                        entry['delta'] += delta
                entry.update(last=value, observations=entry['observations'] + 1)
            first = sample if first is None else first
            previous = last = sample
            count += 1
    if not count:
        return {'file': path.name, 'samples': 0, 'partialTrailingLine': partial, 'state': 'no_complete_samples'}
    elapsed = last['seconds'] - first['seconds']
    for entry in counters.values():
        entry['perSecond'] = entry['delta'] / elapsed if elapsed and not entry['resets'] and entry['observations'] == count else None
    # Linux guest/guest_nice já estão incluídos em user/nice, não somar duas vezes.
    steal = None
    keys = ('user', 'nice', 'system', 'idle', 'iowait', 'irq', 'softirq', 'steal')
    if all(k in first.get('hostCPUJiffies', {}) and k in last.get('hostCPUJiffies', {}) for k in keys):
        deltas = [number(last['hostCPUJiffies'][k], k) - number(first['hostCPUJiffies'][k], k) for k in keys]
        if all(d >= 0 for d in deltas) and sum(deltas):
            steal = deltas[-1] / sum(deltas) * 100
    return {
        'file': path.name, 'samples': count, 'partialTrailingLine': partial,
        'state': 'samples_only', 'firstSeconds': first['seconds'], 'lastSeconds': last['seconds'],
        'observedIntervalSeconds': elapsed, 'maxSampleGapSeconds': max_gap,
        'serverCPUDeltaSeconds': cpu_delta, 'serverCPUCounterResets': cpu_resets,
        'serverAverageCores': cpu_delta / elapsed if elapsed and not cpu_resets else None,
        'hostStealPercent': steal, 'peakHostIOWaitInterval': io_peak, 'peakBytes': peaks, 'gauges': gauges,
        'unavailableGaugeSamples': unavailable, 'counters': counters,
    }


def summarize(evidence):
    files = sorted(evidence.glob('*-samples.jsonl'))
    if not files:
        raise ValueError('No sample journals found')
    manifest = evidence / 'capacity.json'
    reported = json.loads(manifest.read_text(encoding='utf-8')) if manifest.exists() else None
    result = {
        'evidence': str(evidence.resolve()), 'qualificationGrantedByAnalyzer': False,
        'state': 'no_final_report' if reported is None else 'final_report_present',
        'profiles': [summarize_samples(p) for p in files],
        'limits': ['Samples include injected faults; this summary does not exclude fault windows.',
                   'No final report may mean active, interrupted, or failed before report creation.',
                   'CPU uses sampled process-counter deltas, not the runner report lifetime total.',
                   'Host steal includes other host workloads; database CPU is not server CPU.',
                   'Peak and gap observations do not prove daily correctness or capacity.'],
    }
    if reported is not None:
        result['runnerReport'] = {k: reported.get(k) for k in (
            'status', 'error', 'sha', 'dirty', 'profile', 'perfDayRequired',
            'supportedEnvelope', 'postgresStorage', 'postgresContainer', 'cgroupLimits', 'stopReason')}
        outcomes = {}
        for name, outcome in reported.get('comparison', {}).items():
            outcomes[name] = outcome
        for outcome in reported.get('tiers', []):
            outcomes[outcome['name']] = outcome
        for name in ('perfDay', 'perfCheck', 'soak'):
            if name in reported:
                outcomes[name] = reported[name]
        result['reportedOutcomes'] = {name: {k: outcome.get(k) for k in (
            'qualified', 'measurementSeconds', 'admissionStop', 'agentSlots',
            'admittedInstances', 'attempts', 'distinctGraphDefinitionsExecuted',
            'latencies', 'correctness', 'fault')} for name, outcome in outcomes.items()}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('evidence', type=Path, help='Capacity evidence directory, live or copied')
    args = parser.parse_args()
    try:
        result = summarize(args.evidence)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f'Evidence analysis failed: {error}', file=sys.stderr)
        return 1
    print(json.dumps(result, indent=2, allow_nan=False))
    return 0


if __name__ == '__main__':
    sys.exit(main())
