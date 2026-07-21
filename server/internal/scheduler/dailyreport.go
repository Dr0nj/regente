// E5 — Relatório/SLO da daily: o resumo que a operação cobra no fim do dia.
//
// Duas superfícies:
//   - GET /api/daily/report?date=YYYY-MM-DD → BuildDailyReport (1 query
//     agregada em instances + daily_runs + sla_breaches do dia).
//   - Push opcional: setting `daily_report_channels` (CSV slack/webhook/email/
//     pagerduty — REUSA os sinks do alerting via AlertEngine.FireAction).
//     Enviado quando a daily FECHA (nenhuma instance WAITING/RUNNING; checado
//     no tick com throttle de 1 min) OU no horário `daily_report_at` (HH:MM na
//     timezone da daily), o que vier primeiro. Idempotente: o claim é
//     UPDATE daily_runs SET report_sent_at WHERE report_sent_at IS NULL —
//     1 envio por diária mesmo com múltiplos nós.
package scheduler

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// DailyReportCounts — contagens por estado da diária.
type DailyReportCounts struct {
	Ordered   int `json:"ordered"` // total de instances do order_date
	OK        int `json:"ok"`
	NotOK     int `json:"notok"`
	Waiting   int `json:"waiting"`
	Running   int `json:"running"`
	Held      int `json:"held"`
	Cancelled int `json:"cancelled"`
	Carried   int `json:"carried"` // vieram de diárias anteriores (carry-over)
}

