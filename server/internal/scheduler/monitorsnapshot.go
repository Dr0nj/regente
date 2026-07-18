// Package scheduler — M1: imutabilidade TOTAL do Monitoring (2026-07-17).
//
// Tudo que o Monitoring exibe de uma instance vem CONGELADO nela (schemaV18):
// label, job_type, confirm_req, environment, pinned_agent e as condições da
// ordem (conds_in / conds_out_add, JSON arrays com sufixo @odat/@prev/@stat).
// O front nunca mais deriva nada disso da definition viva — renomear/trocar
// tipo/criar jobs novos no Design não reescreve cards já ordenados; a mudança
// só entra numa ordem NOVA (Force pega a def publicada atual; daily seguinte
// idem). Ver docs/monitoring-immutability-plan.md.
package scheduler

import (
	"encoding/json"
	"log"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// monitorCols — valores das colunas congeladas (schemaV18) calculados da def
// NO MOMENTO da ordem. A def aqui já é a normalizada do load (NormalizeConditions
// expandiu upstream legado em condições explícitas), então ConditionsIn/OutAdd
// são o gate real da ordem.
type monitorCols struct {
	label, jobType         string
	confirmReq             int
	environment, pinned    string
	condsIn, condsOutAdd   string
}

func frozenMonitorCols(d domain.JobDefinition) monitorCols {
	return monitorCols{
		label:       d.Label,
		jobType:     d.JobType,
		confirmReq:  boolToInt(d.Confirm),
		environment: d.Environment,
		pinned:      d.AgentID,
		condsIn:     condsJSON(d.ConditionsIn),
		condsOutAdd: condsJSON(producedConds(d)),
	}
}

// producedConds — TODAS as condições que este job adiciona ao pool: OutAdd +
// ações On/Do set-condition. É o conjunto que `producesBase` reconhece (base do
// nome) e o que desenha as linhas produtor→consumidor no Monitoring, cobrindo
// também as arestas on-failure (o pai cria a condição por ação no NOTOK, não por
// OutAdd). Duplicatas são inofensivas (o front casa por base).
func producedConds(d domain.JobDefinition) []string {
	out := append([]string{}, d.ConditionsOutAdd...)
	for _, a := range d.Actions {
		if a.Do == "set-condition" && a.Condition != "" {
			out = append(out, a.Condition)
		}
	}
	return out
}

// condsJSON — lista → JSON array; vazia → '' (a coluna fica no DEFAULT e o
// scan devolve slice vazio sem alocar).
func condsJSON(list []string) string {
	if len(list) == 0 {
		return ""
	}
	b, _ := json.Marshal(list)
	return string(b)
}

const monitorSnapshotFlag = "monitoring-snapshot-v18"

// MigrateMonitoringSnapshot — backfill one-time do schemaV18 (meta_flags).
// Chamar no boot depois de ReloadDefs (precisa das defs vivas p/ dois papéis):
//
//  1. Preenche as colunas congeladas de TODA instance existente (qualquer
//     status — o dia inteiro renderiza certo já no primeiro boot) a partir do
//     definition_snapshot; sem snapshot (legado antigo), congela da def viva
//     UMA vez agora.
//  2. Congela a SEMÂNTICA vigente do lado produtor: até este upgrade o
//     applyConditionsOut aplicava snapshot ∪ def viva; para as instances
//     NÃO-finais (WAITING/HELD/RUNNING) o snapshot é reescrito com essa união
//     — dali em diante o runtime é snapshot-only (ver applyConditionsOut) sem
//     mudar o comportamento de nada que já estava em voo.
func (s *Scheduler) MigrateMonitoringSnapshot() {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM meta_flags WHERE name=?`, monitorSnapshotFlag).Scan(&n); err != nil {
		log.Printf("[monitoring] backfill v18: meta_flags indisponível: %v", err)
		return
	}
	if n > 0 {
		return // já rodou
	}

	s.mu.Lock()
	live := make(map[string]domain.JobDefinition, len(s.defs))
	for _, d := range s.defs {
		live[d.ID] = d
	}
	s.mu.Unlock()

	type row struct{ id, defID, status, snap string }
	var rowsAll []row
	if rows, err := s.db.Query(
		`SELECT id, definition_id, status, COALESCE(definition_snapshot,'') FROM instances WHERE COALESCE(label,'')=''`,
	); err == nil {
		for rows.Next() {
			var r row
			if rows.Scan(&r.id, &r.defID, &r.status, &r.snap) == nil {
				rowsAll = append(rowsAll, r)
			}
		}
		rows.Close()
	} else {
		log.Printf("[monitoring] backfill v18: query: %v", err)
	}

	filled, frozen := 0, 0
	for _, r := range rowsAll {
		var def domain.JobDefinition
		ok := r.snap != "" && json.Unmarshal([]byte(r.snap), &def) == nil && def.ID != ""
		if !ok {
			d, found := live[r.defID]
			if !found {
				continue // sem snapshot e sem def viva: fica no default (front cai no fallback)
			}
			def = d
		}
		// (2) congelamento da união produtor (só não-finais, só com snapshot):
		newSnap := r.snap
		if ok && (r.status == string(domain.StatusWaiting) || r.status == string(domain.StatusHeld) || r.status == string(domain.StatusRunning)) {
			if ld, found := live[def.ID]; found {
				def.ConditionsOutAdd = unionStr(def.ConditionsOutAdd, ld.ConditionsOutAdd)
				def.ConditionsOutRemove = unionStr(def.ConditionsOutRemove, ld.ConditionsOutRemove)
				if b, err := json.Marshal(def); err == nil {
					newSnap = string(b)
					frozen++
				}
			}
		}
		// (1) colunas congeladas — conds_in EXPANDIDO (upstream legado vira
		// condição explícita, mesma régua do gate).
		exp := domain.ExpandSnapshotConditions(def, live)
		c := frozenMonitorCols(exp)
		if _, err := s.db.Exec(
			`UPDATE instances SET label=?, job_type=?, confirm_req=?, environment=?, pinned_agent=?, conds_in=?, conds_out_add=?, definition_snapshot=? WHERE id=?`,
			c.label, c.jobType, c.confirmReq, c.environment, c.pinned, c.condsIn, c.condsOutAdd, newSnap, r.id,
		); err != nil {
			log.Printf("[monitoring] backfill v18: update %s: %v", r.id, err)
			continue
		}
		filled++
	}

	if _, err := s.db.Exec(`INSERT INTO meta_flags(name) VALUES(?)`, monitorSnapshotFlag); err != nil {
		log.Printf("[monitoring] backfill v18: falha ao marcar flag: %v", err)
		return
	}
	if filled > 0 {
		log.Printf("[monitoring] backfill v18: %d instance(s) com colunas congeladas (%d snapshot(s) com união produtor congelada)", filled, frozen)
	}
}
