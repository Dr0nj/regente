// Package api — D-3 Event-driven confiável: ingestão de eventos EXTERNOS.
//
// POST /api/events/ingest — outro sistema (CDC, pipeline, webhook de vendor)
// avisa o Regente que algo aconteceu, e isso destrava jobs SEM polling:
//
//	{"id":"pedido-batch-2026-07-07",        ← dedupe key DO EMISSOR (opcional)
//	 "source":"sap", "kind":"file-arrived",
//	 "conditions":["ARQ_VENDAS_OK"],        ← seta conditions (escopo date)
//	 "forceJob":"etl-vendas",               ← e/ou force-order de um job
//	 "date":"2026-07-07"}                   ← default hoje (tz de negócio)
//
// "Confiável" aqui é semântica, não enfeite:
//   - IDEMPOTENTE: o `id` é PK em external_events; o retry do emissor (at-least-
//     once) responde 200 {duplicate:true} sem re-aplicar o efeito. Sem `id`,
//     não há chave de dedupe e cada POST aplica (documentado).
//   - PERSISTIDO: evento + o que ele causou (`applied`) ficam na tabela — forense
//     de "quem destravou esse job às 03:14".
//   - IMEDIATO: cutuca o tick; job em WAIT_CONDITION despacha na hora.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *server) ingestEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string   `json:"id"`
		Source     string   `json:"source"`
		Kind       string   `json:"kind"`
		Condition  string   `json:"condition"`
		Conditions []string `json:"conditions"`
		ForceJob   string   `json:"forceJob"`
		Date       string   `json:"date"`
	}
	body, err := decodeJSONBody(r, &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Condition != "" {
		req.Conditions = append(req.Conditions, req.Condition)
	}
	if len(req.Conditions) == 0 && req.ForceJob == "" {
		http.Error(w, "nothing to apply: pass conditions[] and/or forceJob", http.StatusBadRequest)
		return
	}
	if req.Date == "" {
		req.Date = s.cfg.Scheduler.TodayDate()
	}

	// Dedupe: INSERT com PK é o teste de duplicata (atômico — dois retries
	// simultâneos não passam os dois). Sem id do emissor, gera um único (sem
	// garantia de dedupe, o emissor optou por não ter).
	dedupe := req.ID != ""
	if req.ID == "" {
		req.ID = fmt.Sprintf("anon-%d", time.Now().UnixNano())
	}
	if _, err := s.cfg.DB.Exec(
		`INSERT INTO external_events(id, source, kind, payload) VALUES(?,?,?,?)`,
		req.ID, req.Source, req.Kind, string(body),
	); err != nil {
		if dedupe && s.externalEventExists(req.ID) {
			writeJSON(w, 200, map[string]any{"id": req.ID, "duplicate": true, "applied": false})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Aplica os efeitos. Falha parcial é registrada em `applied` (o evento JÁ
	// está deduplicado — reprocessar seria manual e consciente, não um retry cego).
	var applied []string
	actor := "external:" + orDefault(req.Source, "unknown")
	if conds := s.cfg.Scheduler.Conditions(); conds != nil {
		for _, name := range req.Conditions {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if err := conds.Set(name, req.Date, actor); err != nil {
				applied = append(applied, "condition "+name+": ERROR "+err.Error())
				continue
			}
			applied = append(applied, "condition "+name+"@"+req.Date)
			s.cfg.Hub.BroadcastWeb("condition.changed", map[string]string{"name": name, "scopeDate": req.Date})
		}
	}
	if req.ForceJob != "" {
		if instID, err := s.cfg.Scheduler.ForceOrder(req.ForceJob); err != nil {
			applied = append(applied, "force "+req.ForceJob+": ERROR "+err.Error())
		} else {
			applied = append(applied, "force "+req.ForceJob+" → "+instID)
		}
	}
	_, _ = s.cfg.DB.Exec(`UPDATE external_events SET applied=? WHERE id=?`, strings.Join(applied, "; "), req.ID)
	go s.cfg.Scheduler.Tick()

	writeJSON(w, 200, map[string]any{"id": req.ID, "duplicate": false, "applied": applied})
}

// listExternalEvents — GET /api/events/external: os últimos ingeridos (forense/debug).
func (s *server) listExternalEvents(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r, 100)
	rows, err := s.cfg.DB.Query(
		`SELECT id, source, kind, applied, received_at FROM external_events ORDER BY received_at DESC LIMIT ?`, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type row struct {
		ID         string    `json:"id"`
		Source     string    `json:"source"`
		Kind       string    `json:"kind"`
		Applied    string    `json:"applied"`
		ReceivedAt time.Time `json:"receivedAt"`
	}
	out := []row{}
	for rows.Next() {
		var e row
		if rows.Scan(&e.ID, &e.Source, &e.Kind, &e.Applied, &e.ReceivedAt) == nil {
			out = append(out, e)
		}
	}
	if !rowsOK(w, rows) {
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) externalEventExists(id string) bool {
	var n int
	_ = s.cfg.DB.QueryRow(`SELECT COUNT(*) FROM external_events WHERE id=?`, id).Scan(&n)
	return n > 0
}

// decodeJSONBody decodifica e devolve os bytes crus (pra persistir o payload).
func decodeJSONBody(r *http.Request, v any) ([]byte, error) {
	var buf strings.Builder
	dec := json.NewDecoder(io.TeeReader(r.Body, &buf))
	if err := dec.Decode(v); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
