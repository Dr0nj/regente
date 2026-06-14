// Package scheduler — F19 SLA + Alerting (Control-M Service Level Manager).
//
// Engine simples avaliada a cada tick:
//
//   - Se instance está RUNNING há mais que sla.expectedDurationMin → breach "duration".
//   - Se hora atual passou de sla.deadlineHM e instance ainda não terminou → breach "deadline".
//
// Breach gera linha em `sla_breaches` (sem dup pelo mesmo instance+kind),
// notifica via webhook se WebhookURL está setado, e broadcasta no WS.
package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
)

type SLAEngine struct {
	db  *db.DB
	hub *hub.Hub
}

func NewSLAEngine(db *db.DB, h *hub.Hub) *SLAEngine {
	return &SLAEngine{db: db, hub: h}
}

// Evaluate verifica todas as instances RUNNING contra suas SLAs.
// Chamado pelo tick do scheduler.
func (e *SLAEngine) Evaluate(defs map[string]domain.JobDefinition, now time.Time) {
	rows, err := e.db.Query(
		`SELECT id, definition_id, order_date, started_at FROM instances
		 WHERE status='RUNNING' AND started_at IS NOT NULL`,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, defID, orderDate string
		var started time.Time
		if err := rows.Scan(&id, &defID, &orderDate, &started); err != nil {
			continue
		}
		def, ok := defs[defID]
		if !ok || def.SLA == nil {
			continue
		}
		// duration breach
		if def.SLA.ExpectedDurationMin > 0 {
			limit := time.Duration(def.SLA.ExpectedDurationMin) * time.Minute
			if now.Sub(started) > limit {
				e.recordBreach(id, defID, "duration", def.SLA.Severity,
					fmt.Sprintf("running for %s, expected ≤ %dmin", now.Sub(started).Round(time.Second), def.SLA.ExpectedDurationMin),
					def.SLA.WebhookURL)
			}
		}
		// deadline breach
		if def.SLA.DeadlineHM != "" {
			parts := strings.Split(def.SLA.DeadlineHM, ":")
			if len(parts) == 2 {
				hh, _ := strconv.Atoi(parts[0])
				mm, _ := strconv.Atoi(parts[1])
				od, _ := time.Parse("2006-01-02", orderDate)
				deadline := time.Date(od.Year(), od.Month(), od.Day(), hh, mm, 0, 0, time.Local)
				if now.After(deadline) {
					e.recordBreach(id, defID, "deadline", def.SLA.Severity,
						fmt.Sprintf("not finished by deadline %s", def.SLA.DeadlineHM),
						def.SLA.WebhookURL)
				}
			}
		}
	}
}

func (e *SLAEngine) recordBreach(instanceID, defID, kind, severity, msg, webhook string) {
	if severity == "" {
		severity = "warning"
	}
	// dedup: 1 breach por (instance, kind)
	var existing int
	_ = e.db.QueryRow(
		`SELECT COUNT(*) FROM sla_breaches WHERE instance_id=? AND kind=?`,
		instanceID, kind,
	).Scan(&existing)
	if existing > 0 {
		return
	}
	id, err := e.db.InsertID(
		`INSERT INTO sla_breaches(instance_id, definition_id, kind, severity, message, detected_at, notified)
		 VALUES(?,?,?,?,?,?,?)`,
		instanceID, defID, kind, severity, msg, time.Now(), 0,
	)
	if err != nil {
		return
	}
	if e.hub != nil {
		e.hub.BroadcastWeb("sla.breach", map[string]any{
			"id": id, "instanceId": instanceID, "definitionId": defID,
			"kind": kind, "severity": severity, "message": msg,
		})
	}
	if webhook != "" {
		go e.notifyWebhook(webhook, map[string]any{
			"event":      "sla.breach",
			"instance":   instanceID,
			"definition": defID,
			"kind":       kind,
			"severity":   severity,
			"message":    msg,
			"timestamp":  time.Now().Format(time.RFC3339),
		})
		_, _ = e.db.Exec(`UPDATE sla_breaches SET notified=1 WHERE id=?`, id)
	}
}

func (e *SLAEngine) notifyWebhook(url string, payload map[string]any) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[sla] webhook %s failed: %v", url, err)
		return
	}
	resp.Body.Close()
}

// ListBreaches retorna últimas N breaches (para dashboard).
func (e *SLAEngine) ListBreaches(limit int) ([]domain.SLABreach, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := e.db.Query(
		`SELECT id, instance_id, definition_id, kind, severity, COALESCE(message,''),
		        detected_at, COALESCE(notified,0)
		 FROM sla_breaches ORDER BY detected_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SLABreach{}
	for rows.Next() {
		var b domain.SLABreach
		var notInt int
		_ = rows.Scan(&b.ID, &b.InstanceID, &b.DefID, &b.Kind, &b.Severity, &b.Message, &b.DetectedAt, &notInt)
		b.Notified = notInt == 1
		out = append(out, b)
	}
	return out, nil
}
