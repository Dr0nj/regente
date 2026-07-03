// Baterias de teste (Aprofundamento Control-M, 2026-07-03) — cobertura densa de
// calendários complexos, controle de recursos sob concorrência e Forecast ≥1
// semana. Fecha o item "baterias de teste" do roadmap.
package scheduler

import (
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// `day()` helper vive em schedule_preview_test.go (mesmo pacote).

// ── Bateria 1: calendários complexos ──────────────────────────────────────

// include + exclude combinados, holidays, includeDates (exceção que roda mesmo
// não sendo business day) e precedência (exclude > include > holiday > weekday).
func TestBattery_CalendarPrecedence(t *testing.T) {
	cal := &domain.Calendar{
		Name:         "corp",
		BusinessDays: []string{"mon", "tue", "wed", "thu", "fri"},
		Holidays:     []string{"2026-07-09"},   // quinta feriado
		ExcludeDates: []string{"2026-07-07"},   // terça bloqueada à força
		IncludeDates: []string{"2026-07-11"},   // sábado que EXCEPCIONALMENTE roda
	}
	cases := []struct {
		date string
		want bool
		why  string
	}{
		{"2026-07-06", true, "segunda útil"},
		{"2026-07-07", false, "exclude vence business day"},
		{"2026-07-08", true, "quarta útil"},
		{"2026-07-09", false, "holiday"},
		{"2026-07-11", true, "includeDate vence weekend"},
		{"2026-07-12", false, "domingo sem exceção"},
	}
	for _, c := range cases {
		if got := IsEligibleDate(cal, day(c.date)); got != c.want {
			t.Errorf("%s (%s): eligible=%v, esperava %v", c.date, c.why, got, c.want)
		}
	}
}

// N-ésimo dia útil com feriado no meio: o feriado NÃO conta como dia útil, então
// o 3º dia útil "anda" para frente. (Control-M nth business day.) O calendar
// declara Mon-Fri como dias úteis + o feriado, senão weekends contariam.
func TestBattery_NthBusinessDayWithHoliday(t *testing.T) {
	// julho/2026: 01=qua 02=qui 03=sex 04=sáb 05=dom 06=seg 07=ter...
	// úteis Mon-Fri menos feriado 02 → 01,03,06,07 → 3º dia útil = 06 (não 03).
	cal := calMap{"h": {Name: "h", BusinessDays: []string{"mon", "tue", "wed", "thu", "fri"}, Holidays: []string{"2026-07-02"}}}
	def := domain.JobDefinition{
		ID: "nb", Schedule: domain.Schedule{Enabled: true, Frequency: "businessday", NthBusinessDays: []int{3}},
		Calendars: []domain.CalendarRef{{Name: "h", Mode: "include"}},
	}
	if IsScheduledOn(def, day("2026-07-03"), cal) {
		t.Error("com feriado no dia 2, o 3º dia útil NÃO é 03/jul")
	}
	if !IsScheduledOn(def, day("2026-07-06"), cal) {
		t.Error("com feriado no dia 2, o 3º dia útil deveria ser 06/jul")
	}
}

// "último dia útil do mês" (-1) cai antes quando o fim do mês é fim de semana.
func TestBattery_LastBusinessDay(t *testing.T) {
	// julho/2026 termina em 31 (sex). agosto/2026 termina em 31 (seg).
	// maio/2026 termina em 31 (dom) → último dia útil = 29 (sex).
	def := domain.JobDefinition{
		ID: "lb", Schedule: domain.Schedule{Enabled: true, Frequency: "businessday", NthBusinessDays: []int{-1}},
	}
	cal := calMap{}
	if !IsScheduledOn(def, day("2026-07-31"), cal) {
		t.Error("31/jul (sex) deveria ser o último dia útil de julho")
	}
	if IsScheduledOn(def, day("2026-05-31"), cal) {
		t.Error("31/mai é domingo — não é o último dia útil")
	}
	if !IsScheduledOn(def, day("2026-05-29"), cal) {
		t.Error("29/mai (sex) deveria ser o último dia útil de maio")
	}
}

// ── Bateria 2: controle de recursos sob concorrência ──────────────────────

// Capacidade limitada: N pedidos concorrentes, só cabe o teto; libera → o
// próximo cabe. All-or-nothing em pedido multi-recurso.
func TestBattery_ResourceContention(t *testing.T) {
	tr := NewResourceTracker()
	tr.SetCapacity("slots", 2)

	if !tr.TryAcquire("a", map[string]int{"slots": 1}) {
		t.Fatal("1º pedido (1/2) deveria caber")
	}
	if !tr.TryAcquire("b", map[string]int{"slots": 1}) {
		t.Fatal("2º pedido (2/2) deveria caber")
	}
	if tr.TryAcquire("c", map[string]int{"slots": 1}) {
		t.Fatal("3º pedido (3/2) NÃO deveria caber")
	}
	tr.Release("a")
	if !tr.TryAcquire("c", map[string]int{"slots": 1}) {
		t.Fatal("após liberar 'a', o pedido 'c' deveria caber")
	}
}

// All-or-nothing: um pedido de 2 recursos onde só 1 cabe NÃO reserva nenhum
// (não deixa o sistema num meio-estado que furaria a semântica).
func TestBattery_ResourceAllOrNothing(t *testing.T) {
	tr := NewResourceTracker()
	tr.SetCapacity("cpu", 4)
	tr.SetCapacity("lock", 1)
	tr.TryAcquire("holder", map[string]int{"lock": 1}) // ocupa o lock exclusivo

	// quer cpu:2 (cabe) + lock:1 (NÃO cabe) → deve falhar SEM reservar cpu.
	if tr.TryAcquire("x", map[string]int{"cpu": 2, "lock": 1}) {
		t.Fatal("pedido multi-recurso com 1 indisponível deveria falhar")
	}
	var cpuUsed int
	for _, rs := range tr.Snapshot() {
		if rs.Name == "cpu" {
			cpuUsed = rs.Used
		}
	}
	if cpuUsed != 0 {
		t.Fatalf("all-or-nothing violado: cpu reservou %d apesar do pedido falhar", cpuUsed)
	}
	// Shortfalls (read-only, usado pelo Explain) reporta exatamente o lock.
	sf := tr.Shortfalls(map[string]int{"cpu": 2, "lock": 1})
	if len(sf) != 1 || sf[0].Name != "lock" {
		t.Fatalf("Shortfalls deveria apontar só 'lock', veio %+v", sf)
	}
}

// ── Bateria 3: Forecast ≥ 1 semana ────────────────────────────────────────

// Prevê a semana inteira pela FONTE ÚNICA de agendamento (IsScheduledOn, a mesma
// que a daily usa): um job diário roda 7×, um job seg-sex roda 5×. Cobre "prever
// ≥1 semana" com a regra real, não a estimativa simplificada do Forecast().
func TestBattery_ForecastOneWeekViaSchedule(t *testing.T) {
	cal := calMap{}
	diario := domain.JobDefinition{ID: "diario", Schedule: domain.Schedule{Enabled: true, Frequency: "daily"}}
	segsex := domain.JobDefinition{ID: "segsex", Schedule: domain.Schedule{
		Enabled: true, Frequency: "weekly", DaysOfWeek: []string{"mon", "tue", "wed", "thu", "fri"}}}

	var diarioRuns, segsexRuns int
	weekend := map[string]bool{"2026-07-11": true, "2026-07-12": true}
	for d := 0; d < 7; d++ {
		date := day("2026-07-06").AddDate(0, 0, d) // seg 06 .. dom 12
		iso := date.Format("2006-01-02")
		if IsScheduledOn(diario, date, cal) {
			diarioRuns++
		}
		got := IsScheduledOn(segsex, date, cal)
		if got {
			segsexRuns++
		}
		if got == weekend[iso] { // seg-sex deve rodar; fim de semana não
			t.Errorf("%s: segsex agendado=%v (esperava %v)", iso, got, !weekend[iso])
		}
	}
	if diarioRuns != 7 {
		t.Errorf("job diário deveria rodar 7× na semana, rodou %d", diarioRuns)
	}
	if segsexRuns != 5 {
		t.Errorf("job seg-sex deveria rodar 5× na semana, rodou %d", segsexRuns)
	}
}

// Forecast atribui ondas topológicas por dependência (A→B→C = 3 ondas) — a
// previsão respeita a ordem de execução, não só a elegibilidade.
func TestBattery_ForecastTopologicalWaves(t *testing.T) {
	defs := []domain.JobDefinition{
		{ID: "A", Schedule: domain.Schedule{Enabled: true}},
		{ID: "B", Schedule: domain.Schedule{Enabled: true}, Upstream: []domain.Upstream{{From: "A", Condition: domain.CondOnSuccess}}},
		{ID: "C", Schedule: domain.Schedule{Enabled: true}, Upstream: []domain.Upstream{{From: "B", Condition: domain.CondOnSuccess}}},
	}
	rep := Forecast(defs, nil, "2026-07-06")
	wave := map[string]int{}
	for _, j := range rep.Jobs {
		wave[j.DefID] = j.Wave
	}
	if !(wave["A"] == 0 && wave["B"] == 1 && wave["C"] == 2) {
		t.Fatalf("ondas topológicas erradas: A=%d B=%d C=%d (esperava 0/1/2)", wave["A"], wave["B"], wave["C"])
	}
}