// DailyReportFailure — um job que terminou NOTOK (cap 100 no relatório).
type DailyReportFailure struct {
	DefID      string     `json:"defId"`
	Team       string     `json:"team,omitempty"`
	ExitCode   int        `json:"exitCode"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// DailyReport — o agregado do dia.
type DailyReport struct {
	Date      string     `json:"date"`
	DailyAt   string     `json:"dailyAt"`            // horário configurado (HH:MM)
	Timezone  string     `json:"timezone,omitempty"` // E1 — tz de negócio ("" = local)
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// LateStart — a daily materializou mais de 5 min depois do horário
	// configurado (SLO de pontualidade do próprio scheduler).
	LateStart bool `json:"lateStart"`
	// Closed — nenhuma instance WAITING/RUNNING: o dia "fechou" (é o gatilho
	// do push; informativo pro card da UI).
	Closed      bool                 `json:"closed"`
	Counts      DailyReportCounts    `json:"counts"`
	Failures    []DailyReportFailure `json:"failures"`
	SLABreaches []domain.SLABreach   `json:"slaBreaches"`
	ReportSent  bool                 `json:"reportSent"` // push já foi (report_sent_at)
}

// lateStartGrace — tolerância entre o horário configurado e o started_at real.
const lateStartGrace = 5 * time.Minute

// dailyReportFailureCap — teto de failures listadas (o count é sempre exato).
const dailyReportFailureCap = 100

// IsDailyLate — a daily de `date` materializou TARDE? started_at depois do horário
// configurado (DailyAt) + tolerância, na timezone de negócio. FONTE ÚNICA da
// pontualidade da daily: usado pelo BuildDailyReport (card/report) e pelo
// /api/daily/status (indicador do rodapé).
func (s *Scheduler) IsDailyLate(date string, startedAt time.Time) bool {
	hh, mm, ok := parseHHMM(s.DailyAt())
	if !ok {
		return false
	}
	_, loc := s.DailyTimezone()
	d, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return false
	}
	target := time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, loc)
	return startedAt.After(target.Add(lateStartGrace))
}

// BuildDailyReport agrega o estado da diária `date` (YYYY-MM-DD).
func (s *Scheduler) BuildDailyReport(date string) (*DailyReport, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, fmt.Errorf("data inválida %q (use YYYY-MM-DD)", date)
	}
	rep := &DailyReport{
		Date:     date,
		DailyAt:  s.DailyAt(),
		Failures: []DailyReportFailure{},
	}
	tzName, _ := s.DailyTimezone()
	rep.Timezone = tzName

	// daily_runs: quando materializou + push já enviado.
	var started, sent sql.NullTime
	err := s.db.QueryRow(
		`SELECT started_at, report_sent_at FROM daily_runs WHERE order_date=?`, date,
	).Scan(&started, &sent)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if started.Valid {
		t := started.Time
		rep.StartedAt = &t
		rep.LateStart = s.IsDailyLate(date, t)
	}
	rep.ReportSent = sent.Valid

	// Contagens por status + carried, em 1 passada agregada.
	rows, err := s.db.Query(
		`SELECT status, COUNT(*), COALESCE(SUM(CASE WHEN COALESCE(carried_from,'') <> '' THEN 1 ELSE 0 END), 0)
		 FROM instances WHERE order_date=? GROUP BY status`, date,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st string
		var n, carried int
		if rows.Scan(&st, &n, &carried) != nil {
			continue
		}
		rep.Counts.Ordered += n
		rep.Counts.Carried += carried
		switch st {
		case string(domain.StatusOK):
			rep.Counts.OK = n
		case string(domain.StatusNotOK):
			rep.Counts.NotOK = n
		case string(domain.StatusWaiting):
			rep.Counts.Waiting = n
		case string(domain.StatusRunning):
			rep.Counts.Running = n
		case string(domain.StatusHeld):
			rep.Counts.Held = n
		case string(domain.StatusCancelled):
			rep.Counts.Cancelled = n
		}
	}
	errIter := rows.Err()
	rows.Close()
	if errIter != nil {
		return nil, errIter // report com contagem parcial mentiria no e-mail do dia
	}
	rep.Closed = rep.Counts.Waiting == 0 && rep.Counts.Running == 0

	// Failures (NOTOK) — detalhe cap 100; o total exato já está em Counts.NotOK.
	frows, err := s.db.Query(
		`SELECT definition_id, COALESCE(team,''), COALESCE(exit_code,0), finished_at
		 FROM instances WHERE order_date=? AND status=? ORDER BY finished_at LIMIT ?`,
		date, string(domain.StatusNotOK), dailyReportFailureCap,
	)
	if err != nil {
		return nil, err
	}
	for frows.Next() {
		var f DailyReportFailure
		var fin sql.NullTime
		if frows.Scan(&f.DefID, &f.Team, &f.ExitCode, &fin) != nil {
			continue
		}
		if fin.Valid {
			t := fin.Time
			f.FinishedAt = &t
		}
		rep.Failures = append(rep.Failures, f)
	}
	errIter = frows.Err()
	frows.Close()
	if errIter != nil {
		return nil, errIter
	}

	// SLA breaches das instances DO DIA (direto da tabela — funciona mesmo sem
	// o SLAEngine atachado; cap igual ao de failures).
	rep.SLABreaches = []domain.SLABreach{}
	brows, err := s.db.Query(
		`SELECT b.id, b.instance_id, b.definition_id, b.kind, b.severity, COALESCE(b.message,''), b.detected_at
		 FROM sla_breaches b JOIN instances i ON i.id = b.instance_id
		 WHERE i.order_date=? ORDER BY b.detected_at LIMIT ?`, date, dailyReportFailureCap,
	)
	if err == nil {
		for brows.Next() {
			var b domain.SLABreach
			if brows.Scan(&b.ID, &b.InstanceID, &b.DefID, &b.Kind, &b.Severity, &b.Message, &b.DetectedAt) != nil {
				continue
			}
			rep.SLABreaches = append(rep.SLABreaches, b)
		}
		errIter = brows.Err()
		brows.Close()
		if errIter != nil {
			return nil, errIter
		}
	}
	return rep, nil
}

// maybeSendDailyReport — chamado a cada Tick (já leader-gated), com throttle
// interno de 1 min. Envia o relatório da diária de HOJE quando devido.
func (s *Scheduler) maybeSendDailyReport() {
	s.mu.Lock()
	if time.Since(s.lastReportCheck) < time.Minute {
		s.mu.Unlock()
		return
	}
	s.lastReportCheck = time.Now()
	s.mu.Unlock()

	channels := strings.TrimSpace(s.setting("daily_report_channels"))
	if channels == "" {
		return // push desligado (default)
	}
	date := s.TodayDate()
	var started, sent sql.NullTime
	if err := s.db.QueryRow(
		`SELECT started_at, report_sent_at FROM daily_runs WHERE order_date=?`, date,
	).Scan(&started, &sent); err != nil {
		return // daily de hoje ainda não materializou
	}
	if sent.Valid {
		return // já enviado (idempotência)
	}

	due := false
	var open int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM instances WHERE order_date=? AND status IN (?,?)`,
		date, string(domain.StatusWaiting), string(domain.StatusRunning),
	).Scan(&open); err == nil && open == 0 {
		due = true // a daily FECHOU
	}
	if !due {
		// Fallback por horário (cobre dias que nunca fecham, ex. cyclic o dia
		// todo): daily_report_at HH:MM na timezone da daily.
		if hh, mm, ok := parseHHMM(strings.TrimSpace(s.setting("daily_report_at"))); ok {
			now := s.NowLocal()
			at := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
			due = !now.Before(at)
		}
	}
	if !due {
		return
	}

	// Claim atômico: só quem transicionar NULL→now envia (1 por diária, mesmo
	// com vários nós checando).
	res, err := s.db.Exec(
		`UPDATE daily_runs SET report_sent_at=CURRENT_TIMESTAMP WHERE order_date=? AND report_sent_at IS NULL`, date,
	)
	if err != nil {
		log.Printf("[scheduler] daily report %s: claim: %v", date, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return // outro nó levou
	}
	rep, err := s.BuildDailyReport(date)
	if err != nil {
		log.Printf("[scheduler] daily report %s: build: %v", date, err)
		return
	}
	s.sendDailyReport(rep, channels)
}

