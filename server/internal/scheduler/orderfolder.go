// Order Folder — ordenar a folder INTEIRA na diária ATIVA.
//
// Equivalente em massa do "Order Force" por job: cria uma ordem NOVA para cada
// definition da folder, com order_date = a DATA DE NEGÓCIO corrente (a diária
// ativa — ver BusinessDate/DAY-1), e não a data de calendário. É a ação de quem
// tem uma folder que não entrou no dia (calendário não bateu, folder criada
// depois da daily, schedule desligado) e quer colocá-la para rodar HOJE.
//
// Semântica (idêntica à do Order Force individual, force_mode='order'):
// bypassa só o AGENDAMENTO — calendário, frequência, schedule.enabled. Todos os
// gates de RUNTIME continuam valendo: condições, agente, recursos, janela de
// horário e Wait-for-confirmation. É justamente isso que faz a folder rodar
// respeitando as dependências, em vez de disparar tudo de uma vez.
//
// IDEMPOTENTE POR DIA: definition que já tem instance na diária ativa é PULADA
// (não duplica a folder inteira em quem chamar duas vezes). A resposta separa
// `ordered` de `skipped` para o operador ver o que aconteceu.
package scheduler

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// FolderOrder — resultado de OrderFolder.
type FolderOrder struct {
	Folder    string   `json:"folder"`
	OrderDate string   `json:"orderDate"`
	Ordered   []string `json:"ordered"` // ids das instances criadas
	Skipped   []string `json:"skipped"` // ids das definitions que já estavam no dia
}

// OrderFolder ordena todas as definitions da folder na diária ativa.
// Erro só quando a folder não existe / não tem definition publicada — folder
// inteiramente já ordenada devolve Ordered vazio e Skipped cheio, que é sucesso.
func (s *Scheduler) OrderFolder(folder string) (FolderOrder, error) {
	now := s.NowLocal()
	date := s.BusinessDate(now)
	res := FolderOrder{Folder: folder, OrderDate: date, Ordered: []string{}, Skipped: []string{}}

	// Cópia das defs da folder sob lock (o slice s.defs é trocado pelo reload).
	s.mu.Lock()
	defs := make([]domain.JobDefinition, 0, 16)
	for i := range s.defs {
		if s.defs[i].Team == folder {
			defs = append(defs, s.defs[i])
		}
	}
	s.mu.Unlock()
	if len(defs) == 0 {
		return res, fmt.Errorf("folder %s has no published definition", folder)
	}

	// Quem já está na diária ativa não é reordenado. Uma query só — a alternativa
	// (um SELECT por job) seria 200 round-trips numa folder como a de teste.
	already := map[string]bool{}
	rows, err := s.db.Query(`SELECT definition_id FROM instances WHERE team=? AND order_date=?`, folder, date)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return res, err
		}
		already[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}

	commitSHA := s.currentCommitSHA()
	// hhmmss no id: mesmo formato do Order Force individual. Como definition já
	// ordenada no dia é pulada, dois "order folder" seguidos não colidem no PK.
	stamp := now.Format("150405")
	batch := make([]pendingInstance, 0, len(defs))
	for _, def := range defs {
		if already[def.ID] {
			res.Skipped = append(res.Skipped, def.ID)
			continue
		}
		snap, err := json.Marshal(def)
		if err != nil {
			log.Printf("[order-folder] %s: snapshot: %v", def.ID, err)
			continue
		}
		batch = append(batch, pendingInstance{
			id: def.ID + "-FORCE-" + stamp, defID: def.ID, team: def.Team,
			scheduledAt: now, snapshot: string(snap), dryRun: def.DryRun,
			mcols: frozenMonitorCols(def),
		})
	}
	if len(batch) == 0 {
		return res, nil // tudo já estava no dia — sucesso, nada a fazer
	}

	created := s.insertForcedBatch(date, commitSHA, batch, "order folder "+folder)
	if len(created) > 0 {
		res.Ordered = created
	}

	s.hub.BroadcastWeb("instance.bulk", map[string]any{
		"action": "ordered", "folder": folder, "total": len(created), "ok": len(created),
	})
	// Um Tick só: ele avalia os gates de TODAS as novas de uma vez e despacha o
	// que já pode rodar (as raízes), deixando o resto em WAITING pelas condições.
	// Repetir aqui o gate+dispatch do ForceOrder individual seria uma segunda
	// cópia da mesma regra — e é o tick que a mantém.
	go s.Tick()
	return res, nil
}

// insertForcedBatch grava as ordens forçadas em lote (uma transação por chunk,
// como a daily) e devolve os ids efetivamente criados. forced=1 +
// force_mode='order': bypassa o agendamento, respeita os gates de runtime.
func (s *Scheduler) insertForcedBatch(date, commitSHA string, batch []pendingInstance, reason string) []string {
	out := make([]string, 0, len(batch))
	for start := 0; start < len(batch); start += dailyBatchChunk {
		end := min(start+dailyBatchChunk, len(batch))
		out = append(out, s.insertForcedChunk(date, commitSHA, batch[start:end], reason)...)
	}
	return out
}

func (s *Scheduler) insertForcedChunk(date, commitSHA string, chunk []pendingInstance, reason string) []string {
	if len(chunk) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("[order-folder] %s: begin tx: %v", date, err)
		return nil
	}
	insStmt, err := tx.Prepare(`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at, forced, force_mode, definition_commit_sha, definition_snapshot, dry_run,
		label, job_type, confirm_req, environment, pinned_agent, conds_in, conds_out_add, resources, cond_logic) VALUES(?,?,?,?,?,?,1,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		log.Printf("[order-folder] %s: prepare insert: %v", date, err)
		return nil
	}
	defer insStmt.Close()
	evtStmt, err := tx.Prepare(`INSERT INTO instance_events(instance_id, kind, actor, message) VALUES(?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		log.Printf("[order-folder] %s: prepare event: %v", date, err)
		return nil
	}
	defer evtStmt.Close()

	msg := reason + " order_date=" + date
	if commitSHA != "" {
		msg += " commit=" + short(commitSHA)
	}
	created := make([]string, 0, len(chunk))
	for _, p := range chunk {
		if _, err := insStmt.Exec(p.id, p.defID, p.team, date, string(domain.StatusWaiting), p.scheduledAt, ForceModeOrder, commitSHA, p.snapshot, boolToInt(p.dryRun),
			p.mcols.label, p.mcols.jobType, p.mcols.confirmReq, p.mcols.environment, p.mcols.pinned, p.mcols.condsIn, p.mcols.condsOutAdd, p.mcols.resources, p.mcols.condLogic); err != nil {
			log.Printf("[order-folder] insert %s: %v", p.id, err)
			continue
		}
		if _, err := evtStmt.Exec(p.id, "force-ordered", "operator", msg); err != nil {
			log.Printf("[order-folder] event %s: %v", p.id, err)
		}
		created = append(created, p.id)
	}
	if err := tx.Commit(); err != nil {
		log.Printf("[order-folder] %s: commit: %v", date, err)
		return nil
	}
	return created
}
