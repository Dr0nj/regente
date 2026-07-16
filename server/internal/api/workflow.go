// Package api — Diferenciais leva 2 (2026-07-07; hold geral 2026-07-16):
//
//	D-2  Pausa/resume de WORKFLOW com estado preservado:
//	     POST /api/folders/{name}/pause   → TODOS os status→HELD em massa (a daily do dia)
//	     POST /api/folders/{name}/resume  → HELD→status ORIGINAL em massa
//	D-11 Chaos engineering:
//	     POST /api/instances/{id}/inject-failure → falha sintética via fluxo REAL
//
// Pausa ≠ cancel: NADA além do status muda — attempts, cycle_runs, local_vars,
// confirmed e scheduled_at ficam intactos, então o resume retoma exatamente de
// onde parou (um cyclic na volta 3 continua da volta 3; um retry agendado pra
// daqui a 2 dias segue agendado). Hold GERAL (schemaV16): a pausa segura o dia
// INTEIRO da folder — WAITING, NOTOK, OK, CANCELLED, e as instances de
// CARRY-OVER (o carry avança order_date pro dia ativo, então elas entram no
// mesmo WHERE) — congelando o status original em held_from_status; o resume
// restaura cada um ao que era, não a WAITING cego. RUNNING é a exceção (a
// execução já está no agente — termina e pronto; um job não é segurável nem
// deletável em execução); HELD individual (hold_scope='') também fica de fora,
// preservando o hold do operador através do pause/resume da folder. O
// carry-over persiste HELD entre diárias, então uma folder pode ficar pausada
// por dias sem perder estado.
package api

import (
	"net/http"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

// pauseFolder — D-2 + hold geral. Segura o dia INTEIRO da folder (qualquer
// status exceto RUNNING/HELD, carry-over incluso). Set-based (1 UPDATE +
// 1 INSERT...SELECT de eventos), não por item: pausar uma folder de 50k jobs
// não pode ser 50k round-trips.
func (s *server) pauseFolder(w http.ResponseWriter, r *http.Request) {
	s.folderPauseResume(w, r, true)
}

// resumeFolder — D-2. Libera TODOS os segurados pela pausa da folder no dia,
// restaurando o status ORIGINAL de cada um (semântica Control-M "Release
// folder": o resume de workflow é um destrave em massa).
func (s *server) resumeFolder(w http.ResponseWriter, r *http.Request) {
	s.folderPauseResume(w, r, false)
}

func (s *server) folderPauseResume(w http.ResponseWriter, r *http.Request, pause bool) {
	folder := chi.URLParam(r, "name")
	if !s.requireFolderWrite(w, r, folder) {
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		date = s.cfg.Scheduler.TodayDate()
	}
	// hold_scope separa a PAUSA DE FOLDER (D-2) de um hold individual: só a folder
	// segura marca 'folder', e o resume só destrava o que a folder segurou —
	// holds individuais (scope '') sobrevivem ao resume da folder. Simétrico ao
	// Control-M "Hold folder" ⇒ "Release folder" (schemaV14). Hold GERAL
	// (schemaV16): a pausa pega QUALQUER status exceto RUNNING/HELD, congelando o
	// original em held_from_status; o resume restaura cada instance ao seu.
	//   - PAUSA:  ...status NOT IN (RUNNING,HELD)  → status=HELD, held_from_status=<original>, hold_scope='folder'
	//   - RESUME: ...status=HELD AND scope=folder  → status=<original|WAITING>,  hold_scope=''
	kind, msg := "paused", "workflow pause (folder "+folder+")"
	whereSQL := ` WHERE team=? AND order_date=? AND status NOT IN (?,?)`
	whereArgs := []any{folder, date, string(domain.StatusRunning), string(domain.StatusHeld)}
	updSQL := `UPDATE instances SET held_from_status=status, status=?, hold_scope='folder'`
	updSetArgs := []any{string(domain.StatusHeld)}
	if !pause {
		kind, msg = "resumed", "workflow resume (folder "+folder+")"
		whereSQL = ` WHERE team=? AND order_date=? AND status=? AND hold_scope=?`
		whereArgs = []any{folder, date, string(domain.StatusHeld), "folder"}
		updSQL = releaseSQL // restaura held_from_status ('' legado → WAITING) e zera o scope
		updSetArgs = nil
	}
	actor := actorFromCtx(r)

	// Evento ANTES do UPDATE (o SELECT do INSERT precisa ver o status antigo).
	// Mesma transação: ou pausa+registra tudo, ou nada.
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(
		`INSERT INTO instance_events(instance_id, kind, actor, message)
		 SELECT id, ?, ?, ? FROM instances`+whereSQL,
		append([]any{kind, actor, msg}, whereArgs...)...,
	); err != nil {
		_ = tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := tx.Exec(updSQL+whereSQL, append(updSetArgs, whereArgs...)...)
	if err != nil {
		_ = tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()

	s.cfg.Hub.BroadcastWeb("instance.bulk", map[string]any{
		"action": kind, "folder": folder, "total": n, "ok": n, "actor": actor,
	})
	if !pause && n > 0 {
		go s.cfg.Scheduler.Tick() // resume destrava — despacha sem esperar o próximo tick
	}
	writeJSON(w, 200, map[string]any{"folder": folder, "date": date, "action": kind, "affected": n})
}

// injectFailure — D-11 Chaos: POST /api/instances/{id}/inject-failure.
// Writer + write na folder da instance (mesmo enforcement das ações operacionais).
func (s *server) injectFailure(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	if err := s.cfg.Scheduler.InjectFailure(id, actorFromCtx(r)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "injected": true})
}