// setting lê um valor da tabela settings ("" se ausente).
func (s *Scheduler) setting(key string) string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

// sendDailyReport entrega o resumo nos canais configurados via o MESMO caminho
// dos alertas (persistência em alert_events + sino da UI + sinks externos).
func (s *Scheduler) sendDailyReport(rep *DailyReport, channelsCSV string) {
	var chans []string
	for _, c := range strings.Split(channelsCSV, ",") {
		if t := strings.TrimSpace(strings.ToLower(c)); t != "" {
			chans = append(chans, t)
		}
	}
	severity, verdict := "info", "fechada"
	if rep.Counts.NotOK > 0 {
		severity, verdict = "warning", fmt.Sprintf("fechada com %d falha(s)", rep.Counts.NotOK)
	}
	if !rep.Closed {
		verdict = "parcial (ainda há jobs abertos)"
	}
	late := ""
	if rep.LateStart {
		late = " · INÍCIO ATRASADO"
	}
	msg := fmt.Sprintf("Daily %s %s: %d ordenadas · %d OK · %d NOTOK · %d aguardando · %d rodando · %d canceladas · %d carregadas%s",
		rep.Date, verdict, rep.Counts.Ordered, rep.Counts.OK, rep.Counts.NotOK,
		rep.Counts.Waiting, rep.Counts.Running, rep.Counts.Cancelled, rep.Counts.Carried, late)
	if len(rep.Failures) > 0 {
		names := make([]string, 0, 5)
		for i, f := range rep.Failures {
			if i == 5 {
				names = append(names, "…")
				break
			}
			names = append(names, f.DefID)
		}
		msg += " · falhas: " + strings.Join(names, ", ")
	}
	log.Printf("[scheduler] daily report %s enviado (%s): %s", rep.Date, channelsCSV, msg)
	if s.alerts != nil {
		s.alerts.FireAction("daily-report", "Daily "+rep.Date, "Relatório da daily", severity, msg, chans)
	}
}
