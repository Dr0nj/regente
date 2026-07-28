# Post LinkedIn — Fase Z (rascunhos prontos pra colar, PT + EN)

> **Como usar:** a versão **principal** é pra publicar do **seu perfil pessoal** (post
> pessoal tem 10–50x mais alcance orgânico que company page nova); a página Regente
> Orchestrator **reposta**. Versões **curtas** servem pra página ou follow-up. Escolha
> o idioma pelo público: PT pra sua rede, EN pra alcance internacional (ou publique os
> dois com 1–2 dias de intervalo). Prints/vídeo e primeiro comentário: ver notas no fim.
>
> ⚠️ **Regra de honestidade:** o `AI_AGENT` é **roadmap (spec pronta), não entregue** —
> em qualquer adaptação, mantenha ele sempre como "próximo passo", nunca como feature.

---

## Versão principal — Português

Nos últimos meses eu construí, como projeto pessoal, um orquestrador de jobs classe
enterprise — do zero. Chama **Regente**.

Quem opera batch enterprise conhece o padrão: daily que materializa o dia, dependências
com condições, calendários de dia útil, hold/release/rerun/force, confirm de operador,
forecast. Ferramentas dessa categoria são excelentes — e caras, proprietárias, e guardam
o runbook da empresa fora do versionamento.

A pergunta que me moveu: quanto disso dá pra reconstruir com engenharia moderna, sem
perder a semântica operacional que faz essas ferramentas serem boas?

O resultado:

🔹 **Git-nativo** — as definições de job são YAML num repo GitHub; a daily sincroniza o
main antes de materializar e cada ordem grava o SHA que a originou. Editar na UI cria uma
sessão de design; nada roda sem publish (commit/PR).

🔹 **Semântica de orquestrador enterprise clássico, de verdade** — daily imutável (snapshot congelado na ordem),
carry-over honesto na virada (a ordem avança de dia preservando a data de origem),
dependências como condições explícitas com lógica AND/OR real e consumo (rerun do pai
volta a segurar quem depende dele), calendários com N-ésimo dia útil e shift, cyclic,
Confirm de operador, retry agendado durável.

🔹 **Escala validada ao vivo** — 1.000.000 de jobs/dia: materialização em ~17s, summary
em 51ms, UI virtualizada com 37 elementos no DOM para o dia inteiro.

🔹 **Do VPS de US$5 ao HA** — um binário Go serve API + WebSocket + UI sobre SQLite;
as mesmas flags viram HA multi-nó com Postgres e leader election, ou deploy serverless
com scheduler dirigido por cron externo.

🔹 **Agent-native** — além de UI, CLI e OpenAPI, o plano de controle é exposto via MCP
(22 tools): um agente de IA pode diagnosticar por que um job não rodou ("Why not?") e,
com permissão, agir.

🔹 **Próximo no roadmap: IA que não tira dado do perímetro** — um jobType `AI_AGENT`
(spec pronta): a LLM roda **no mesmo host do agente** (Ollama/llama.cpp/vLLM), analisa
sysout e logs ali mesmo e o server recebe só o veredito — nada sai da sua máquina, e é
literal. O prompt congela no snapshot imutável da ordem (auditável pra sempre) e a
primeira fase é análise-only, sem tools: um prompt injetado no máximo gera um relatório
errado, nunca um comando executado. Caso de fábrica: RCA automático quando um job falha.

São ~47 mil linhas de Go, ~24 mil de TypeScript e 456 testes — incluindo baterias de
calendário validadas contra um oráculo escrito à mão.

A lição que levo: a parte difícil de um orquestrador não é o scheduler — é a semântica
nas bordas (o que acontece com a dependência quando o operador dá rerun no pai? o que
sobrevive à virada do dia?). É aí que ferramentas de 30 anos ganham respeito, e foi aí
que este projeto mais me ensinou.

Case study técnico completo no primeiro comentário.

#engenharia #golang #react #orquestracao #batch #devops #sre

---

## Main version — English

Over the past months I built, as a personal project, an enterprise-class job
orchestrator — from scratch. It's called **Regente**.

Anyone who runs enterprise batch knows the pattern: a daily that materializes the day,
condition-based dependencies, business-day calendars, hold/release/rerun/force, operator
confirm, forecast. Tools in this category are excellent — and expensive, proprietary,
and they keep your company's runbook outside version control.

The question that drove me: how much of that can be rebuilt with modern engineering,
without losing the operational semantics that make these tools good?

The result:

🔹 **Git-native** — job definitions are YAML in a GitHub repo; the daily syncs main
before materializing, and every order records the SHA it came from. Editing in the UI
opens a design session; nothing runs without a publish (commit/PR).

🔹 **Real classic-enterprise semantics** — immutable daily (snapshot frozen into the order),
honest carry-over at day rollover (the order moves forward while keeping its original
date), dependencies as explicit conditions with real AND/OR logic and consumption
(rerunning a parent holds its dependents again), calendars with Nth-business-day and
shift, cyclic jobs, operator Confirm, durable scheduled retries.

