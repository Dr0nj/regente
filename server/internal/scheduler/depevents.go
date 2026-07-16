// Package scheduler — eventos de dependência com CONSUMO por instância.
//
// Modelo (2026-07-13, report do usuário; ver schemaV15):
//
//   - Todo término TERMINAL de uma instance (OK/NOTOK pós-retries, Set OK)
//     publica um EVENTO imutável em dep_events. Rerun do pai = evento NOVO.
//   - A satisfação de uma aresta upstream é um CLAIM (latch) do consumidor
//     sobre um evento livre: dep_claims. Uma vez clamado, o latch NUNCA é
//     desfeito por rerun/cancel do PAI — a "linha verde" do Monitoring é a
//     história daquela instance, não o estado vivo do upstream.
//   - O consumo só se MATERIALIZA quando o consumidor termina OK. Se o
//     consumidor FALHA (NOTOK terminal) ou é CANCELADO, os eventos que ele
//     clamou VOLTAM pro pool (a condição não foi gasta por uma execução que
//     não aconteceu de verdade) — um clone pendente ou o rerun re-clama.
//   - UNIQUE(event_id, consumer_def_id): um evento satisfaz NO MÁXIMO uma
//     instance de cada definition consumidora — a cópia forçada de um job cuja
//     dependência "já rodou" NÃO herda o evento consumido pela instance
//     original; entra em WAIT EVENT até o pai emitir um término novo.
//   - Rerun do CONSUMIDOR depende do run anterior ter CONSUMIDO ou não
//     (RerunDepClaims): se terminou OK (real ou Set OK), o consumo é
//     PERMANENTE — o claim vira LÁPIDE e o evento segue gasto para esta
//     definition; o novo run entra em WAIT EVENT até o pai emitir um término
//     NOVO. Se NÃO consumiu (rerun de RUNNING/HELD; NOTOK/CANCELLED já
//     devolveram no término), os claims são deletados e os eventos voltam
//     pro pool. É o "delete input conditions on OK" do Control-M: a condição
//     que liberou o job SOME quando ele executa com sucesso.
//   - DATAS (2026-07-16, ver odate.go): o evento nasce com o ODAT do produtor
//     (a origem da ordem — o carry-over avança order_date, nunca o ODAT) e o
//     claim exige a diária resolvida pelo dateRef da aresta contra o ODAT do
//     CONSUMIDOR: odat (default) = mesma origem · prev = diária anterior ·
//     stat = qualquer. Um consumidor carregado do dia 14 disputa os eventos
//     DO DIA 14 — nunca os dos jobs frescos de hoje (report do usuário).
//
// Compat/backfill: instances terminadas ANTES da feature não têm eventos.
// tryClaimEdge materializa lazy o evento implícito (1 por instance terminal sem
// evento) e dá o claim primeiro aos consumidores que JÁ RODARAM (consumo
// histórico) — só então o pool fica visível para instances novas.
package scheduler

