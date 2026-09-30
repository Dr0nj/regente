package scheduler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"
)

func (s *Scheduler) AttachDurable(e *execution.Engine) {
	s.durable = e
	e.Transition = s.durableTransition
	e.Prepare = s.durablePrepare
	e.Notify = s.durableChanged
}
func (s *Scheduler) IsDurableInstance(id string) bool {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM runtime_orders WHERE instance_id=?", id).Scan(&n)
	return (err != nil && s.durable != nil) || n != 0
}
func (s *Scheduler) DurableEngine() *execution.Engine { return s.durable }
func (s *Scheduler) durablePrepare(tx *db.Tx, o execution.Order, def domain.JobDefinition) (domain.JobDefinition, error) {
	var od, localJSON string
	var confirmed int
	if err := tx.QueryRow("SELECT "+odateExpr+",COALESCE(local_vars,''),COALESCE(confirmed,0) FROM instances WHERE id=?", o.SourceInstanceID).Scan(&od, &localJSON, &confirmed); err != nil {
		return def, err
	}
	if def.Confirm && confirmed == 0 {
		return def, execution.ErrConflict
	}
	calendar, ok := frozenCalendar(def)
	if !ok {
		return def, execution.ErrInvalid
	}
	ctx := BuildContextAt(def, o.SourceInstanceID, od, nil, "", s.Now().In(calendar.Location()))
	if localJSON != "" {
		if err := json.Unmarshal([]byte(localJSON), &ctx.Local); err != nil {
			return def, err
		}
	}
	if s.variables != nil {
		ctx.Global = s.variables.Snapshot()
	}
	if s.calStore != nil {
		cal := businessCalendar(def, s.calStore)
		ctx.BusinessDay = func(t time.Time) bool { return isBusinessDay(t, cal) }
	}
	def.Params = InterpolateParams(def.Params, ctx)
	return def, nil
}
func (s *Scheduler) durableAgent(def domain.JobDefinition) (string, error) {
	if strings.EqualFold(def.JobType, "SSH") || def.AgentID == "SERVER-AGENT" {
		if s.internalAgentID == "" {
			return "", execution.ErrNotFound
		}
		return s.internalAgentID, nil
	}
	rows, err := s.db.Query("SELECT p.agent_id,p.capabilities,p.internal FROM machine_principals p WHERE p.environment=? AND (p.internal=1 OR EXISTS(SELECT 1 FROM agent_tokens t WHERE t.agent_id=p.agent_id AND t.revoked_at=0 AND t.expires_at>?)) ORDER BY (SELECT COUNT(*) FROM execution_attempts a WHERE a.agent_id=p.agent_id AND a.state NOT IN ('succeeded','failed','cancelled')),p.agent_id", def.Environment, time.Now().UnixMilli())
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, caps string
		var internal bool
		if err = rows.Scan(&id, &caps, &internal); err != nil {
			return "", err
		}
		if internal && id != s.internalAgentID {
			continue
		}
		if def.AgentID != "" && def.AgentID != id {
			continue
		}
		values := strings.Split(caps, ",")
		if slices.Contains(values, execution.Capability) && slices.Contains(values, strings.ToUpper(def.JobType)) {
			return id, nil
		}
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	return "", execution.ErrNotFound
}
func (s *Scheduler) startDurable(id string, def domain.JobDefinition) {
	if s.durable == nil {
		return
	}
	agent, err := s.durableAgent(def)
	if err != nil {
		s.maybeEmitNoAgent(id, def.JobType)
		return
	}
	o, err := s.durable.CreateRuntimeOrder(id)
	if err != nil {
		log.Printf("[execution] order %s: %v", id, err)
		return
	}
	intent := "start"
	if o.CurrentExecution != "" {
		intent = "rerun"
		if o.State == "failed" {
			intent = "retry"
		}
	}
	_, err = s.durable.Start(o.ID, agent, "runtime:"+strconv.Itoa(o.Attempt+1), intent)
	if err != nil && !errors.Is(err, execution.ErrConflict) {
		log.Printf("[execution] intent %s: %v", id, err)
	}
}
func durableEvent(tx *db.Tx, id, kind, actor, message string, at time.Time) error {
	_, err := tx.Exec("INSERT INTO instance_events(instance_id,kind,actor,message,ts) VALUES(?,?,?,?,?)", id, kind, actor, message, at)
	return err
}
func durableCondition(tx *db.Tx, name, odate, actor string, remove bool, now time.Time) error {
	var lookupErr error
	prev := func(date string) string {
		var previous string
		lookupErr = tx.QueryRow("SELECT COALESCE(MAX(order_date),'') FROM daily_runs WHERE order_date<? AND state IN ('completed','legacy')", date).Scan(&previous)
		if previous == "" {
			previous = AddDays(date, -1)
		}
		return previous
	}
	base, scope := resolveCondScope(name, odate, prev)
	if lookupErr != nil {
		return lookupErr
	}
	if remove {
		_, err := tx.Exec("DELETE FROM conditions WHERE name=? AND scope_date=?", base, scope)
		return err
	}
	_, err := tx.Exec("INSERT INTO conditions(name,scope_date,set_at,set_by) VALUES(?,?,?,?) ON CONFLICT(name,scope_date) DO UPDATE SET set_at=excluded.set_at,set_by=excluded.set_by", base, scope, now, actor)
	return err
}
func durableOut(tx *db.Tx, def domain.JobDefinition, odate, actor string, now time.Time) error {
	for _, name := range def.ConditionsOutAdd {
		if err := durableCondition(tx, name, odate, actor, false, now); err != nil {
			return err
		}
	}
	for _, name := range def.ConditionsOutRemove {
		if err := durableCondition(tx, name, odate, actor, true, now); err != nil {
			return err
		}
	}
	return nil
}
func (s *Scheduler) durableResources(tx *db.Tx, id string, want map[string]int, bypass bool) error {
	for name, qty := range want {
		if qty <= 0 {
			continue
		}
		if _, err := tx.Exec("INSERT INTO resources(name,capacity) VALUES(?,1) ON CONFLICT(name) DO NOTHING", name); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE resources SET capacity=capacity WHERE name=?", name); err != nil {
			return err
		}
		var capacity, used, held int
		if err := tx.QueryRow("SELECT capacity FROM resources WHERE name=?", name).Scan(&capacity); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT COALESCE(SUM(quantity),0) FROM execution_resource_holds WHERE name=?", name).Scan(&used); err != nil {
			return err
		}
		err := tx.QueryRow("SELECT quantity FROM execution_resource_holds WHERE name=? AND instance_id=?", name, id).Scan(&held)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !bypass && used-held+qty > capacity {
			return execution.ErrCapacity
		}
		if _, err = tx.Exec("INSERT INTO execution_resource_holds(instance_id,name,quantity) VALUES(?,?,?) ON CONFLICT(instance_id,name) DO UPDATE SET quantity=excluded.quantity", id, name, qty); err != nil {
			return err
		}
	}
	return nil
}
func (s *Scheduler) durableTransition(tx *db.Tx, o execution.Order, a execution.Attempt, kind string, result *execution.Result) error {
	var raw, expected, odate, status, localJSON string
	var budget, runs int
	if err := tx.QueryRow("SELECT i.definition_snapshot,COALESCE(l.snapshot_checksum,''),"+"COALESCE(NULLIF(i.carried_from,''),i.order_date)"+",i.status,COALESCE(i.attempts,1),COALESCE(i.cycle_runs,0),COALESCE(i.local_vars,'') FROM instances i LEFT JOIN daily_order_ledger l ON l.instance_id=i.id WHERE i.id=?", o.SourceInstanceID).Scan(&raw, &expected, &odate, &status, &budget, &runs, &localJSON); err != nil {
		return err
	}
	var def domain.JobDefinition
	if json.Unmarshal([]byte(raw), &def) != nil || def.BusinessTime == nil || def.BusinessTime.Validate() != nil {
		return execution.ErrInvalid
	}
	// O snapshot runtime deve continuar sendo o que a ordem verificada congelou.
	var sourceSum string
	if err := tx.QueryRow("SELECT snapshot_checksum FROM lab_orders WHERE id=?", o.ID).Scan(&sourceSum); err != nil {
		return err
	}
	if digest(raw) != sourceSum || (expected != "" && expected != sourceSum) {
		return execution.ErrConflict
	}
	var blocked int
	if err := tx.QueryRow("SELECT COUNT(*) FROM daily_order_ledger l JOIN daily_runs d ON d.order_date=l.order_date WHERE l.instance_id=? AND d.state NOT IN ('completed','legacy')", o.SourceInstanceID).Scan(&blocked); err != nil {
		return err
	}
	if blocked != 0 {
		return execution.ErrConflict
	}
	now := s.Now()
	id := o.SourceInstanceID
	switch kind {
	case "planned":
		if status != "WAITING" {
			return execution.ErrConflict
		}
		var forced int
		var mode string
		if err := tx.QueryRow("SELECT COALESCE(forced,0),COALESCE(force_mode,'') FROM instances WHERE id=?", id).Scan(&forced, &mode); err != nil {
			return err
		}
		if err := s.durableResources(tx, id, def.Resources, forced != 0 && mode != ForceModeOrder); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE instances SET status='RUNNING',started_at=NULL,finished_at=NULL,exit_code=NULL,agent_id=? WHERE id=? AND status='WAITING'", a.AgentID, id); err != nil {
			return err
		}
		return durableEvent(tx, id, "submitted", "scheduler", "durable execution "+a.ExecutionID, now)
	case "started":
		if a.State != "running" {
			return nil
		}
		if _, err := tx.Exec("UPDATE instances SET started_at=?,status='RUNNING' WHERE id=?", now, id); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO instance_runs(instance_id,definition_id,order_date,attempt,started_at,status) VALUES(?,?,?,?,?,'RUNNING')", id, def.ID, odate, a.Attempt, now); err != nil {
			return err
		}
		return durableEvent(tx, id, "started", "agent", a.ExecutionID, now)
	case "uncertain":
		if _, err := tx.Exec("UPDATE instances SET status='UNCERTAIN' WHERE id=?", id); err != nil {
			return err
		}
		return durableEvent(tx, id, "uncertain", "execution", a.Reason, now)
	case "cancel_requested":
		return durableEvent(tx, id, "cancel-requested", "operator", "awaiting durable cancellation receipt", now)
	case "result", "cancelled", "resolved":
	default:
		return nil
	}
	code, output := 0, ""
	final := "OK"
	if result != nil {
		code, output = result.ExitCode, result.Output
	}
	cancelled := a.State == "cancelled"
	if cancelled {
		code = -1
		output = "(cancellation acknowledged; external effects are not rolled back)"
	}
	if code != 0 {
		final = "NOTOK"
	}
	if cancelled && a.StartedAt == 0 {
		final = "CANCELLED"
	}
	if _, err := tx.Exec("UPDATE instance_runs SET finished_at=?,status=?,exit_code=?,agent_id=? WHERE instance_id=? AND finished_at IS NULL", now, final, code, a.AgentID, id); err != nil {
		return err
	}
	local := map[string]string{}
	if localJSON != "" {
		if err := json.Unmarshal([]byte(localJSON), &local); err != nil {
			return err
		}
	}
	for _, m := range setLocalVarDirective.FindAllStringSubmatch(output, maxSetVarsPerJob) {
		local[m[1]] = m[2]
		if err := durableEvent(tx, id, "set-var-local", "execution", m[1]+"="+m[2], now); err != nil {
			return err
		}
	}
	localRaw, err := json.Marshal(local)
	if err != nil {
		return err
	}
	retry := kind == "result" && !cancelled && final == "NOTOK" && def.Retries > 0 && budget <= def.Retries
	events := []actionEvent{}
	if final == "NOTOK" && kind == "result" && !cancelled {
		events = append(events, actionEvent{kind: "attempt", attempt: budget})
	}
	if !retry && final != "CANCELLED" {
		events = append(events, actionEvent{kind: "result", status: domain.InstanceStatus(final)}, actionEvent{kind: "exit", exitCode: code})
	}
	for i, rule := range def.Actions {
		matched := false
		for _, ev := range events {
			matched = matched || actionMatches(rule, ev)
		}
		if !matched {
			continue
		}
		effectID := a.ExecutionID + "-action-" + strconv.Itoa(i)
		state := "done"
		switch rule.Do {
		case "set-condition":
			if err = durableCondition(tx, rule.Condition, odate, "actions", false, now); err != nil {
				return err
			}
		case "set-ok":
			if !retry && final == "NOTOK" {
				final = "OK"
				code = 0
			}
		case "notify", "run-job":
			state = "pending"
		default:
			return execution.ErrInvalid
		}
		payload, err := json.Marshal(durableEffectPayload{Definition: def, Rule: rule, OrderDate: odate})
		if rule.Do == "notify" {
			var raw string
			raw, state, err = s.durableActionNotify(tx, def, rule, id)
			payload = []byte(raw)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO execution_effects(id,execution_id,instance_id,kind,payload,state,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", effectID, a.ExecutionID, id, rule.Do, string(payload), state, now.UnixMilli()); err != nil {
			return err
		}
		if err = durableEvent(tx, id, "action", "actions", rule.Do+" "+effectID, now); err != nil {
			return err
		}
	}
	if !retry && final == "OK" {
		if err = durableOut(tx, def, odate, "execution", now); err != nil {
			return err
		}
	}
	nextAt := now
	nextBudget := budget
	finished := any(now)
	if retry {
		final = "WAITING"
		nextBudget++
		delay := 5 * time.Second
		if def.RetryDelayMin > 0 {
			delay = time.Duration(def.RetryDelayMin) * time.Minute
		}
		nextAt = now.Add(delay)
		finished = nil
	}
	cycle := false
	if !retry && final == "OK" && def.Schedule.Cyclic && def.Schedule.IntervalMin > 0 {
		done := runs + 1
		next := now.Add(time.Duration(def.Schedule.IntervalMin) * time.Minute)
		end := orderWindowEnd(def, odate)
		if (def.Schedule.CyclicMaxRuns <= 0 || done < def.Schedule.CyclicMaxRuns) && (end.IsZero() || !next.After(end)) {
			cycle = true
			final = "WAITING"
			nextAt = next
			nextBudget = 1
			finished = nil
			runs = done
		}
	}
	if _, err = tx.Exec("UPDATE instances SET status=?,exit_code=?,output=?,finished_at=?,scheduled_at=?,attempts=?,cycle_runs=?,local_vars=? WHERE id=?", final, code, output, finished, nextAt, nextBudget, runs, string(localRaw), id); err != nil {
		return err
	}
	if !retry {
		if _, err = tx.Exec("DELETE FROM execution_resource_holds WHERE instance_id=?", id); err != nil {
			return err
		}
	}
	if !retry {
		if err = s.durableAlerts(tx, a, def, id, func() string {
			if code == 0 {
				return "OK"
			}
			if final == "CANCELLED" {
				return final
			}
			return "NOTOK"
		}(), budget); err != nil {
			return err
		}
	}
	eventKind := "finished"
	if retry {
		eventKind = "retry"
	}
	if cycle {
		eventKind = "cyclic"
	}
	if kind == "resolved" {
		eventKind = "resolved"
	}
	if err = durableEvent(tx, id, eventKind, "execution", fmt.Sprintf("execution=%s status=%s exit=%d", a.ExecutionID, final, code), now); err != nil {
		return err
	}
	if !retry && final != "CANCELLED" {
		if _, err = tx.Exec("INSERT INTO execution_effects(id,execution_id,instance_id,kind,payload,created_at) VALUES(?,?,?,'terminal-hooks',?,?) ON CONFLICT(id) DO NOTHING", a.ExecutionID+"-hooks", a.ExecutionID, id, output, now.UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}

type durableEffectPayload struct {
	Definition domain.JobDefinition
	Rule       domain.ActionRule
	OrderDate  string
}

func (s *Scheduler) durableChanged(exec string) {
	a, err := s.durable.Attempt(exec)
	if err != nil {
		return
	}
	o, err := s.durable.Order(a.OrderID)
	if err != nil || !o.Runtime {
		return
	}
	var status string
	_ = s.db.QueryRow("SELECT status FROM instances WHERE id=?", o.SourceInstanceID).Scan(&status)
	if s.resources != nil {
		rows, err := s.db.Query("SELECT name,quantity FROM execution_resource_holds WHERE instance_id=?", o.SourceInstanceID)
		if err == nil {
			want := map[string]int{}
			for rows.Next() {
				var name string
				var qty int
				if err = rows.Scan(&name, &qty); err != nil {
					break
				}
				want[name] = qty
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err == nil {
				if len(want) == 0 {
					s.resources.Release(o.SourceInstanceID)
				} else {
					s.resources.LoadFromDB(s.db)
					s.resources.Reacquire(o.SourceInstanceID, want)
				}
			}
		}
	}
	s.hub.BroadcastWeb("instance.changed", map[string]any{"id": o.SourceInstanceID, "status": status, "execution": a})
	s.hub.BroadcastWeb("condition.changed", map[string]string{"by": o.SourceInstanceID})
}