🔹 **Scale validated live** — 1,000,000 jobs/day: materialization in ~17s, summary in
51ms, a virtualized UI holding the whole day with 37 DOM elements.

🔹 **From a $5 VPS to HA** — one Go binary serves API + WebSocket + UI over SQLite;
the same flags turn into multi-node HA with Postgres and leader election, or a
serverless deploy driven by an external cron scheduler.

🔹 **Agent-native** — beyond UI, CLI and OpenAPI, the control plane is exposed via MCP
(22 tools): an AI agent can diagnose why a job didn't run ("Why not?") and, with
permission, act.

🔹 **Next on the roadmap: AI that never leaves your perimeter** — an `AI_AGENT` job
type (spec ready): the LLM runs **on the same host as the agent** (Ollama/llama.cpp/
vLLM), analyzes sysout and logs right there, and the server only receives the verdict —
your data never leaves your machine, literally. The prompt is frozen into the order's
immutable snapshot (auditable forever), and phase one is analysis-only, no tools: a
prompt injection can at worst produce a wrong report, never an executed command.
Built-in use case: automatic RCA when a job fails.

That's ~47k lines of Go, ~24k of TypeScript and 456 tests — including calendar suites
validated against a hand-written oracle.

The lesson I take away: the hard part of an orchestrator isn't the scheduler — it's the
semantics at the edges (what happens to a dependency when an operator reruns the parent?
what survives the day rollover?). That's where 30-year-old tools earn their respect, and
that's where this project taught me the most.

Full technical case study in the first comment.

#engineering #golang #react #orchestration #batch #devops #sre

---

## Versão curta — Português

Construí do zero um orquestrador de jobs classe enterprise: **Regente**.

Git como fonte de verdade (jobs em YAML, publish = commit/PR), daily imutável,
dependências como condições com AND/OR real, calendários de dia útil, Confirm,
forecast — e 1.000.000 de jobs/dia validado ao vivo (daily materializada em ~17s,
UI virtualizada).

Um binário Go roda tudo num VPS de US$5; as mesmas flags viram HA com Postgres ou
deploy serverless. O plano de controle é agent-native: 22 tools MCP para um agente de
IA operar o dia com permissão. E o próximo passo do roadmap é o jobType `AI_AGENT`:
LLM local no host do agente analisando falhas sem nenhum dado sair do perímetro.

~47k linhas de Go, ~24k de TS, 456 testes. A parte difícil não foi o scheduler — foi a
semântica nas bordas, exatamente onde as ferramentas de 30 anos ganham o respeito delas.

Case study técnico no primeiro comentário.

#golang #devops #orquestracao

---

## Short version — English

I built an enterprise-class job orchestrator from scratch: **Regente**.

Git as the source of truth (jobs in YAML, publish = commit/PR), immutable daily,
dependencies as conditions with real AND/OR logic, business-day calendars, operator
Confirm, forecast — and 1,000,000 jobs/day validated live (daily materialized in ~17s,
virtualized UI).

One Go binary runs everything on a $5 VPS; the same flags turn into HA with Postgres or
a serverless deploy. The control plane is agent-native: 22 MCP tools so an AI agent can
operate the day with permission. Next on the roadmap: an `AI_AGENT` job type — a local
LLM on the agent's host analyzing failures with zero data leaving your perimeter.

~47k lines of Go, ~24k of TS, 456 tests. The hard part wasn't the scheduler — it was
the semantics at the edges, exactly where the 30-year-old tools earn their respect.

Full technical case study in the first comment.

#golang #devops #orchestration

---

## Notas para a publicação

- **Perfil pessoal × página:** a versão principal é do PERFIL PESSOAL (a página com
  poucos seguidores quase não tem distribuição orgânica — os posts dela são vitrine,
  não alcance). A página reposta o post pessoal e pode seguir com a série de
  mini-posts (1 feature + 1 vídeo curto: What-If · Explain "Why not?" · MCP · 1M ao
  vivo · AI_AGENT quando entregue).
- **Prints sugeridos:** Monitoring com grafo de dependências (linhas verdes/vermelhas),
  ViewPoint com 1M de jobs, tela de temas, e o Explain "Why not?".
- **Case study = ARTIGO no LinkedIn (decidido 2026-07-21):** publicar PRIMEIRO os
  artigos — PT = `docs/case-study.md` · EN = `docs/case-study.en.md`, ambos já em
  formato de artigo (sem tabelas; títulos `##` viram Heading, negrito e listas colam
  direto no editor) —, depois os posts: post PT linka o artigo PT no **1º comentário**,
  post EN linka o artigo EN (o LinkedIn penaliza link no corpo do post). Capa sugerida
  do artigo: print do Monitoring com grafo.
- **AI_AGENT:** sempre como "próximo no roadmap / spec pronta" — nunca anunciar como
  entregue antes da AI-1 fechar (ver §🔮 AI_AGENT no roadmap).