import (
	"log"
	"strings"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// ForceModeOrder — instances.force_mode do "Order Force" (Design): ordem NOVA
// fora do agendamento que RESPEITA os gates de runtime (deps por evento,
// conditions, agente, recursos, confirm). "" = "Run Now" clássico (bypass).
const ForceModeOrder = "order"

// edgeWants — statuses de dep_event que satisfazem a condição da aresta.
// nil = a condição não consome evento (always: basta o upstream existir).
func edgeWants(cond domain.EdgeCondition) []string {
	switch cond {
	case domain.CondOnFailure:
		return []string{string(domain.StatusNotOK)}
	case domain.CondOnComplete:
		return []string{string(domain.StatusOK), string(domain.StatusNotOK)}
	case domain.CondAlways:
		return nil
	default: // on-success e "" (default do produto — igual ao front)
		return []string{string(domain.StatusOK)}
	}
}

// emitDepEvent — publica o evento de dependência de um término terminal.
// Best-effort: falha é logada, nunca quebra o fluxo de finish.
// A data do evento é o ODAT do produtor (origem, NÃO o order_date avançado
// pelo carry-over): um pai carregado do dia 14 que termina hoje satisfaz o
// consumidor DO DIA 14, não o fresco de hoje.
func (s *Scheduler) emitDepEvent(instanceID string, status domain.InstanceStatus) {
	var defID, orderDate string
	if err := s.db.QueryRow(
		`SELECT definition_id, `+odateExpr+` FROM instances WHERE id=?`, instanceID,
	).Scan(&defID, &orderDate); err != nil {
		return
	}
	if _, err := s.db.Exec(
		`INSERT INTO dep_events(def_id, instance_id, order_date, status) VALUES(?,?,?,?)`,
		defID, instanceID, orderDate, string(status),
	); err != nil {
		log.Printf("[scheduler] dep-event %s (%s): %v", instanceID, status, err)
	}
}

// ResetDepClaims — devolve pro pool os eventos clamados por uma instance
// (as linhas dela resetam). Semântica de "não gastou": término NOTOK terminal
// (FinishInstance — falha não consome) e cancel do consumidor (handlers).
// Rerun NÃO passa por aqui — usa RerunDepClaims (consumo OK é permanente).
func (s *Scheduler) ResetDepClaims(instanceID string) {
	if _, err := s.db.Exec(`DELETE FROM dep_claims WHERE consumer_instance_id=?`, instanceID); err != nil {
		log.Printf("[scheduler] reset dep-claims %s: %v", instanceID, err)
	}
}

// RerunDepClaims — rerun do CONSUMIDOR (chamar ANTES de resetar o status, com
// o status TERMINAL ainda gravado). Regra do usuário (2026-07-14):
//
//   - Run anterior OK (real ou Set OK) → o consumo MATERIALIZOU e é
//     PERMANENTE. O claim vira LÁPIDE: o consumer_instance_id ganha o sufixo
//     "#spent@<event>" — some do claimsFor/DepsSatisfied (a linha do novo run
//     reseta), mas o UNIQUE(event_id, consumer_def_id) continua bloqueando o
//     evento para esta definition. O novo run entra em WAIT EVENT até o pai
//     emitir um término NOVO. (Deletar aqui era o bug: o evento gasto voltava
//     pro pool e o próprio rerun o re-clamava e rodava na hora.)
//   - Qualquer outro status → não consumiu: claims deletados, eventos de
//     volta ao pool (NOTOK/CANCELLED em geral já devolveram no término).
func (s *Scheduler) RerunDepClaims(instanceID string) {
	var status string
	_ = s.db.QueryRow(`SELECT status FROM instances WHERE id=?`, instanceID).Scan(&status)
	if status != string(domain.StatusOK) {
		s.ResetDepClaims(instanceID)
		return
	}
	if _, err := s.db.Exec(
		`UPDATE dep_claims SET consumer_instance_id = consumer_instance_id || '#spent@' || event_id
		 WHERE consumer_instance_id=?`, instanceID,
	); err != nil {
		log.Printf("[scheduler] tombstone dep-claims %s: %v", instanceID, err)
	}
}

// claimsFor — latches existentes de um consumidor: upstream_def_id → true.
func (s *Scheduler) claimsFor(instanceID string) map[string]bool {
	out := map[string]bool{}
	rows, err := s.db.Query(`SELECT upstream_def_id FROM dep_claims WHERE consumer_instance_id=?`, instanceID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var up string
		if rows.Scan(&up) == nil {
			out[up] = true
		}
	}
	return out
}

// loadDayClaims — todos os latches do dia numa query (hot path do tick):
// consumer_instance_id → {upstream_def_id → true}.
func (s *Scheduler) loadDayClaims(date string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	rows, err := s.db.Query(
		`SELECT c.consumer_instance_id, c.upstream_def_id
		 FROM dep_claims c JOIN instances i ON i.id = c.consumer_instance_id
		 WHERE i.order_date=?`, date,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var cid, up string
		if rows.Scan(&cid, &up) != nil {
			continue
		}
		m := out[cid]
		if m == nil {
			m = map[string]bool{}
			out[cid] = m
		}
		m[up] = true
	}
	return out
}

// DepsSatisfied — export p/ API: upstream defs já latchados por um consumidor.
func (s *Scheduler) DepsSatisfied(instanceID string) []string {
	m := s.claimsFor(instanceID)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// tryClaimEdge — tenta CLAMAR um evento livre do upstream para o consumidor.
// Retorna true se a aresta ficou satisfeita (claim novo). Idempotente sob
// corrida: os UNIQUEs de dep_claims decidem; conflito = tenta o próximo evento.
//
// DATAS (2026-07-16): `wantDate` é a diária do PAI que satisfaz a aresta, já
// resolvida pelo dateRef contra o ODAT do consumidor (odat = mesma origem;
// prev = diária anterior; stat = anyDate, sem filtro). `consumerOdate` escopa
// o consumo histórico (irmãos da MESMA origem disputam os mesmos eventos).
//
// Passos (todos idempotentes e no-op quando não há nada a fazer):
//  1. backfill lazy: instance terminal do upstream SEM evento (pré-feature ou
//     status escrito por fora do FinishInstance) ganha seu evento implícito;
//  2. consumo histórico: instances do MESMO consumer_def que JÁ rodaram sem
//     claim herdam os eventos mais antigos (elas consumiram de fato);
//  3. claim de verdade para ESTE consumidor no evento livre mais antigo.
func (s *Scheduler) tryClaimEdge(consumerID, consumerDefID, upstreamDefID, wantDate string, anyDate bool, consumerOdate string, cond domain.EdgeCondition) bool {
	wants := edgeWants(cond)
	if wants == nil {
		return false // always não consome evento (caller decide pela existência)
	}

	// 1) backfill lazy de eventos implícitos (terminais sem evento). A data do
	// evento implícito é o ODAT da instance (origem), como no emitDepEvent.
	backfillSQL := `INSERT INTO dep_events(def_id, instance_id, order_date, status)
		 SELECT definition_id, id, ` + odateExpr + `, status FROM instances i
		 WHERE i.definition_id=? AND i.status IN (?,?)
		   AND NOT EXISTS (SELECT 1 FROM dep_events e WHERE e.instance_id = i.id)`
	backfillArgs := []any{upstreamDefID, string(domain.StatusOK), string(domain.StatusNotOK)}
	if !anyDate {
		backfillSQL += ` AND ` + odateExpr + `=?`
		backfillArgs = append(backfillArgs, wantDate)
	}
	if _, err := s.db.Exec(backfillSQL, backfillArgs...); err != nil {
		log.Printf("[scheduler] dep backfill %s@%s: %v", upstreamDefID, wantDate, err)
	}

	// 2) consumo histórico: quem RODOU e não falhou (RUNNING/OK) consumiu um
	// evento na época — recebe o claim antes de qualquer instance nova. NOTOK e
	// CANCELLED ficam de fora DE PROPÓSITO: falha não consome (o término NOTOK
	// devolve o evento — ver FinishInstance), então o backfill não pode entregar
	// o evento de volta ao job falho; Set OK reconverte em OK e aí sim consome
	// (lazy, na próxima disputa).
	if rows, err := s.db.Query(
		`SELECT id FROM instances
		 WHERE definition_id=? AND `+odateExpr+`=? AND id<>?
		   AND status IN (?,?)
		   AND NOT EXISTS (SELECT 1 FROM dep_claims c WHERE c.consumer_instance_id = instances.id AND c.upstream_def_id=?)
		 ORDER BY COALESCE(started_at, scheduled_at), id`,
		consumerDefID, consumerOdate, consumerID,
		string(domain.StatusRunning), string(domain.StatusOK), upstreamDefID,
	); err == nil {
		var started []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				started = append(started, id)
			}
		}
		rows.Close()
		for _, id := range started {
			s.claimOldestFree(id, consumerDefID, upstreamDefID, wantDate, anyDate, wants)
		}
	}

	// 3) claim para ESTE consumidor.
	return s.claimOldestFree(consumerID, consumerDefID, upstreamDefID, wantDate, anyDate, wants)
}

