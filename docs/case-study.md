# Case study — Regente: um orquestrador de jobs classe Control-M, Git-nativo, do zero

> **TL;DR.** O Regente é um orquestrador de workloads batch no espírito do BMC Control-M —
> daily imutável, dependências com condições, calendários, recursos, Confirm, forecast —
> construído do zero como monorepo Go + React, com **Git como fonte de verdade** das
> definições. Validado ao vivo com **1.000.000 de jobs/dia** (materialização em 17s,
> summary em 51ms), roda de um único binário num VPS de US$5 até HA multi-nó com Postgres,
> e expõe o plano de controle a agentes de IA via **MCP (22 tools)**. ~47k linhas de Go,
> ~24k de TypeScript, 456 testes.

---

## 1. O problema

Orquestradores enterprise (Control-M, Autosys, Tivoli) resolvem um problema real — o dia
de operação batch: "materialize os jobs de hoje, respeite dependências/calendários/janelas,
me dê hold/release/rerun/force e um trilho de auditoria". Mas cobram caro por isso, em
três moedas:

- **Licença e lock-in** — o runbook da empresa fica preso num console proprietário.
- **Jobs fora do versionamento** — a definição vive no banco da ferramenta; diff, review
  e rollback viram processos manuais.
- **Fricção de DevEx** — testar um job novo exige ambiente compartilhado; "promover de
  dev pra prod" é um ritual.

A pergunta do projeto: *quanto dessa categoria dá pra reconstruir com engenharia moderna
— Git, binário único, API aberta — sem perder a semântica operacional que faz o Control-M
ser bom?*

## 2. As apostas de arquitetura

**Git-nativo de verdade.** As definições de job são YAML num repositório GitHub separado
(`regente-workspace`). A daily sincroniza `origin/main` **antes** de materializar; cada
ordem grava o SHA do commit que a originou. Editar no Design da UI cria uma *design
session* (clone efêmero server-side) e **nada roda sem publicar** — publish = commit+push
(ou PR, se a política exigir). Drift entre runtime e Git é detectado e alertado.

**Imutabilidade como regra de produto.** Ao ordenar, a definição é **congelada** na
instância (snapshot JSON + colunas denormalizadas). Publicar uma mudança no Design nunca
reescreve o dia corrente — o Monitoring é a foto do que foi agendado, como no Control-M.
Essa regra virou o teste decisivo de vários bugs ("o selo mudou sozinho" = leu a def viva).

**Monolito operável, serverless opcional.** Um binário Go serve API + WebSocket + UI
(single-origin) sobre SQLite — deploy de 1 caixa. As mesmas flags viram um deploy
"serverless portátil": scheduler dirigido por cron externo (`POST /scheduler/tick`,
idempotente, advisory-lock por tick), transporte de agente plugável (WS, long-poll, SSE,
NATS), executor WASM. Postgres + leader election (advisory lock) dão HA multi-nó sem
mudar o código de scheduling — só quem é líder materializa e despacha.

**Fonte única de decisão.** Todo gate de execução (janela, dependência, condition,
agente, recurso, Confirm) passa por **um** avaliador (`gateInstance`). O tick usa-o para
decidir; o "Why not?" da UI usa-o para explicar. Nenhum bloqueio existe sem aparecer no
Explain — por construção, não por disciplina.

## 3. Semântica Control-M (a parte difícil)

A paridade não é a lista de features — é o comportamento nas bordas:

- **Daily / New Day** — materialização set-based em lote (1 commit por chunk de 5k),
  carry-over honesto: a ordem que atravessa a virada **avança de dia preservando a data
  de origem** — tudo que é escopado por data (condições `@odat`, variáveis, janelas)
  continua usando a data em que a ordem nasceu. RUNNING/HELD sempre atravessam; NOTOK
  não tratado sobrevive os dias configurados desde a falha; WAITING só com retenção
  explícita.
