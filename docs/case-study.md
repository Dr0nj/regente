# Case study — Regente: um orquestrador de jobs classe enterprise, Git-nativo, do zero

> **TL;DR.** **1.000.000 de jobs/dia rodando num VPS de US$5** — daily materializada em
> 17s, summary do dia em 51ms. O Regente é um orquestrador de workloads batch no espírito
> dos orquestradores enterprise clássicos — daily imutável, dependências com condições,
> calendários, recursos, Confirm, forecast — que eu construí **sozinho, do zero**, como
> projeto pessoal: um monorepo Go + React com **Git como fonte de verdade** das
> definições. Escala do binário único até HA multi-nó com Postgres, e expõe o plano de
> controle a agentes de IA via **MCP (22 tools)**. ~47k linhas de Go, ~24k de TypeScript,
> 460 testes. Aberto sob Apache 2.0 — código em **github.com/Dr0nj/regente**,
> documentação em **dr0nj.github.io/regente**.

---

## 1. O problema

Os orquestradores enterprise clássicos resolvem um problema real — o dia
de operação batch: "materialize os jobs de hoje, respeite dependências/calendários/janelas,
me dê hold/release/rerun/force e um trilho de auditoria". Mas cobram caro por isso, em
três moedas:

- **Licença e lock-in** — o runbook da empresa fica preso num console proprietário.
- **Jobs fora do versionamento** — a definição vive no banco da ferramenta; diff, review
  e rollback viram processos manuais.
- **Fricção de DevEx** — testar um job novo exige ambiente compartilhado; "promover de
  dev pra prod" é um ritual.

A pergunta do projeto: *quanto dessa categoria dá pra reconstruir com engenharia moderna
— Git, binário único, API aberta — sem perder a semântica operacional que faz essa
categoria ser boa?*

## 2. As apostas de arquitetura

**Git-nativo de verdade.** As definições de job são YAML num repositório GitHub separado
(`regente-workspace`). A daily sincroniza `origin/main` **antes** de materializar; cada
ordem grava o SHA do commit que a originou. Editar no Design da UI cria uma *design
session* (clone efêmero server-side) e **nada roda sem publicar** — publish = commit+push
(ou PR, se a política exigir). Drift entre runtime e Git é detectado e alertado.

**Imutabilidade como regra de produto.** Ao ordenar, a definição é **congelada** na
instância (snapshot JSON + colunas denormalizadas). Publicar uma mudança no Design nunca
reescreve o dia corrente — o Monitoring é a foto do que foi agendado, como manda o modelo
enterprise clássico.
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

## 3. Semântica enterprise clássica (a parte difícil)

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
  o que modela fan-in com consumo, como no modelo clássico. A entrada aceita **lógica E/OU
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

## 4. Além do modelo clássico

Onde dá pra ser melhor que os incumbentes sem inventar moda:

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
- **460 testes Go** — inclusive baterias exaustivas de calendário (dois caminhos de
  "dia útil especial" têm que concordar dia a dia por um mês inteiro) e um oráculo de
  forecast escrito à mão, independente do código que ele valida.

## 7. Segurança e enterprise

RBAC por folder (ACLs), OIDC/SSO opcional, mTLS server↔agente, trilha de auditoria com
export a SIEM, tokens por agente, secrets via provider (env/arquivo — o PAT do GitHub
nunca precisa persistir em claro no state store), sandbox de execução para agentes
multi-tenant (container sem capabilities, sem mounts, uid dedicado).

E o item menos técnico da lista, que era o que realmente barrava tudo: **licença**. O
repositório ficou público por meses sem `LICENSE` — e sem licença vale o direito autoral
padrão, "todos os direitos reservados": qualquer um podia ler, ninguém podia legalmente
usar. Jurídico de banco ou seguradora — exatamente o público-alvo declarado — barra
dependência sem licença antes de qualquer POC. Hoje é **Apache 2.0**, escolhida em cima
de MIT pela **concessão explícita de patente** e pela cláusula de marca: é a norma do
ecossistema Go/infra (Kubernetes, Prometheus) e passa em revisão jurídica corporativa sem
discussão. Copyleft foi descartado de propósito — espanta adoção corporativa, que é
justamente o gargalo do projeto.

## 8. Deploy de 1 caixa — e a primeira instalação REAL (o teste ácido)

`curl … | sudo bash` num VPS Linux: baixa o bundle da release (binário + SPA + deploy),
registra systemd, serve UI+API+WS numa origem, `regente-configure` guia token forte +
PAT via secrets provider, nginx+certbot dão TLS com renovação. O mesmo produto que faz
HA com Postgres roda inteiro numa máquina de US$5 — essa amplitude era um objetivo, não
um acidente.