// claimOldestFree — clama o evento livre mais antigo do upstream que case os
// statuses da aresta (e a diária resolvida, salvo anyDate/stat). Conflito de
// UNIQUE (corrida) tenta o próximo candidato.
func (s *Scheduler) claimOldestFree(consumerID, consumerDefID, upstreamDefID, wantDate string, anyDate bool, wants []string) bool {
	ph := placeholdersN(len(wants))
	dateFilter := ` AND e.order_date=?`
	args := []any{upstreamDefID}
	if anyDate {
		dateFilter = ``
	} else {
		args = append(args, wantDate)
	}
	for _, w := range wants {
		args = append(args, w)
	}
	args = append(args, consumerDefID)
	rows, err := s.db.Query(
		`SELECT e.id FROM dep_events e
		 WHERE e.def_id=?`+dateFilter+` AND e.status IN (`+ph+`)
		   AND NOT EXISTS (SELECT 1 FROM dep_claims c WHERE c.event_id = e.id AND c.consumer_def_id=?)
		 ORDER BY e.id LIMIT 20`,
		args...,
	)
	if err != nil {
		return false
	}
	var candidates []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			candidates = append(candidates, id)
		}
	}
	rows.Close()
	for _, ev := range candidates {
		if _, err := s.db.Exec(
			`INSERT INTO dep_claims(event_id, consumer_instance_id, consumer_def_id, upstream_def_id) VALUES(?,?,?,?)`,
			ev, consumerID, consumerDefID, upstreamDefID,
		); err == nil {
			return true
		}
		// UNIQUE bateu: ou outra instance levou o evento (tenta o próximo), ou
		// ESTE consumidor já tem claim nesta aresta (satisfeita — corrida benigna).
		if s.claimsFor(consumerID)[upstreamDefID] {
			return true
		}
	}
	return false
}

func placeholdersN(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// claimDepEdges — pré-passe do tick: tenta latchar TODAS as arestas pendentes
// de uma instance WAITING/HELD, ANTES do gating por janela — assim a instance
// do dia "reserva" o evento do pai assim que ele termina, mesmo que ainda
// esteja esperando o horário (uma cópia forçada depois não rouba o evento
// dela). Broadcast em claim novo pro Monitoring pintar a linha na hora.
// Ordem de chamada (tick): daily primeiro, forçadas depois — ver tickOnce.
func (s *Scheduler) claimDepEdges(r instRow, def domain.JobDefinition, claimed map[string]bool) bool {
	if len(def.Upstream) == 0 {
		return false
	}
	newClaim := false
	odate := r.Odate()
	for _, u := range def.Upstream {
		if claimed[u.From] {
			continue
		}
		// Data resolvida pelo dateRef da aresta contra o ODAT do consumidor —
		// a origem, não o order_date avançado pelo carry-over.
		wantDate, anyDate := s.resolveEdgeDate(odate, u.DateRef)
		if s.tryClaimEdge(r.ID, def.ID, u.From, wantDate, anyDate, odate, u.Condition) {
			if claimed != nil {
				claimed[u.From] = true
			}
			newClaim = true
		}
	}
	if newClaim {
		s.hub.BroadcastWeb("instance.changed", map[string]string{"id": r.ID, "status": r.Status})
	}
	return newClaim
}