- **Dependências como CONDIÇÕES explícitas num pool único** — a seta A→B do canvas é
  açúcar para uma condição nomeada (`A-TO-B`): o OK do pai **cria** a condição, a
  entrada do sucessor **aguarda** por ela, e uma saída negativa **consome** (deleta) —
  o que modela fan-in com consumo, como no Control-M. A entrada aceita **lógica E/OU
  real** (grupos com operador de topo) e o token `$TIME` ("condição OU horário");
  rerun do pai desfaz o OK e quem depende **volta a aguardar** um término novo.
- **Force com dois gestos** — "Run Now" destrava a instância existente (bypass de
  janela/deps/recursos; Confirm e agente nunca são bypassados); "Order Force" cria uma
  ordem nova fora do agendamento que **respeita** os gates de runtime.
- **Conditions IN/OUT, recursos quantitativos, calendários** (N-ésimo dia útil, shift
  para dia útil seguinte/anterior, include/exclude, feriados), **cyclic** com janela e
  teto, **Confirm** (wait de runtime, não de schedule), **retry agendado durável**
  (sobrevive a restart e à virada — uma goroutine dormindo 3 dias morreria no primeiro
  deploy), **%%variáveis** com escopo global/local e offsets de dia útil (`%%ODATE+3B`).
- **Agentes pinados estritos** — job criado num agente fica **naquele** agente; se ele
  cair, o job espera em WAIT AGENT. Nunca migra sozinho. Todo server nasce com um
  **SERVER-AGENT embutido** (HTTP/REST) — chamada de API roda sem instalar agente externo.

## 4. Além do Control-M

Onde dá pra ser melhor que o original sem inventar moda:

- **Explain ("por que não rodou?")** — resposta estruturada por instância, da mesma
  fonte que decide o dispatch.
- **Blast radius / What-If / Dry-run / Daily diff** — simulações do dia sem materializar.
- **Agent-native (MCP)** — 22 tools (11 read + 11 write gated) expõem o plano de
  controle a agentes de IA: um LLM pode diagnosticar um job travado e (com permissão)
  dar rerun.
- **DevEx de linha de comando** — `regente test` (valida+simula um workspace, CI-friendly),
  `regente dev` (ambiente descartável local), `regente promote` (promoção entre branches),
  DSL Go para gerar YAML, OpenAPI curada servida pelo próprio binário.
- **Quick actions assinadas** em alertas (Slack/e-mail/PagerDuty) — rerun/confirm com um
  clique, com token expirável.

## 5. Escala validada (não estimada)

Cenário de 1M jobs/dia seedado e operado ao vivo:

- **Materialização da daily (write-path):** 1.000.000 de instances em **~17s**.
- **Summary do dia (read-path):** **51ms @100k**, paginação keyset+offset.
- **UI com 1M de jobs:** dashboard instantâneo; folder abre em ~39ms; **37 rows no
  DOM** para o dia inteiro (virtualização).
- **Board do canvas:** cap configurável (500–5000) + ViewPoint server-driven.

As decisões que pagaram a conta: existência set-based (1 query, não 1 por def), inserts
em lote com prepared statements, claim atômico de dispatch (`UPDATE … WHERE status=WAITING`),
paginação keyset no read-path e sidebar virtualizada com altura comprimida (o browser
estoura float32 acima de ~16,7M px — detalhe que só aparece com 1M de linhas).

## 6. Confiabilidade

- **Claim atômico** por instância garante ≤1 execução mesmo com múltiplas vias de
  dispatch (tick, force, retry) ou múltiplos nós.
- **Panic-recovery** em todo ponto de entrada do scheduler; watchdog de RUNNING preso;
  self-monitoring (R7) que alerta pelos próprios canais do produto.
- **Backup online** (`-backup` = VACUUM INTO), DR documentado, retenção/archives com GC.
- **456 testes Go** — inclusive baterias exaustivas de calendário (dois caminhos de
  "dia útil especial" têm que concordar dia a dia por um mês inteiro) e um oráculo de
  forecast escrito à mão, independente do código que ele valida.

## 7. Segurança e enterprise