Só que "tem instalador, e o smoke test passa" não é a mesma coisa que "instala". Em
2026-07-28 eu instalei num VPS pago de verdade: Ubuntu 24.04, domínio próprio, DNS no
registrador, certificado do Let's Encrypt. **A instalação em si passou limpa — e mesmo
assim apareceram nove furos.** O padrão entre eles é a lição inteira: *todos* estavam
FORA da fronteira do que o smoke test rodava. Ele provava install + configure +
persistência, e não tocava em três coisas — a borda (nginx/TLS/DNS), a entrada **colada**
num terminal, e o primeiro dia de operação.

Os três que mais doeram:

- **O terminal corrompeu o PAT.** Colar num terminal em modo bracketed-paste entrega
  `\e[200~<texto>\e[201~` ao `read` — e com `-s` (silencioso) nada disso aparece na tela.
  O escape foi junto pro `server.env`, o **systemd descartou a linha inteira** do
  EnvironmentFile, e o sintoma que chegou até mim foi `git clone: authentication
  required: Repository not found` — três saltos de distância da causa, e com cara de
  problema de permissão no GitHub.
- **A daily carimbou o dia com zero jobs.** Ela rodou antes de o GitOps conectar,
  materializou 0 instances e marcou a data como processada. Dali em diante o dia estava
  "feito": nem com o clone pronto o board voltava. O que o operador vê é "instalei e
  subiu sem job nenhum".
- **Reinstalar por cima não atualizava.** É o caminho de upgrade que o próprio README
  manda usar, e `systemctl enable --now` só INICIA unit parada — numa unit já ativa, o
  start é no-op. O binário novo ia pro disco e o processo ANTIGO seguia rodando: o
  operador reinstalava, via tudo "ok" na tela, e nada mudava.

Os nove foram corrigidos — e o smoke test **cresceu para cobrir a borda**: sobe nginx de
verdade no container, aplica o conf real, prova o circuito com Host forjado (proxy, UI,
API autenticada, headers de hardening) e verifica que `deploy/vps` existe na máquina —
exatamente a asserção que teria pego o primeiro furo antes de mim.

## 9. O próximo passo: IA que não sai do perímetro *(roadmap, spec pronta)*

O público de orquestração enterprise vive em ambiente regulado — bancos, seguradoras,
utilities — onde sysout, logs e dados de host **não podem sair do perímetro**. Todo
scheduler "com IA" do mercado manda esses dados pra uma API de nuvem; o próximo jobType
do Regente, o `AI_AGENT`, faz o inverso: a LLM roda **no mesmo host do agente**
(Ollama/llama.cpp/vLLM), analisa sysout e logs ali mesmo, e o server recebe só o
veredito — nenhum dado viaja. Como `AI_AGENT` é um jobType igual aos outros, Confirm,
conditions, SLA, RBAC e audit trail funcionam sem uma linha nova, e o prompt — que é
parte da definição — **congela no snapshot imutável da ordem**, auditável pra sempre.
A primeira fase é análise-only, **sem tools**: um prompt injetado no máximo produz um
relatório errado, nunca um comando executado.

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
7. **O que o teste não roda, apodrece.** Nove furos numa instalação que o smoke test
   dava como verde — e todos exatamente onde ele não olhava. A fronteira da sua suíte
   é a fronteira da sua confiança; fora dela você não tem software testado, tem
   suposição com badge verde.

## 11. Números do projeto

- **Código:** ~47k linhas Go (server+agent) · ~24k linhas TS/TSX (UI).
- **Testes:** 460 funções de teste Go em 103 arquivos + validações E2E ao vivo.
- **Executores:** COMMAND · SCRIPT · HTTP/REST · SSH agentless · DATABASE · FILE_WATCH
  · MFT · WASM · K8s · Lambda · Batch · Glue · Step Functions · Cloud Run.
- **Escala:** 1M jobs/dia validado ao vivo (write 17s · summary 51ms · UI virtualizada).
- **Deploy:** binário único (SQLite) → HA multi-nó (Postgres + leader election) →
  serverless portátil (tick externo) — instalado e operando num VPS público real.
- **Interface:** UI React (Design/Monitoring), 17 temas, CLI, OpenAPI, MCP (22 tools).
- **Licença:** Apache 2.0 (com concessão explícita de patente).
- **Código:** github.com/Dr0nj/regente · **Documentação:** dr0nj.github.io/regente

---

*Escrito em 2026-07-13, ao fechar a Fase Z do roadmap; revisado em 2026-07-21 (condições
E/OU e carry-over por data de origem) e em 2026-07-28 (primeira instalação real em VPS
público e os nove furos que ela achou · licenciamento Apache 2.0 · números recontados).
Detalhes de cada entrega, com datas e commits, no roadmap do repositório:
github.com/Dr0nj/regente*
