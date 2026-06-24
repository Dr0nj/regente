package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// seedDiffInst insere uma instance com commit + snapshot da def (p/ o diff).
func seedDiffInst(t *testing.T, s *Scheduler, date, commit string, def domain.JobDefinition) {
	t.Helper()
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at, definition_commit_sha, definition_snapshot)
		 VALUES(?,?,?,?,?,?,?,?)`,
		def.ID+"-"+date, def.ID, def.Team, date, string(domain.StatusWaiting), time.Now(), commit, string(snap),
	); err != nil {
		t.Fatalf("seed %s/%s: %v", def.ID, date, err)
	}
}

func changeOf(c DefChange, field string) *FieldChange {
	for i := range c.Changes {
		if c.Changes[i].Field == field {
			return &c.Changes[i]
		}
	}
	return nil
}
func changedDef(d DailyDiff, id string) *DefChange {
	for i := range d.Changed {
		if d.Changed[i].DefID == id {
			return &d.Changed[i]
		}
	}
	return nil
}

func TestDiffDaily_AddedRemovedChangedUnchanged(t *testing.T) {
	s := newTestScheduler(t)
	const A, B = "2026-06-20", "2026-06-21"

	base := func(id string) domain.JobDefinition {
		return domain.JobDefinition{ID: id, Team: "OPS", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	}
	// keep: igual nos dois (commits diferentes, mas snapshot idêntico) → unchanged.
	seedDiffInst(t, s, A, "aaa", base("keep"))
	seedDiffInst(t, s, B, "bbb", base("keep"))
	// gone: só em A → removido.
	seedDiffInst(t, s, A, "aaa", base("gone"))
	// new: só em B → adicionado.
	seedDiffInst(t, s, B, "bbb", base("new"))
	// sched: schedule mudou (runAt).
	schedA := base("sched")
	schedA.Schedule.RunAt = "06:00"
	schedB := base("sched")
	schedB.Schedule.RunAt = "07:00"
	seedDiffInst(t, s, A, "aaa", schedA)
	seedDiffInst(t, s, B, "bbb", schedB)
	// dep: dependência mudou.
	depA := base("dep")
	depA.Upstream = []domain.Upstream{{From: "x", Condition: domain.CondOnSuccess}}
	depB := base("dep")
	depB.Upstream = []domain.Upstream{{From: "y", Condition: domain.CondOnSuccess}}
	seedDiffInst(t, s, A, "aaa", depA)
	seedDiffInst(t, s, B, "bbb", depB)

	d, err := s.DiffDaily(A, B, "")
	if err != nil {
		t.Fatalf("DiffDaily: %v", err)
	}
	if d.SameCommit {
		t.Fatal("commits diferentes não deveriam dar SameCommit")
	}
	if d.Counts.Added != 1 || d.Counts.Removed != 1 || d.Counts.Changed != 2 || d.Counts.Unchanged != 1 {
		t.Fatalf("counts errados: %+v", d.Counts)
	}
	if len(d.Added) != 1 || d.Added[0].DefID != "new" {
		t.Fatalf("esperava added=[new], veio %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].DefID != "gone" {
		t.Fatalf("esperava removed=[gone], veio %+v", d.Removed)
	}
	// sched mudou no campo schedule.
	if c := changedDef(d, "sched"); c == nil || changeOf(*c, "schedule") == nil {
		t.Fatalf("esperava 'sched' alterado no campo schedule, veio %+v", d.Changed)
	}
	// dep mudou no campo upstream.
	if c := changedDef(d, "dep"); c == nil || changeOf(*c, "upstream") == nil {
		t.Fatalf("esperava 'dep' alterado no campo upstream, veio %+v", d.Changed)
	}
}

// Fast-path: mesma commit em ambas as dailies → nenhum def comum é comparado;
// tudo comum vira unchanged (mesmo commit ⟹ mesmo conteúdo).
func TestDiffDaily_SameCommitFastPath(t *testing.T) {
	s := newTestScheduler(t)
	const A, B = "2026-06-20", "2026-06-21"
	def := domain.JobDefinition{ID: "j", Team: "OPS", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedDiffInst(t, s, A, "samesha", def)
	seedDiffInst(t, s, B, "samesha", def)

	d, err := s.DiffDaily(A, B, "")
	if err != nil {
		t.Fatalf("DiffDaily: %v", err)
	}
	if !d.SameCommit || d.Counts.Changed != 0 || d.Counts.Unchanged != 1 {
		t.Fatalf("esperava SameCommit + 0 changed + 1 unchanged, veio sameCommit=%v counts=%+v", d.SameCommit, d.Counts)
	}
}

func TestDiffDaily_FolderScope(t *testing.T) {
	s := newTestScheduler(t)
	const A, B = "2026-06-20", "2026-06-21"
	mk := func(id, team string) domain.JobDefinition {
		return domain.JobDefinition{ID: id, Team: team, JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	}
	seedDiffInst(t, s, A, "aaa", mk("pa", "PAY"))
	seedDiffInst(t, s, A, "aaa", mk("oa", "OPS"))
	seedDiffInst(t, s, B, "bbb", mk("pb", "PAY")) // só PAY mudou (pa→pb)

	d, err := s.DiffDaily(A, B, "PAY")
	if err != nil {
		t.Fatalf("DiffDaily: %v", err)
	}
	// Escopado a PAY: vê pa (removido) e pb (adicionado); OPS some do escopo.
	if d.Counts.TotalA != 1 || d.Counts.TotalB != 1 || d.Counts.Added != 1 || d.Counts.Removed != 1 {
		t.Fatalf("folder scope PAY errado: %+v", d.Counts)
	}
}

func TestPrevDailyDate(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "j", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedDiffInst(t, s, "2026-06-18", "a", def)
	seedDiffInst(t, s, "2026-06-20", "b", def)

	if got := s.PrevDailyDate("2026-06-24"); got != "2026-06-20" {
		t.Fatalf("PrevDailyDate(24) = %q, esperava 2026-06-20", got)
	}
	if got := s.PrevDailyDate("2026-06-19"); got != "2026-06-18" {
		t.Fatalf("PrevDailyDate(19) = %q, esperava 2026-06-18", got)
	}
	if got := s.PrevDailyDate("2026-06-18"); got != "" {
		t.Fatalf("PrevDailyDate(18) = %q, esperava vazio (não há anterior)", got)
	}
}
