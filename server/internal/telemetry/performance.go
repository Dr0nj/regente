package telemetry

import (
	"fmt"
	"io"
	"math"
	"sync/atomic"
	"time"
)

// Enum fechado: nunca usar IDs de jobs, usuários ou revisões como labels.
type PerfStage uint8

const (
	DefinitionsDigest PerfStage = iota
	DefinitionsRead
	DefinitionsNormalize
	DefinitionsBuild
	WorkspaceView
	AuditExport
	AuditPersist
	SchedulerCandidates
	SchedulerEvaluation
	perfStageCount
)

var stageNames = [perfStageCount]string{"definitions_digest", "definitions_read", "definitions_normalize", "definitions_build", "workspace_view", "audit_export", "audit_persist", "scheduler_candidates", "scheduler_evaluation"}
var bounds = [...]float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10, 30, 60}

type histogram struct {
	count   atomic.Uint64
	nanos   atomic.Uint64
	buckets [len(bounds)]atomic.Uint64
}

var stages [perfStageCount]histogram

func Measure(stage PerfStage) func() {
	start := time.Now()
	return func() { ObserveDuration(stage, time.Since(start)) }
}
func ObserveDuration(stage PerfStage, d time.Duration) {
	if stage >= perfStageCount {
		return
	}
	h := &stages[stage]
	h.count.Add(1)
	h.nanos.Add(uint64(max(0, d)))
	for i, b := range bounds {
		if d.Seconds() <= b {
			h.buckets[i].Add(1)
		}
	}
}

type PerfEvent uint8

const (
	CacheHit PerfEvent = iota
	CacheBuild
	CacheFailure
	AuditBatchRecords
	AuditReplay
	AuditRejected
	SchedulerEvaluated
	SchedulerPage
	perfEventCount
)

var eventNames = [perfEventCount]string{"definitions_cache_hits", "definitions_builds", "definitions_build_failures", "audit_exported_records", "execution_receipt_replays", "audit_admission_rejections", "scheduler_candidate_evaluations", "scheduler_reconciliation_pages"}
var events [perfEventCount]atomic.Uint64

func Count(event PerfEvent, n uint64) {
	if event < perfEventCount {
		events[event].Add(n)
	}
}
func WritePerformance(w io.Writer) {
	fmt.Fprintln(w, "# TYPE regente_perf_stage_seconds histogram")
	for i := range stages {
		h := &stages[i]
		for j, b := range bounds {
			fmt.Fprintf(w, "regente_perf_stage_seconds_bucket{stage=%q,le=%q} %d\n", stageNames[i], fmt.Sprint(b), h.buckets[j].Load())
		}
		fmt.Fprintf(w, "regente_perf_stage_seconds_bucket{stage=%q,le=%q} %d\nregente_perf_stage_seconds_count{stage=%q} %d\nregente_perf_stage_seconds_sum{stage=%q} %.9f\n", stageNames[i], fmt.Sprint(math.Inf(1)), h.count.Load(), stageNames[i], h.count.Load(), stageNames[i], float64(h.nanos.Load())/1e9)
	}
	for i := range events {
		fmt.Fprintf(w, "# TYPE regente_%s_total counter\nregente_%s_total %d\n", eventNames[i], eventNames[i], events[i].Load())
	}
}
