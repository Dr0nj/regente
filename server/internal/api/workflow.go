// Package api — Diferenciais leva 2 (2026-07-07):
//
//	D-2  Pausa/resume de WORKFLOW com estado preservado:
//	     POST /api/folders/{name}/pause   → WAITING→HELD em massa (a daily do dia)
//	     POST /api/folders/{name}/resume  → HELD→WAITING em massa
//	D-11 Chaos engineering:
//	     POST /api/instances/{id}/inject-failure → falha sintética via fluxo REAL
//
// Pausa ≠ cancel: NADA além do status muda — attempts, cycle_runs, local_vars,
// confirmed e scheduled_at ficam intactos, então o resume retoma exatamente de
// onde parou (um cyclic na volta 3 continua da volta 3; um retry agendado pra
// daqui a 2 dias segue agendado). RUNNING não é pausável (termina e os
// sucessores WAITING já estão HELD); o carry-over persiste HELD entre diárias,
// então uma folder pode ficar pausada por dias sem perder estado.
package api

import (
	"net/http"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

// pauseFolder — D-2. Set-based (1 UPDATE + 1 INSERT...SELECT de eventos), não
// por item: pausar uma folder de 50k jobs não pode ser 50k round-trips.
func (s *server) pauseFolder(w http.ResponseWriter, r *http.Request) {
	s.folderPauseResume(w, r, true)
}

// resumeFolder — D-2. Libera TODOS os HELD da folder no dia (semântica
// Control-M "Release folder": o resume de workflow é um destrave em massa).
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
	from, to, kind, msg := string(domain.StatusWaiting), string(domain.StatusHeld), "paused", "workflow pause (folder "+folder+")"
	if !pause {
		from, to, kind, msg = string(domain.StatusHeld), string(domain.StatusWaiting), "resumed", "workflow resume (folder "+folder+")"
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
		 SELECT id, ?, ?, ? FROM instances WHERE team=? AND order_date=? AND status=?`,
		kind, actor, msg, folder, date, from,
	); err != nil {
		_ = tx.Rollback()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := tx.Exec(
		`UPDATE instances SET status=? WHERE team=? AND order_date=? AND status=?`,
		to, folder, date, from,
	)
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
