// Baterias EXAUSTIVAS do Aprofundamento Control-M (CTM-4/5/6, 2026-07-08).
//
// Fecham os itens de VALIDAÇÃO residual do roadmap: as features (calendários,
// recursos/quotas, forecast) já existiam; aqui cobrimos densamente todas as
// combinações contra a FONTE ÚNICA de gating (IsScheduledOn / ResourceTracker /
// Forecast) e travamos os pontos onde dois caminhos poderiam divergir.
//
//   CTM-4 — Calendários complexos: 1º dia útil · só segundas · 1º dia útil que
//           NÃO é segunda · N-ésimo dia útil · include/exclude · feriados · meses.
//   CTM-5 — Controle de recursos: quantitative · lock exclusivo · máximo
//           simultâneo por pool · fila quando esgota · liberação correta.
//   CTM-6 — Forecast ≥1 semana à frente contra o gating real (calendars+deps+
//           recursos), incluindo a correção da divergência do Forecast() que
//           só olhava o calendar legado e ignorava frequência/meses.
//
// Helpers `day()` (schedule_preview_test.go) e `calMap` (shift_test.go) reusados.
package scheduler

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// mesesJul2026 — referência de dias da semana usada nas asserções:
//
//	01 qua 02 qui 03 sex 04 sáb 05 dom 06 seg 07 ter 08 qua 09 qui 10 sex
//	11 sáb 12 dom 13 seg ... 31 sex. 1º dia útil = 01(qua); último = 31(sex).
//
// Junho/2026 começa numa SEGUNDA (01=seg) — 1º dia útil É segunda, o caso duro
// do "1º dia útil que não é segunda".

// ─────────────────────────────────────────────────────────────────────────────
// CTM-4 — Calendários complexos
// ─────────────────────────────────────────────────────────────────────────────

// (a) 1º dia útil do mês — advanced:first-businessday. Sem feriado o 1º útil de
// julho é 01(qua); com feriado em 01, "anda" para 02(qui).
func TestCTM4_FirstBusinessDay(t *testing.T) {
	plain := domain.JobDefinition{ID: "fbd",
		Schedule: domain.Schedule{Enabled: true, Frequency: "advanced", AdvancedRule: "first-businessday"}}
	empty := calMap{}
	if !IsScheduledOn(plain, day("2026-07-01"), empty) {
		t.Error("01/jul (qua) deveria ser o 1º dia útil de julho")
	}
	if IsScheduledOn(plain, day("2026-07-02"), empty) {
		t.Error("02/jul NÃO é o 1º dia útil")
	}

	// Com feriado em 01/jul (calendar include declarando Mon-Fri + o feriado),
	// o 1º dia útil passa a ser 02/jul.
	withHol := domain.JobDefinition{ID: "fbdh",
		Schedule:  domain.Schedule{Enabled: true, Frequency: "advanced", AdvancedRule: "first-businessday"},
		Calendars: []domain.CalendarRef{{Name: "c", Mode: "include"}}}
	cal := calMap{"c": {Name: "c", BusinessDays: []string{"mon", "tue", "wed", "thu", "fri"}, Holidays: []string{"2026-07-01"}}}
	if IsScheduledOn(withHol, day("2026-07-01"), cal) {
		t.Error("com feriado em 01, ele não é mais o 1º dia útil")
	}
	if !IsScheduledOn(withHol, day("2026-07-02"), cal) {
		t.Error("com feriado em 01, o 1º dia útil deveria ser 02/jul")
	}
}

// (b) Só segundas — weekly com DaysOfWeek=[mon].
func TestCTM4_OnlyMondays(t *testing.T) {
	def := domain.JobDefinition{ID: "seg",
		Schedule: domain.Schedule{Enabled: true, Frequency: "weekly", DaysOfWeek: []string{"mon"}}}
	empty := calMap{}
	cases := map[string]bool{
		"2026-07-06": true,  // seg
		"2026-07-13": true,  // seg
		"2026-07-07": false, // ter
		"2026-07-08": false, // qua
		"2026-07-05": false, // dom
	}
	for d, want := range cases {
		if got := IsScheduledOn(def, day(d), empty); got != want {
			t.Errorf("só-segundas %s: got %v, want %v", d, got, want)
		}
	}
}

