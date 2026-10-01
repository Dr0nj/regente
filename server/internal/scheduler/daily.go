package scheduler

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"log"
	"sort"
	"time"
)

// DailyRun expõe somente metadados; o plano contém comandos e nunca sai na API.
type DailyRun struct {
	OrderDate  string     `json:"orderDate"`
	State      string     `json:"state"`
	CommitSHA  string     `json:"commitSha"`
	Expected   int        `json:"expected"`
	Inserted   int        `json:"inserted"`
	Checkpoint int        `json:"checkpoint"`
	Carried    int        `json:"carried"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	CanResume  bool       `json:"canResume"`
}

func (s *Scheduler) DailyRun(date string) (*DailyRun, error) {
	var r DailyRun
	var finished sql.NullTime
	err := s.db.QueryRow(`SELECT order_date,state,target_commit_sha,expected_count,inserted_count,checkpoint,carried_count,last_error,started_at,finished_at FROM daily_runs WHERE order_date=?`, date).Scan(&r.OrderDate, &r.State, &r.CommitSHA, &r.Expected, &r.Inserted, &r.Checkpoint, &r.Carried, &r.Error, &r.StartedAt, &finished)
	if err != nil {
		return nil, err
	}
	if finished.Valid {
		r.FinishedAt = &finished.Time
	}
	r.CanResume = r.State != "completed" && r.State != "legacy"
	return &r, nil
}
func (s *Scheduler) PendingDaily() (*DailyRun, error) {
	var date string
	if err := s.db.QueryRow(`SELECT order_date FROM daily_runs WHERE state NOT IN ('completed','legacy') ORDER BY order_date LIMIT 1`).Scan(&date); err != nil {
		return nil, err
	}
	return s.DailyRun(date)
}
func digest(v string) string                  { return fmt.Sprintf("%x", sha256.Sum256([]byte(v))) }
func planDigest(date, sha, raw string) string { return digest(date + "\n" + sha + "\n" + raw) }

var errNoDailyDefinitions = errors.New("no job definitions loaded; daily has not been planned")

// RunDaily é o wrapper histórico; API/auto usam MaterializeDaily para propagar erro.
func (s *Scheduler) RunDaily(date string) int {
	_, n, err := s.MaterializeDaily(date)
	if err != nil && !errors.Is(err, errNoDailyDefinitions) {
		log.Printf("[scheduler] daily %s incomplete: %v", date, err)
	}
	return n
}

// MaterializeDaily nunca consulta definitions vivas se já existe plano durável.
func (s *Scheduler) MaterializeDaily(date string) (run *DailyRun, created int, err error) {
	s.dailyMu.Lock()
	defer s.dailyMu.Unlock()
	if _, e := time.Parse("2006-01-02", date); e != nil {
		return nil, 0, fmt.Errorf("invalid daily date")
	}
	run, err = s.DailyRun(date)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.freezeDailyPlan(date)
	}
	if err != nil {
		return run, 0, err
	}
	run, err = s.DailyRun(date)
	if err != nil {
		return nil, 0, err
	}
	if !run.CanResume {
		return run, 0, nil
	}
	var raw, sum string
	if err = s.db.QueryRow(`SELECT plan_json,plan_checksum FROM daily_runs WHERE order_date=?`, date).Scan(&raw, &sum); err != nil {
		return run, 0, err
	}
	// O diagnóstico de falha só altera ciclos ainda abertos. Um concorrente que
	// completou não pode ser rebaixado por um resultado de commit ambíguo.
	defer func() {
		if err != nil {
			_, e := s.db.Exec(`UPDATE daily_runs SET state='failed',last_error=? WHERE order_date=? AND state NOT IN ('completed','legacy')`, err.Error(), date)
			if e != nil {
				log.Printf("[scheduler] daily failure state: %v", e)
			}
			s.dailyChanged(date)
		}
		if current, e := s.DailyRun(date); e == nil {
			run = current
		}
	}()
	var defs []domain.JobDefinition
	if planDigest(date, run.CommitSHA, raw) != sum || json.Unmarshal([]byte(raw), &defs) != nil || defs == nil || len(defs) != run.Expected {
		return run, 0, fmt.Errorf("frozen daily plan is invalid; restore verified state before resuming")
	}
	seen := map[string]bool{}
	for _, d := range defs {
		if d.ID == "" || seen[d.ID] || d.BusinessTime == nil || d.BusinessTime.Validate() != nil {
			return run, 0, fmt.Errorf("frozen daily plan contains an invalid definition")
		}
		seen[d.ID] = true
	}
	if _, err = s.carryOverWithCheckpoint(date, true); err != nil {
		return run, 0, fmt.Errorf("daily carry-over: %w", err)
	}
	for {
		var n int
		var done bool
		n, done, err = s.materializeDailyChunk(date, run.CommitSHA, sum, defs)
		if err != nil {
			return run, created, err
		}
		created += n
		s.dailyChanged(date)
		if done {
			break
		}
	}
	s.hub.BroadcastWeb("daily.started", map[string]interface{}{"orderDate": date, "created": created, "commitSha": run.CommitSHA})
	return run, created, nil
}

func (s *Scheduler) freezeDailyPlan(date string) error {
	calendar := s.BusinessCalendar()
	if e := calendar.Validate(); e != nil {
		return e
	}
	var defs []domain.JobDefinition
	var sha string
	if s.git != nil {
		var e error
		defs, sha, e = s.git.DefinitionSnapshot()
		if e != nil {
			return fmt.Errorf("daily definition snapshot: %w", e)
		}
	} else {
		s.mu.Lock()
		defs = append([]domain.JobDefinition(nil), s.defs...)
		s.mu.Unlock()
	}
	if len(defs) == 0 {
		_, e := s.carryOver(date)
		if e != nil {
			return e
		}
		s.mu.Lock()
		first := s.emptyDailyLoggedFor != date
		s.emptyDailyLoggedFor = date
		s.mu.Unlock()
		if first {
			log.Printf("[scheduler] daily %s: no job definitions loaded; retry when source is available", date)
		}
		return errNoDailyDefinitions
	}
	rows, e := s.db.Query(`SELECT definition_id FROM instances WHERE order_date=? AND COALESCE(carried_from,'')=''`, date)
	if e != nil {
		return e
	}
	existing := map[string]bool{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		existing[id] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	t, _ := time.Parse("2006-01-02", date)
	plan := []domain.JobDefinition{}
	seen := map[string]bool{}
	for _, d := range defs {
		if d.ID == "" || seen[d.ID] {
			return fmt.Errorf("daily source has an empty or duplicate definition ID")
		}
		seen[d.ID] = true
		if !d.Schedule.Enabled || existing[d.ID] || (s.calStore != nil && !IsScheduledOn(d, t, s.calStore)) {
			continue
		}
		plan = append(plan, s.freezeTime(d, calendar))
	}
	sort.Slice(plan, func(i, j int) bool { return plan[i].ID < plan[j].ID })
	raw, e := json.Marshal(plan)
	if e != nil {
		return e
	}
	// Uma linha congela toda a fonte. Não há instances até esta escrita terminar.
	_, e = s.db.Exec(`INSERT INTO daily_runs(order_date,started_at,state,target_commit_sha,expected_count,plan_json,plan_checksum) VALUES(?,?,'planning',?,?,?,?) ON CONFLICT(order_date) DO NOTHING`, date, s.Now(), sha, len(plan), string(raw), planDigest(date, sha, string(raw)))
	if e == nil {
		s.dailyChanged(date)
	}
	return e
}

func (s *Scheduler) dailyChanged(date string) {
	s.hub.BroadcastWeb("daily.changed", map[string]interface{}{"orderDate": date})
}

// UPDATE reserva a linha no PG e o writer no SQLite antes de ler checkpoint.
// Chunk e checkpoint COMMITam juntos: crash antes/depois tem a mesma retomada.
func (s *Scheduler) materializeDailyChunk(date, sha, sum string, defs []domain.JobDefinition) (int, bool, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return 0, false, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE daily_runs SET checkpoint=checkpoint WHERE order_date=?`, date); e != nil {
		return 0, false, e
	}
	var cp, expected, inserted, carried int
	var stored, state string
	if e = tx.QueryRow(`SELECT checkpoint,expected_count,inserted_count,carry_done,plan_checksum,state FROM daily_runs WHERE order_date=?`, date).Scan(&cp, &expected, &inserted, &carried, &stored, &state); e != nil {
		return 0, false, e
	}
	if state == "completed" {
		return 0, true, nil
	}
	if inserted != cp || carried != 1 || stored != sum || cp < 0 || cp > len(defs) || expected != len(defs) {
		return 0, false, fmt.Errorf("daily checkpoint does not match frozen plan")
	}
	if cp == len(defs) {
		if e = reconcileDaily(tx, date, defs); e != nil {
			return 0, false, e
		}
		if _, e = tx.Exec(`UPDATE daily_runs SET state='completed',finished_at=?,last_error='' WHERE order_date=?`, s.Now(), date); e != nil {
			return 0, false, e
		}
		return 0, true, tx.Commit()
	}
	end := min(cp+dailyBatchChunk, len(defs))
	ins, e := tx.Prepare(`INSERT INTO instances(id,definition_id,team,order_date,status,scheduled_at,definition_commit_sha,definition_snapshot,dry_run,label,job_type,confirm_req,environment,pinned_agent,conds_in,conds_out_add,resources,cond_logic) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if e != nil {
		return 0, false, e
	}
	defer ins.Close()
	evt, e := tx.Prepare(`INSERT INTO instance_events(instance_id,kind,actor,message,ts) VALUES(?,?,?,?,?)`)
	if e != nil {
		return 0, false, e
	}
	defer evt.Close()
	ledger, e := tx.Prepare(`INSERT INTO daily_order_ledger(order_date,ordinal,definition_id,instance_id,snapshot_checksum) VALUES(?,?,?,?,?)`)
	if e != nil {
		return 0, false, e
	}
	defer ledger.Close()
	for i := cp; i < end; i++ {
		d := defs[i]
		id := d.ID + "-" + date
		snap, e := json.Marshal(d)
		if e != nil {
			return 0, false, e
		}
		m := frozenMonitorCols(d)
		at := computeScheduledAt(d, date)
		if _, e = ins.Exec(id, d.ID, d.Team, date, string(domain.StatusWaiting), at, sha, string(snap), boolToInt(d.DryRun), m.label, m.jobType, m.confirmReq, m.environment, m.pinned, m.condsIn, m.condsOutAdd, m.resources, m.condLogic); e != nil {
			return 0, false, fmt.Errorf("daily order insert failed: %w", e)
		}
		if _, e = evt.Exec(id, "ordered", "scheduler", fmt.Sprintf("daily order_date=%s scheduled=%s commit=%s", date, at.Format(time.RFC3339), short(sha)), s.Now()); e != nil {
			return 0, false, fmt.Errorf("daily event insert failed: %w", e)
		}
		if _, e = ledger.Exec(date, i, d.ID, id, digest(string(snap))); e != nil {
			return 0, false, e
		}
	}
	if _, e = tx.Exec(`UPDATE daily_runs SET checkpoint=?,inserted_count=?,state='materializing',last_error='' WHERE order_date=?`, end, end, date); e != nil {
		return 0, false, e
	}
	if e = tx.Commit(); e != nil {
		return 0, false, e
	}
	return end - cp, false, nil
}

// Ledger não depende da instance continuar existindo após um Delete do operador.
func reconcileDaily(tx *db.Tx, date string, defs []domain.JobDefinition) error {
	rows, e := tx.Query(`SELECT ordinal,definition_id,instance_id,snapshot_checksum FROM daily_order_ledger WHERE order_date=? ORDER BY ordinal`, date)
	if e != nil {
		return e
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var i int
		var def, id, sum string
		if e = rows.Scan(&i, &def, &id, &sum); e != nil {
			return e
		}
		if i != n || i >= len(defs) {
			return fmt.Errorf("daily ledger is incomplete")
		}
		snap, e := json.Marshal(defs[i])
		if e != nil {
			return e
		}
		if def != defs[i].ID || id != def+"-"+date || sum != digest(string(snap)) {
			return fmt.Errorf("daily ledger differs from frozen plan")
		}
		n++
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if n != len(defs) {
		return fmt.Errorf("daily ledger count differs from expected plan")
	}
	return nil
}