RBAC por folder (ACLs), OIDC/SSO opcional, mTLS server↔agente, trilha de auditoria com
export a SIEM, tokens por agente, secrets via provider (env/arquivo — o PAT do GitHub
nunca precisa persistir em claro no state store), sandbox de execução para agentes
multi-tenant (container sem capabilities, sem mounts, uid dedicado).

## 8. Deploy de 1 caixa (o teste ácido)

`curl … | sudo bash` num VPS Linux: baixa o bundle da release (binário + SPA + deploy),
registra systemd, serve UI+API+WS numa origem, `regente-configure` guia token forte +
PAT via secrets provider, nginx+certbot dão TLS com renovação. O mesmo produto que faz
HA com Postgres roda inteiro numa máquina de US$5 — essa amplitude era um objetivo, não
um acidente.

## 9. O próximo passo: IA que não sai do perímetro *(roadmap, spec pronta)*

O público Control-M vive em ambiente regulado — bancos, seguradoras, utilities — onde
sysout, logs e dados de host **não podem sair do perímetro**. Todo scheduler "com IA"
do mercado manda esses dados pra uma API de nuvem; o próximo jobType do Regente, o
`AI_AGENT`, faz o inverso: a LLM roda **no mesmo host do agente** (Ollama/llama.cpp/
vLLM), analisa sysout e logs ali mesmo, e o server recebe só o veredito — nenhum dado
viaja. O prompt é parte da definição, então **congela no snapshot imutável da ordem**:
o prompt exato que rodou fica auditável pra sempre, e Confirm, conditions, SLA, RBAC e
audit trail funcionam sem uma linha nova, porque `AI_AGENT` é um jobType como outro
qualquer. A primeira fase é análise-only, **sem tools** — um prompt injetado no máximo
produz um relatório errado, nunca um comando executado. Caso de fábrica: RCA automático
quando um job falha, com saída validada por JSON Schema virando variáveis para os
sucessores.

## 10. Lições que valem para qualquer sistema

1. **Imutabilidade é feature de produto, não detalhe de storage.** Congelar a definição
   na ordem eliminou uma classe inteira de bugs de "mudou sozinho".
2. **Uma fonte única de decisão compra explicabilidade de graça.** O Explain nunca
   diverge do dispatch porque É o dispatch.
3. **Estado durável > goroutines pacientes.** Retry de 3 dias vive no banco, não num
   sleep.
4. **Satisfação de dependência é estado explícito, não uma leitura.** Derivar a aresta
   do status vivo do pai quebra na primeira operação humana (rerun/cancel); condições
   nomeadas criadas e consumidas num pool — e congeladas na ordem — modelam o que o
   operador espera.
5. **Escala é aditiva se o caminho quente for set-based desde o começo.** O mesmo código
   que atende 10 jobs atende 1M — as otimizações foram de forma de acesso, não de
   arquitetura.
6. **Teste contra um oráculo, não contra a própria função.** As baterias de calendário
   pegaram divergências reais que testes espelho jamais veriam.

## 11. Números do projeto

- **Código:** ~47k linhas Go (server+agent) · ~24k linhas TS/TSX (UI).
- **Testes:** 456 funções de teste Go em 102 arquivos + validações E2E ao vivo.
- **Executores:** COMMAND · SCRIPT · HTTP/REST · SSH agentless · DATABASE · FILE_WATCH
  · MFT · WASM · K8s · Lambda · Batch · Glue · Step Functions · Cloud Run.
- **Escala:** 1M jobs/dia validado ao vivo (write 17s · summary 51ms · UI virtualizada).
- **Deploy:** binário único (SQLite) → HA multi-nó (Postgres + leader election) →
  serverless portátil (tick externo).
- **Interface:** UI React (Design/Monitoring), 17 temas, CLI, OpenAPI, MCP (22 tools).

---

*Escrito em 2026-07-13, ao fechar a Fase Z do roadmap; revisado em 2026-07-21 (condições
E/OU e carry-over por data de origem no lugar dos modelos aposentados · números
atualizados · seção AI_AGENT · formato pronto pra artigo, sem tabelas). Detalhes de cada
entrega, com datas e commits, em [`docs/roadmap.md`](roadmap.md).*