// (c) 1º dia útil que NÃO é segunda — advanced:first-businessday-not-monday.
// julho começa numa quarta → 01(qua) já não é segunda, então roda em 01.
// junho começa numa SEGUNDA → pula 01(seg) e roda em 02(ter). Com feriado em
// 02(ter), anda para 03(qua).
func TestCTM4_FirstBusinessDayNotMonday(t *testing.T) {
	def := domain.JobDefinition{ID: "fbdnm",
		Schedule: domain.Schedule{Enabled: true, Frequency: "advanced", AdvancedRule: "first-businessday-not-monday"}}
	empty := calMap{}
	// julho: 1º útil é 01(qua) — não é segunda → roda em 01.
	if !IsScheduledOn(def, day("2026-07-01"), empty) {
		t.Error("julho: 01(qua) é o 1º dia útil não-segunda")
	}
	if IsScheduledOn(def, day("2026-07-02"), empty) {
		t.Error("julho: 02 não é o alvo")
	}
	// junho: 01 é segunda → pula; alvo = 02(ter).
	if IsScheduledOn(def, day("2026-06-01"), empty) {
		t.Error("junho: 01 é segunda — não deveria rodar")
	}
	if !IsScheduledOn(def, day("2026-06-02"), empty) {
		t.Error("junho: 02(ter) é o 1º dia útil não-segunda")
	}

	// junho com feriado em 02(ter): alvo anda para 03(qua).
	defc := domain.JobDefinition{ID: "fbdnmc",
		Schedule:  domain.Schedule{Enabled: true, Frequency: "advanced", AdvancedRule: "first-businessday-not-monday"},
		Calendars: []domain.CalendarRef{{Name: "j", Mode: "include"}}}
	cal := calMap{"j": {Name: "j", BusinessDays: []string{"mon", "tue", "wed", "thu", "fri"}, Holidays: []string{"2026-06-02"}}}
	if IsScheduledOn(defc, day("2026-06-02"), cal) {
		t.Error("junho c/ feriado em 02: 02 não roda")
	}
	if !IsScheduledOn(defc, day("2026-06-03"), cal) {
		t.Error("junho c/ feriado em 02: alvo deveria andar p/ 03(qua)")
	}
}

// (d) N-ésimo dia útil — businessday. 5º dia útil de julho = 07(ter); múltiplos
// [1,-1] pegam 01 e 31.
func TestCTM4_NthBusinessDay(t *testing.T) {
	empty := calMap{}
	fifth := domain.JobDefinition{ID: "n5",
		Schedule: domain.Schedule{Enabled: true, Frequency: "businessday", NthBusinessDays: []int{5}}}
	if !IsScheduledOn(fifth, day("2026-07-07"), empty) {
		t.Error("5º dia útil de julho é 07(ter)")
	}
	if IsScheduledOn(fifth, day("2026-07-06"), empty) {
		t.Error("06 é o 4º dia útil, não o 5º")
	}

	firstLast := domain.JobDefinition{ID: "fl",
		Schedule: domain.Schedule{Enabled: true, Frequency: "businessday", NthBusinessDays: []int{1, -1}}}
	if !IsScheduledOn(firstLast, day("2026-07-01"), empty) {
		t.Error("1º dia útil = 01")
	}
	if !IsScheduledOn(firstLast, day("2026-07-31"), empty) {
		t.Error("último dia útil = 31")
	}
	if IsScheduledOn(firstLast, day("2026-07-15"), empty) {
		t.Error("15 não é 1º nem último dia útil")
	}
}

