package scheduler

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
)

// Motivo legível de NÃO haver agente elegível para um job na execução durável.
//
// Por que existe (produção, 2026-11-13): o único agente da VPS tinha 4 slots e
// 4 execuções UNCERTAIN desde três dias antes. Por desenho, UNCERTAIN retém o
// slot até resolução auditada — então NADA era despachado, e a única pista era
// "no eligible agent with matching identity, capabilities and available
// admission capacity". A diária inteira (28,8 mil jobs) ficou WAITING sem que o
// produto dissesse o que o operador precisava fazer.
//
// durableAgent decide; esta função só EXPLICA, e só é chamada quando ele já
// recusou. Reaplica os mesmos critérios, um por vez, para nomear o que falhou.

const genericAgentBlock = "no eligible agent with matching identity, capabilities and available admission capacity"

// agentBlockTTL — a explicação é pedida por instance em espera a cada tick; o
// motivo é o mesmo para todo job do mesmo (agente pinado, tipo).
const agentBlockTTL = 5 * time.Second

type agentBlockEntry struct {
	at     time.Time
	detail string
}

var agentBlockCache = struct {
	sync.Mutex
	by map[*Scheduler]map[string]agentBlockEntry
}{by: map[*Scheduler]map[string]agentBlockEntry{}}

func (s *Scheduler) agentBlockReason(def domain.JobDefinition) string {
	key := def.AgentID + "|" + strings.ToUpper(def.JobType)
	now := time.Now()
	agentBlockCache.Lock()
	if e, ok := agentBlockCache.by[s][key]; ok && now.Sub(e.at) < agentBlockTTL {
		agentBlockCache.Unlock()
		return e.detail
	}
	agentBlockCache.Unlock()
	detail := s.computeAgentBlockReason(def)
	agentBlockCache.Lock()
	if agentBlockCache.by[s] == nil {
		agentBlockCache.by[s] = map[string]agentBlockEntry{}
	}
	agentBlockCache.by[s][key] = agentBlockEntry{now, detail}
	agentBlockCache.Unlock()
	return detail
}

func (s *Scheduler) computeAgentBlockReason(def domain.JobDefinition) string {
	jobType := strings.ToUpper(def.JobType)
	if jobType == "SSH" || def.AgentID == "SERVER-AGENT" {
		if s.internalAgentID == "" {
			return "this job runs on the built-in SERVER-AGENT, which is disabled on this server (-server-agent=false); enable it or pin the job to another agent"
		}
		return genericAgentBlock
	}
	rows, err := s.db.Query("SELECT p.agent_id,p.capabilities,p.internal,COALESCE(c.slots,0),COALESCE(c.pending_limit,0),COALESCE(c.available,0),COALESCE(c.last_seen,0),"+
		"(SELECT COUNT(*) FROM execution_attempts a WHERE a.agent_id=p.agent_id AND a.state NOT IN ('succeeded','failed','cancelled')),"+
		"(SELECT COUNT(*) FROM execution_attempts a WHERE a.agent_id=p.agent_id AND a.state NOT IN ('succeeded','failed','cancelled') AND a.state='uncertain'),"+
		"(SELECT COUNT(*) FROM agent_tokens t WHERE t.agent_id=p.agent_id AND t.revoked_at=0 AND (t.rotated_at=0 OR t.expires_at>?)) "+
		"FROM machine_principals p LEFT JOIN execution_agent_capacity c ON c.agent_id=p.agent_id ORDER BY p.agent_id", time.Now().UnixMilli())
	if err != nil {
		return genericAgentBlock
	}
	defer rows.Close()
	fresh := time.Now().Add(-15 * time.Second).UnixMilli()
	var full, offline, credential string
	matched := false
	for rows.Next() {
		var id, caps string
		var internal bool
		var slots, pending, available, seen, active, uncertain, tokens int64
		if err = rows.Scan(&id, &caps, &internal, &slots, &pending, &available, &seen, &active, &uncertain, &tokens); err != nil {
			return genericAgentBlock
		}
		if internal && id != s.internalAgentID {
			continue
		}
		if def.AgentID != "" && def.AgentID != id {
			continue
		}
		values := strings.Split(caps, ",")
		if !slices.Contains(values, execution.Capability) || !slices.Contains(values, jobType) {
			continue
		}
		matched = true
		switch {
		case available != 1 || seen <= fresh:
			if offline == "" {
				offline = fmt.Sprintf("agent '%s' is offline or not reporting capacity", id)
			}
		case !internal && tokens == 0:
			if credential == "" {
				credential = fmt.Sprintf("agent '%s' has no valid credential (token revoked or expired)", id)
			}
		case active >= slots || active >= pending:
			if full != "" {
				continue
			}
			limit := slots
			if pending < limit {
				limit = pending
			}
			full = fmt.Sprintf("agent '%s' has no free slot: %d of %d in use", id, active, limit)
			if uncertain > 0 {
				full += fmt.Sprintf(", %d by UNCERTAIN executions that keep their slot until an operator resolves them (open each UNCERTAIN job, Execution tab)", uncertain)
			}
		}
	}
	if rows.Err() != nil {
		return genericAgentBlock
	}
	// Ordem: o que o operador consegue destravar primeiro.
	for _, detail := range []string{full, offline, credential} {
		if detail != "" {
			return detail
		}
	}
	if !matched {
		if def.AgentID != "" {
			return fmt.Sprintf("agent '%s' is not registered for durable execution or does not provide %s", def.AgentID, jobType)
		}
		return "no registered agent provides " + jobType + " with durable execution"
	}
	return genericAgentBlock
}
