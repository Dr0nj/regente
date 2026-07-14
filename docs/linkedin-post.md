# Post LinkedIn — Fase Z (rascunho pronto pra colar)

> **Como usar:** copie a versão que preferir, ajuste o link do repositório (se for
> torná-lo público) ou anexe prints da UI (Monitoring com o grafo + ViewPoint @1M são
> os mais fortes). Publicar é ação sua — o texto abaixo é o entregável da Fase Z.

---

## Versão principal

Nos últimos meses eu construí, como projeto pessoal, um orquestrador de jobs classe
Control-M — do zero. Chama **Regente**.

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

🔹 **Semântica Control-M de verdade** — daily imutável (snapshot congelado na ordem),
carry-over com orçamento na virada, dependências como eventos consumíveis (rerun do pai
não apaga o que já foi satisfeito; uma cópia forçada espera um término novo), calendários
com N-ésimo dia útil e shift, cyclic, Confirm, retry agendado durável.

🔹 **Escala validada ao vivo** — 1.000.000 de jobs/dia: materialização em ~17s, summary
em 51ms, UI virtualizada com 37 elementos no DOM para o dia inteiro.

🔹 **Do VPS de US$5 ao HA** — um binário Go serve API + WebSocket + UI sobre SQLite;
as mesmas flags viram HA multi-nó com Postgres e leader election, ou deploy serverless
com scheduler dirigido por cron externo.

🔹 **Agent-native** — além de UI, CLI e OpenAPI, o plano de controle é exposto via MCP
(22 tools): um agente de IA pode diagnosticar por que um job não rodou ("Why not?") e,
com permissão, agir.

São ~40 mil linhas de Go, ~24 mil de TypeScript e 372 testes — incluindo baterias de
calendário validadas contra um oráculo escrito à mão.

A lição que levo: a parte difícil de um orquestrador não é o scheduler — é a semântica
nas bordas (o que acontece com a dependência quando o operador dá rerun no pai? o que
sobrevive à virada do dia?). É aí que ferramentas de 30 anos ganham respeito, e foi aí
que este projeto mais me ensinou.

Case study técnico completo nos comentários / no repositório.

#engenharia #golang #react #orquestracao #controlm #devops #sre

---

## Versão curta (alternativa)

Construí do zero um orquestrador de jobs classe Control-M: **Regente**.

Git como fonte de verdade (jobs em YAML, publish = commit/PR), daily imutável,
dependências como eventos consumíveis, calendários de dia útil, Confirm, forecast —
e 1.000.000 de jobs/dia validado ao vivo (daily materializada em ~17s, UI virtualizada).

Um binário Go roda tudo num VPS de US$5; as mesmas flags viram HA com Postgres ou
deploy serverless. E o plano de controle é agent-native: 22 tools MCP para um agente
de IA operar o dia com permissão.

~40k linhas de Go, ~24k de TS, 372 testes. A parte difícil não foi o scheduler — foi a
semântica nas bordas, exatamente onde as ferramentas de 30 anos ganham o respeito delas.

Case study técnico no link.

#golang #devops #orquestracao

---

## Notas para a publicação

- **Prints sugeridos:** Monitoring com grafo de dependências (linhas verdes/vermelhas),
  ViewPoint com 1M de jobs, tela de temas, e o Explain "Why not?".
- **Primeiro comentário:** link para o case study (`docs/case-study.md`) — o LinkedIn
  penaliza link no corpo do post.
- Se o repo continuar privado, o case study pode ser publicado como artigo no próprio
  LinkedIn ou num gist público.