// (e/f) include + exclude + feriados, no nível do IsScheduledOn (não só do
// IsEligibleDate): job diário com calendar include (com feriado) NÃO roda no
// feriado nem no fim de semana; job diário com calendar exclude "segundas" roda
// todo dia MENOS segunda.
func TestCTM4_IncludeExcludeHolidays(t *testing.T) {
	inc := domain.JobDefinition{ID: "inc",
		Schedule:  domain.Schedule{Enabled: true, Frequency: "daily"},
		Calendars: []domain.CalendarRef{{Name: "corp", Mode: "include"}}}
	cal := calMap{"corp": {Name: "corp", BusinessDays: []string{"mon", "tue", "wed", "thu", "fri"}, Holidays: []string{"2026-07-09"}}}
	incCases := map[string]bool{
		"2026-07-08": true,  // qua útil
		"2026-07-09": false, // feriado
		"2026-07-11": false, // sábado (fora dos business days)
	}
	for d, want := range incCases {
		if got := IsScheduledOn(inc, day(d), cal); got != want {
			t.Errorf("include %s: got %v, want %v", d, got, want)
		}
	}

	// exclude "mondays": o calendar marca segundas como elegíveis; em modo
	// exclude, o job roda todo dia EXCETO segunda.
	exc := domain.JobDefinition{ID: "exc",
		Schedule:  domain.Schedule{Enabled: true, Frequency: "daily"},
		Calendars: []domain.CalendarRef{{Name: "mon", Mode: "exclude"}}}
	calM := calMap{"mon": {Name: "mon", BusinessDays: []string{"mon"}}}
	excCases := map[string]bool{
		"2026-07-06": false, // seg — excluída
		"2026-07-07": true,  // ter
		"2026-07-11": true,  // sáb (não é segunda → não excluída)
	}
	for d, want := range excCases {
		if got := IsScheduledOn(exc, day(d), calM); got != want {
			t.Errorf("exclude %s: got %v, want %v", d, got, want)
		}
	}
}

// (g) Meses específicos — MonthsOfYear filtra qualquer frequência.
func TestCTM4_MonthsOfYear(t *testing.T) {
	empty := calMap{}
	julOnly := domain.JobDefinition{ID: "jul",
		Schedule: domain.Schedule{Enabled: true, Frequency: "daily", MonthsOfYear: []int{7}}}
	if !IsScheduledOn(julOnly, day("2026-07-08"), empty) {
		t.Error("julho está em monthsOfYear")
	}
	if IsScheduledOn(julOnly, day("2026-08-08"), empty) {
		t.Error("agosto está FORA de monthsOfYear")
	}

	// combinado com weekly[mon]: só segundas E só em julho.
	julMon := domain.JobDefinition{ID: "julmon",
		Schedule: domain.Schedule{Enabled: true, Frequency: "weekly", DaysOfWeek: []string{"mon"}, MonthsOfYear: []int{7}}}
	if !IsScheduledOn(julMon, day("2026-07-06"), empty) {
		t.Error("06/jul(seg) deveria rodar")
	}
	if IsScheduledOn(julMon, day("2026-07-07"), empty) {
		t.Error("07/jul(ter) não é segunda")
	}
	if IsScheduledOn(julMon, day("2026-08-03"), empty) { // 03/ago é segunda mas fora do mês
		t.Error("03/ago(seg) está fora de monthsOfYear")
	}
}

// GATING DIVERGENCE GUARD — os dois caminhos de "dia útil especial" (o enum
// `advanced` e o `businessday`+NthBusinessDays) precisam concordar dia a dia por
// TODO o mês. Se um dia divergir, o roadmap pediu para "corrigir o gating".
func TestCTM4_AdvancedVsNthBusinessDayAgree(t *testing.T) {
	empty := calMap{}
	pairs := []struct {
		rule string
		nth  int
	}{
		{"first-businessday", 1},
		{"last-businessday", -1},
		{"penultimate-businessday", -2},
	}
	for _, p := range pairs {
		adv := domain.JobDefinition{ID: "adv",
			Schedule: domain.Schedule{Enabled: true, Frequency: "advanced", AdvancedRule: p.rule}}
		nth := domain.JobDefinition{ID: "nth",
			Schedule: domain.Schedule{Enabled: true, Frequency: "businessday", NthBusinessDays: []int{p.nth}}}
		for d := 1; d <= 31; d++ {
			date := time.Date(2026, 7, d, 0, 0, 0, 0, time.UTC)
			a := IsScheduledOn(adv, date, empty)
			n := IsScheduledOn(nth, date, empty)
			if a != n {
				t.Errorf("%s vs nth=%d divergem em %s: advanced=%v nth=%v",
					p.rule, p.nth, date.Format("2006-01-02"), a, n)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CTM-5 — Controle de recursos
// ─────────────────────────────────────────────────────────────────────────────

// Lock exclusivo: capacidade 1 → só um detentor; o 2º espera; libera → entra.
func TestCTM5_ExclusiveLock(t *testing.T) {
	tr := NewResourceTracker()
	tr.SetCapacity("lock", 1)
	if !tr.TryAcquire("a", map[string]int{"lock": 1}) {
		t.Fatal("1º pedido deveria pegar o lock")
	}
	if tr.TryAcquire("b", map[string]int{"lock": 1}) {
		t.Fatal("lock exclusivo: 2º pedido NÃO deveria pegar")
	}
	tr.Release("a")
	if !tr.TryAcquire("b", map[string]int{"lock": 1}) {
		t.Fatal("após liberar, o 2º deveria pegar o lock")
	}
}

// Recurso desconhecido nasce com capacidade 1 (gating "no máx 1 por vez") — vira
// um lock exclusivo implícito.
func TestCTM5_UnknownResourceIsExclusive(t *testing.T) {
	tr := NewResourceTracker()
	if !tr.TryAcquire("a", map[string]int{"novo": 1}) {
		t.Fatal("recurso desconhecido deveria nascer com cap 1 e caber")
	}
	if tr.TryAcquire("b", map[string]int{"novo": 1}) {
		t.Fatal("recurso desconhecido default cap=1 → 2º não cabe")
	}
	for _, rs := range tr.Snapshot() {
		if rs.Name == "novo" && rs.Capacity != 1 {
			t.Fatalf("cap default deveria ser 1, veio %d", rs.Capacity)
		}
	}
}

// Máximo simultâneo por pool: capacidade N = no máx N rodando ao mesmo tempo. O
// (N+1)-ésimo entra na FILA (fica bloqueado); ao liberar um, o próximo entra.
func TestCTM5_PoolMaxSimultaneous(t *testing.T) {
	tr := NewResourceTracker()
	tr.SetCapacity("pool", 3)
	for _, id := range []string{"j1", "j2", "j3"} {
		if !tr.TryAcquire(id, map[string]int{"pool": 1}) {
			t.Fatalf("%s (≤3) deveria caber no pool", id)
		}
	}
	// 3/3 → o 4º NÃO cabe e Shortfalls (a MESMA função que o gate WAIT_RESOURCE
	// usa) aponta o pool: é o job "na fila".
	if tr.TryAcquire("j4", map[string]int{"pool": 1}) {
		t.Fatal("pool cheio (3/3): j4 não deveria caber")
	}
	sf := tr.Shortfalls(map[string]int{"pool": 1})
	if len(sf) != 1 || sf[0].Name != "pool" || sf[0].Used != 3 || sf[0].Capacity != 3 {
		t.Fatalf("Shortfalls deveria reportar pool 3/3 na fila, veio %+v", sf)
	}
	// libera um → o job na fila agora cabe e Shortfalls zera (saiu da fila).
	tr.Release("j2")
	if len(tr.Shortfalls(map[string]int{"pool": 1})) != 0 {
		t.Fatal("após liberar, o pool deveria ter vaga (fila esvazia)")
	}
	if !tr.TryAcquire("j4", map[string]int{"pool": 1}) {
		t.Fatal("após liberar j2, j4 deveria entrar")
	}
}

// Quantitative multi-unidade + all-or-nothing quando só parte cabe.
func TestCTM5_QuantitativeMultiUnit(t *testing.T) {
	tr := NewResourceTracker()
	tr.SetCapacity("cpu", 8)
	if !tr.TryAcquire("big", map[string]int{"cpu": 5}) {
		t.Fatal("5/8 deveria caber")
	}
	// quer 4, só há 3 livres → não cabe, e NÃO reserva nada.
	if tr.TryAcquire("x", map[string]int{"cpu": 4}) {
		t.Fatal("4 num pool com 3 livres não deveria caber")
	}
	if got := usedOf(tr, "cpu"); got != 5 {
		t.Fatalf("pedido falho não pode ter mexido no uso: cpu=%d (esperava 5)", got)
	}
	// 3 cabe exatamente.
	if !tr.TryAcquire("y", map[string]int{"cpu": 3}) {
		t.Fatal("3 deveria caber (8/8)")
	}
	if got := usedOf(tr, "cpu"); got != 8 {
		t.Fatalf("cpu deveria estar 8/8, veio %d", got)
	}
}

// Liberação correta com múltiplos detentores e múltiplos recursos + idempotência.
func TestCTM5_ReleaseMultiHolder(t *testing.T) {
	tr := NewResourceTracker()
	tr.SetCapacity("a", 10)
	tr.SetCapacity("b", 10)
	tr.TryAcquire("i1", map[string]int{"a": 1, "b": 2})
	tr.TryAcquire("i2", map[string]int{"a": 3})
	if usedOf(tr, "a") != 4 || usedOf(tr, "b") != 2 {
		t.Fatalf("estado inicial errado: a=%d b=%d", usedOf(tr, "a"), usedOf(tr, "b"))
	}
	tr.Release("i1")
	if usedOf(tr, "a") != 3 || usedOf(tr, "b") != 0 {
		t.Fatalf("após liberar i1: a=%d b=%d (esperava 3/0)", usedOf(tr, "a"), usedOf(tr, "b"))
	}
	// release idempotente: liberar de novo não deixa negativo.
	tr.Release("i1")
	if usedOf(tr, "a") != 3 {
		t.Fatalf("release idempotente violado: a=%d", usedOf(tr, "a"))
	}
	// liberar instance inexistente é no-op.
	tr.Release("fantasma")
	tr.Release("i2")
	if usedOf(tr, "a") != 0 {
		t.Fatalf("após liberar tudo: a=%d (esperava 0)", usedOf(tr, "a"))
	}
}

// Reduzir a capacidade abaixo do uso corrente (over-subscription): o uso não
// muda, novos pedidos ficam barrados até liberar.
func TestCTM5_CapacityReducedBelowUsed(t *testing.T) {
	tr := NewResourceTracker()
	tr.SetCapacity("slots", 2)
	tr.TryAcquire("a", map[string]int{"slots": 2}) // 2/2
	tr.SetCapacity("slots", 1)                     // over-subscription
	if usedOf(tr, "slots") != 2 {
		t.Fatalf("reduzir capacidade não pode alterar o uso: %d", usedOf(tr, "slots"))
	}
	if tr.TryAcquire("b", map[string]int{"slots": 1}) {
		t.Fatal("com uso 2 > cap 1, nada novo deveria caber")
	}
	tr.Release("a")
	if !tr.TryAcquire("b", map[string]int{"slots": 1}) {
		t.Fatal("após liberar, 1/1 deveria caber")
	}
}

func usedOf(tr *ResourceTracker, name string) int {
	for _, rs := range tr.Snapshot() {
		if rs.Name == name {
			return rs.Used
		}
	}
	return 0
}

// ─────────────────────────────────────────────────────────────────────────────
// CTM-6 — Forecast ≥1 semana à frente, contra o gating real
// ─────────────────────────────────────────────────────────────────────────────

// Correção da DIVERGÊNCIA: antes o Forecast() só olhava o calendar legado e
// ignorava a frequência — um job "só segundas" aparecia elegível todo dia,
// contradizendo o que a daily materializava. Agora usa IsScheduledOn (fonte
// única do RunDaily): elegível no forecast == ordenado na daily.
func TestCTM6_ForecastRespectsFrequency(t *testing.T) {
	defs := []domain.JobDefinition{{ID: "mononly",
		Schedule: domain.Schedule{Enabled: true, Frequency: "weekly", DaysOfWeek: []string{"mon"}}}}
	// domingo 12/jul → NÃO elegível (não é segunda).
	if elig := forecastEligible(Forecast(defs, nil, "2026-07-12")); elig["mononly"] {
		t.Error("job só-segundas não pode ser elegível num domingo no Forecast")
	}
	// segunda 06/jul → elegível.
	if elig := forecastEligible(Forecast(defs, nil, "2026-07-06")); !elig["mononly"] {
		t.Error("job só-segundas deveria ser elegível numa segunda no Forecast")
	}
}

// A semana inteira: o conjunto de jobs elegíveis do Forecast, dia a dia, tem que
// bater com um oráculo escrito à mão (independente do IsScheduledOn) para uma
// carga mista. É a validação "≥1 semana à frente contra o gating real".
func TestCTM6_ForecastWeekMatchesOracle(t *testing.T) {
	defs := []domain.JobDefinition{
		{ID: "diario", Schedule: domain.Schedule{Enabled: true, Frequency: "daily"}},
		{ID: "monfri", Schedule: domain.Schedule{Enabled: true, Frequency: "weekly", DaysOfWeek: []string{"mon", "tue", "wed", "thu", "fri"}}},
		{ID: "mononly", Schedule: domain.Schedule{Enabled: true, Frequency: "weekly", DaysOfWeek: []string{"mon"}}},
		{ID: "firstbd", Schedule: domain.Schedule{Enabled: true, Frequency: "advanced", AdvancedRule: "first-businessday"}},
		{ID: "off", Schedule: domain.Schedule{Enabled: false, Frequency: "daily"}},
	}
	// Semana 06/jul(seg) .. 12/jul(dom). firstbd (1º útil = 01/jul) NÃO cai nesta
	// semana; off é disabled; mononly só em 06.
	oracle := map[string][]string{
		"2026-07-06": {"diario", "monfri", "mononly"},
		"2026-07-07": {"diario", "monfri"},
		"2026-07-08": {"diario", "monfri"},
		"2026-07-09": {"diario", "monfri"},
		"2026-07-10": {"diario", "monfri"},
		"2026-07-11": {"diario"},
		"2026-07-12": {"diario"},
	}
	reports := ForecastRange(defs, nil, "2026-07-06", 7)
	if len(reports) != 7 {
		t.Fatalf("ForecastRange deveria devolver 7 dias, veio %d", len(reports))
	}
	for _, rep := range reports {
		want := setOf(oracle[rep.OrderDate])
		got := forecastEligibleSet(rep)
		if !sameSet(want, got) {
			t.Errorf("%s: elegíveis=%v, esperava %v", rep.OrderDate, ctmKeys(got), ctmKeys(want))
		}
	}
}

// Range respeita calendars (feriado/exclude) sobre a semana: no feriado o job
// some da lista de elegíveis daquele dia.
func TestCTM6_ForecastRangeRespectsHolidays(t *testing.T) {
	defs := []domain.JobDefinition{{ID: "corp",
		Schedule:  domain.Schedule{Enabled: true, Frequency: "daily"},
		Calendar:  "corp"}} // usa o campo legado calendar (include)
	cals := map[string]*domain.Calendar{"corp": {Name: "corp",
		BusinessDays: []string{"mon", "tue", "wed", "thu", "fri"}, Holidays: []string{"2026-07-09"}}}
	reports := ForecastRange(defs, cals, "2026-07-06", 7)
	for _, rep := range reports {
		elig := forecastEligible(rep)
		switch rep.OrderDate {
		case "2026-07-09": // feriado
			if elig["corp"] {
				t.Error("no feriado 09/jul o job não deveria ser elegível")
			}
		case "2026-07-11", "2026-07-12": // fim de semana
			if elig["corp"] {
				t.Errorf("%s é fim de semana — não elegível", rep.OrderDate)
			}
		default: // dias úteis
			if !elig["corp"] {
				t.Errorf("%s é dia útil — deveria ser elegível", rep.OrderDate)
			}
		}
	}
}

// Range respeita deps: as ondas topológicas (A→B→C) valem em cada dia elegível
// da semana.
func TestCTM6_ForecastRangeWaves(t *testing.T) {
	defs := []domain.JobDefinition{
		{ID: "A", Schedule: domain.Schedule{Enabled: true}},
		{ID: "B", Schedule: domain.Schedule{Enabled: true}, Upstream: []domain.Upstream{{From: "A", Condition: domain.CondOnSuccess}}},
		{ID: "C", Schedule: domain.Schedule{Enabled: true}, Upstream: []domain.Upstream{{From: "B", Condition: domain.CondOnSuccess}}},
	}
	for _, rep := range ForecastRange(defs, nil, "2026-07-06", 5) {
		w := map[string]int{}
		for _, j := range rep.Jobs {
			w[j.DefID] = j.Wave
		}
		if w["A"] != 0 || w["B"] != 1 || w["C"] != 2 {
			t.Fatalf("%s: ondas A=%d B=%d C=%d (esperava 0/1/2)", rep.OrderDate, w["A"], w["B"], w["C"])
		}
	}
}

// Range respeita recursos: o pico por recurso é a soma na onda mais carregada.
// Dois jobs na MESMA onda (sem dep entre si) somam; um dependente fica em outra
// onda e não soma no pico.
func TestCTM6_ForecastRangeResourcePeak(t *testing.T) {
	defs := []domain.JobDefinition{
		{ID: "a", Schedule: domain.Schedule{Enabled: true}, Resources: map[string]int{"db": 2}},
		{ID: "b", Schedule: domain.Schedule{Enabled: true}, Resources: map[string]int{"db": 3}},
		{ID: "c", Schedule: domain.Schedule{Enabled: true}, Resources: map[string]int{"db": 10},
			Upstream: []domain.Upstream{{From: "a", Condition: domain.CondOnSuccess}}},
	}
	rep := Forecast(defs, nil, "2026-07-06")
	// onda 0: a(2)+b(3)=5; onda 1: c(10). pico = max(5,10)=10.
	if rep.Resources["db"] != 10 {
		t.Fatalf("pico de 'db' deveria ser 10 (onda de c), veio %d", rep.Resources["db"])
	}
}

// ForecastRange: clamps e data inválida.
func TestCTM6_ForecastRangeClamps(t *testing.T) {
	defs := []domain.JobDefinition{{ID: "x", Schedule: domain.Schedule{Enabled: true}}}
	if got := ForecastRange(defs, nil, "2026-07-06", 0); len(got) != 1 {
		t.Errorf("days=0 deveria virar 1, veio %d", len(got))
	}
	if got := ForecastRange(defs, nil, "2026-07-06", 400); len(got) != 366 {
		t.Errorf("days=400 deveria clampar em 366, veio %d", len(got))
	}
	if got := ForecastRange(defs, nil, "data-ruim", 7); got != nil {
		t.Errorf("data inválida deveria devolver nil, veio %v", got)
	}
}

// Conditions são gate de RUNTIME, não de ORDENAÇÃO: um job com ConditionsIn não
// satisfeitas AINDA aparece elegível no Forecast (a daily o materializa; quem
// segura é o gateInstance no tick — coberto por TestExplain_WaitCondition). Este
// teste trava essa fronteira para o forecast não passar a "esconder" o job.
func TestCTM6_ForecastIgnoresRuntimeConditions(t *testing.T) {
	defs := []domain.JobDefinition{{ID: "cond",
		Schedule:     domain.Schedule{Enabled: true, Frequency: "daily"},
		ConditionsIn: []string{"upstream-done"}}}
	if elig := forecastEligible(Forecast(defs, nil, "2026-07-06")); !elig["cond"] {
		t.Error("Forecast é order-time: job com condition pendente ainda é ordenado/elegível")
	}
}

// ── helpers dos testes de forecast ──

func forecastEligible(rep ForecastReport) map[string]bool {
	out := map[string]bool{}
	for _, j := range rep.Jobs {
		out[j.DefID] = j.Eligible
	}
	return out
}

func forecastEligibleSet(rep ForecastReport) map[string]bool {
	out := map[string]bool{}
	for _, j := range rep.Jobs {
		if j.Eligible {
			out[j.DefID] = true
		}
	}
	return out
}

func setOf(ids []string) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func ctmKeys(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
