# Plano de correção documental e operacional — 2026-09-21

## Objetivo, escopo e fonte de status

Corrigir os **14 achados D01–D14** da auditoria da baseline
`5318e783665ad80768e9f38bee23fd215032e22b` (v0.2.33): documentação coerente com
código, receitas reproduzíveis e verificações que evitem nova defasagem.
Prioridades: 2 P1, 11 P2 e 1 P3.

O [Backlog do roadmap](../roadmap.md#-backlog-o-que-falta) é o **único registro de
status** de DOC-01–DOC-14. DOC-01 corresponde a D01, e assim sucessivamente.
Este arquivo descreve execução/aceite, não uma segunda lista de conclusão.
Ao entregar, mover o item do Backlog para Entregue/Changelog com commit e evidência.

**Escopo desta entrega:** registrar plano e fila. Não significa executar as
correções, alterar semântica do produto ou implantar em produção. Plano interno
em português; futuras alterações em documentação publicada, mensagens de scripts
e contratos de API continuam em inglês, conforme a convenção do produto.

## Estratégia e dependências

Ordem recomendada: **A → B → C → D → E**.

- A elimina orientações com risco de perda/indisponibilidade e fixa limites.
- B corrige receitas/scripts; C reconcilia contratos usando os limites de A.
- D reconcilia garantias e apresentação pública com a evidência disponível.
- E consolida automação de checks e fecha a revisão transversal.

Cada etapa inclui os testes pertinentes e regeneração do site. E **não adia** a
validação das anteriores. Antes de cada incremento, revalidar HEAD e contratos:
a baseline da auditoria não autoriza regredir mudanças posteriores.

## A — Operação segura e migrações

**Primeiro incremento recomendado:** DOC-01, DOC-02 e DOC-06.

### DOC-01 / D01 — P1 — Upgrade, compatibilidade e rollback

**Fontes/arquivos:** `README.md`, `docs/operations.md`, `docs/authentication.md`,
`server/internal/db/migrate.go`, mensagens e receitas de `server/deploy/`.

**Implementação recomendada:** criar matriz binário/schema/protocolo baseada em
pares verificados; distinguir upgrade compatível, migração coordenada e recuperação
por backup. Retirar downgrade irrestrito, rollback por mera cópia de `.bak`,
rolling upgrade universal e promessa fixa de segundos. Revisar mensagens de
update/rolling-upgrade que repitam o erro. Não implementar downgrade destrutivo
para fazer um texto antigo funcionar.

**Aceite:** ensaio com banco descartável, backup restaurado/verificado, faixa
incompatível recusada e nenhuma recomendação de coexistência não comprovada.
Verificar procedimentos SQLite/PostgreSQL ou manter a validação faltante aberta.

### DOC-02 / D02 — P1 — DR, drafts e estado fora do banco

**Fontes/arquivos:** `docs/dr-backup.md`, `docs/integration-baseline.md`,
`server/internal/storage/session.go`, scripts de backup/restore.

**Implementação recomendada:** inventariar DB/WAL, definições publicadas, diretórios
de drafts e configuração externa; explicar paths/permissões/quiescência e alcance
de cada backup. Reconciliar a ressalva inicial de drafts com todo o checklist.
Distinguir refresh, restart no mesmo volume, perda de container e failover.
Scripts que salvam só DB devem declarar essa limitação e apontar uma receita
complementar testada; nunca copiar segredos para logs ou artefatos públicos.

**Aceite:** criar draft sintético não publicado, salvar conjunto documentado,
restaurar isoladamente e comparar conteúdo; demonstrar que só o DB não o garante.
Não usar/remover drafts reais. Não declarar durabilidade distribuída implementada.

### DOC-06 / D06 — P2 — Schema corrente e ADR

**Fontes/arquivos:** `docs/integration-baseline.md`,
`docs/adr/001-safe-migrations.md`, constantes/saída de migração do servidor.

**Implementação recomendada:** reconciliar a expectativa corrente com o runtime
(na baseline, `[25,25]`); datar a decisão histórica no ADR e apontar a referência
atual. Acrescentar check da expectativa versus constantes/saída `-migrate-only`.
Não proibir menções legítimas a versões antigas em fixtures/histórico.

**Aceite:** testes DB e check passam; fixture com expectativa corrente errada é
rejeitada. Nenhuma migração nova apenas para corrigir documentação.

**Gate A:** fontes de entrada/especializadas coerentes, ensaios de recuperação,
testes DB e docsite. Evidências com comando, ambiente e resultado.

## B — Instalação, bootstrap e demo reproduzíveis

**Itens:** DOC-03, DOC-04, DOC-05, DOC-12 e DOC-14 (P2).

### DOC-03 / D03 — Identidade de máquina

**Fontes/arquivos:** `app/README.md`, `server/README.md`, `deploy/demo/README.md`,
`deploy/demo/host-demo.ps1`, `docs/agent-identity.md`, `machine_identity.go` e testes.

**Implementação recomendada:** provisionar credencial de máquina pelo fluxo atual;
ID/ambiente/capacidades devem coincidir. Não reintroduzir fallback humano/admin.
Anunciar conexão apenas após presença autenticada, com timeout e erro claro.
Criar caminho de smoke local sem túnel público; controlar só recursos do teste
e não imprimir credenciais nos resultados.

**Aceite:** estado limpo → provisionamento → agente autenticado → COMMAND sintético
concluído. Token humano/revogado e claims incompatíveis recusados. Exercitar o
launcher PowerShell, não só o teste Go; `docker run` sozinho não prova conexão.

### DOC-04 / D04 — Same-origin no comando de build

**Fontes/arquivos:** README principal, `server/deploy/README.md`, `app/README.md`,
`app/src/lib/server-client.ts`.

**Implementação recomendada:** padronizar POSIX em
`npm ci && VITE_REGENTE_SERVER_URL=@origin npm run build`; fornecer equivalente
PowerShell com variável no ambiente do build. Reconciliar todas as ocorrências.

**Aceite:** sem `.env` ou variável herdada, SPA servida pelo Go usa a mesma origem,
não localStorage silenciosamente. Preservar modo local intencional.

### DOC-05 / D05 — Node e toolchain

**Fontes/arquivos:** READMEs, `app/package-lock.json` e workflow de build.

**Implementação recomendada:** alinhar mínimo declarado às engines e ao CI;
baseline Vite/plugin React/rolldown: `^20.19.0 || >=22.12.0`. Acrescentar check de
consistência. Não remover lockfile/atualizar dependências sem necessidade.

**Aceite:** instalar/buildar na versão mínima declarada e na usada em CI; requisito
documental abaixo da engine deve falhar no check.

### DOC-12 / D12 — GitOps explícito

**Fontes/arquivos:** `docs/operations.md`, flags de `server/main.go`, guias de instalação.

**Implementação recomendada:** informar origem (`-git-source`/`REGENTE_GIT_SOURCE`),
branch, credencial e owner/repo do operador. Não usar workspace pessoal como default.
Explicar que `-github-repo` sozinho não ativa sincronização.

**Aceite:** sem configuração herdada, receita lê workspace de teste pela origem
declarada. Sem origem, modo offline é explícito. Não escrever em repo real no smoke.

### DOC-14 / D14 — Rede dos jobs versus canal do agente

**Fontes/arquivos:** `deploy/demo/README.md`, `deploy/vps/README.md`, `agent/main.go`.

**Implementação recomendada:** retirar `--network none` como receita para cortar
apenas rede de jobs; explicar que também corta WS/HTTP/SSE do agente. Documentar
egress restrito que preserve o control plane **somente se houver mecanismo
suportado e testável**. Se não houver, declarar limitação, sem inventar isolamento.
Separar executor/rede é capacidade de produto com escopo próprio, não simples doc.

**Aceite:** receita suportada preserva conexão/dispatch/resultado e demonstra
qualquer restrição alegada. Alternativa válida: retirar a recomendação inviável
e declarar explicitamente o isolamento fino não suportado, sem marcar a futura
capacidade como entregue.

**Gate B:** receitas sem estado herdado, POSIX/Linux e PowerShell/Windows cobertos
onde correspondem; nenhum túnel/recurso público automático; site regenerado.

## C — Contratos de API e comportamento operacional

**Itens:** DOC-07, DOC-08 e DOC-09 (P2). Não mudar comportamento correto para
adaptá-lo a texto antigo.

### DOC-07 / D07 — Browser, API, máquina e modos de autenticação

**Fontes/arquivos:** `server/internal/api/openapi.yaml`, `server/README.md`,
`docs/authentication.md`, `docs/agent-identity.md`, guias dos componentes.

**Implementação recomendada:** separar cookie HttpOnly/CSRF, bearer API e credencial
de máquina; rotas públicas; local/hybrid/oidc; event-ticket. Token estático não
contorna SSO-only. Reconciliar exemplos e configuração de segurança da spec.

**Aceite:** spec válida; exemplos e testes positivos/negativos de transporte/CSRF;
integração dos modos aplicáveis. Sem token de browser em URL/localStorage ou
descrito como bearer reutilizável.

### DOC-08 / D08 — Cancelamento por estado

**Fontes/arquivos:** OpenAPI, `docs/mcp.md`, README, descrições MCP e `Scheduler.Cancel`.

**Implementação recomendada:** tabela RUNNING → NOTOK sem retry após sinal;
WAITING/HELD → CANCELLED; terminal → erro. Explicar alertas/On-Do e sinalização
best-effort, sem prometer confirmação de término físico remoto além do protocolo.

**Aceite:** exemplos/respostas reais e `TestCancel_*`; testes API/MCP pertinentes
coerentes com a matriz, sem expectativa universal de CANCELLED.

### DOC-09 / D09 — Rerun e pool de condições

**Fontes/arquivos:** ler **integralmente** `docs/conditions-events.md` antes de atuar;
revisar `docs/case-study.en.md`, versão PT e descrições reaproveitadas.

**Implementação recomendada:** explicar rerun do produtor com condição remanescente,
do consumidor após OK/consumo e após falha sem consumo. Rerun não revoga pool nem
redefine filhos automaticamente. Corrigir exemplos preservando C1–C7/M1.

**Aceite:** exemplos confrontados com testes de condições/API; acrescentar regressão
focada quando faltar cenário, sem alterar a semântica só para conciliar texto.

**Gate C:** scheduler/API, OpenAPI e docsite; contrato uniforme em guia, explorer,
MCP e case study.

## D — Garantias públicas, evidência e status

### DOC-10 / D10 — P2 — Capacidade e HA delimitadas

**Fontes/arquivos:** README, `docs/case-study*.md`, `docs/operations.md`,
`docs/slos.md`, arquitetura e divulgação versionada que repitam as alegações.

**Implementação recomendada:** separar materialização, consulta/UI e execução;
relacionar números a versão/data/ambiente/perfil/operação e artefato disponível.
Sem evidência recuperável, marcar relato histórico não reproduzido, sem inventar
benchmark nem concluir que o número antigo é falso. Distinguir atomic claim de
ACK durável, fencing, recuperação e efeitos externos. Retirar garantia universal
de zero perda/duplicação; não editar posts externos sem solicitação específica.

**Aceite:** alegações atuais rastreáveis ou limitadas explicitamente; não chamar
materialização de 1M de homologação de 1M execuções/dia. Novas garantias dependem
dos incrementos enterprise correspondentes, não deste ajuste editorial.

### DOC-13 / D13 — P3 — Status sem apagar histórico

**Fontes/arquivos:** README, `docs/roadmap.md`, `docs/authentication.md`,
`docs/integration-baseline.md`, `docs/web-events.md`.

**Implementação recomendada:** datar/limitar feature-complete e manutenção às
trilhas antigas; explicitar fila enterprise I05–I17. README aponta ao roadmap,
não duplica tracking. Atualizar referências antigas a I04 futuro.

**Aceite:** I04 não apresentado como futuro em texto corrente; backlog não descrito
como homologado; marcos antigos preservados como histórico identificado.

**Gate D:** revisão pública versus evidência/status e site regenerado. Nenhuma
capacidade nova declarada como entregue por edição de texto.

## E — Proteção contra regressão e encerramento

### DOC-11 / D11 — P2 — Verify quick/full e CI

**Fontes/arquivos:** `scripts/verify.sh`, `.claude/commands/verify.md`, `CLAUDE.md`,
workflows e scripts de integração/browser/docsite.

**Implementação recomendada:** manter verify rápido e oferecer `verify --full`
(ou dois comandos inequívocos). Quick lista gates omitidos; full exige seus
pré-requisitos e falha se gate obrigatório não puder rodar. Skip/indisponível não
é sucesso. Mapear build/vet/test, staticcheck, UI lint/build, docsite, browser e
integração real; compartilhar implementação com CI quando viável.

Consolidar checks focados de schema corrente, engines/receitas, contratos,
links/geração; evitar regex indiscriminada que rejeita exemplos históricos ou
produz confiança falsa. Exercitar falhas intencionais apenas em fixtures/ambiente
isolado: schema errado, link quebrado, HTML defasado e gate obrigatório omitido.

**Aceite:** contrato quick/full verificável, cobertura mapeada ao CI corrente,
sem “same as CI” para execução parcial e checks negativos demonstrados.

### Definição de pronto por item e global

Cada DOC só sai do Backlog quando:

1. Fonte primária e ocorrências relacionadas revisadas, incluindo scripts/traduções.
2. Correção implementada sem ampliar promessa além do mecanismo/evidência disponível.
3. Testes/ensaio do item executados, com comando, ambiente e resultado registrados.
4. Site regenerado pelo gerador, links válidos e diff revisado; nunca editar HTML à mão.
5. Commit rastreável, CI pertinente concluído e evidência em Entregue/Changelog.

Fechamento global: **14 achados resolvidos + revisão cruzada final + CI verde**.
Item sem ambiente/prova necessária permanece aberto com dependência explícita.
Incrementos prontos/testados seguem a política de commit/push e acompanhamento de
CI. `[no release]` é adequado a alteração exclusivamente documental, não a mudança
de comportamento de script/bundle disfarçada de docs.

## Relação com I05–I17 e limites

Esta fila resolve as inconsistências auditadas, não todas as capacidades ausentes
do produto. Durabilidade real de drafts, ACK/fencing, recuperação, HA sob partição
e homologação de volume continuam na trilha enterprise com seus gates próprios.
Não são concluídos por remover uma frase excessiva.

Recomenda-se concluir A antes de novo upgrade orientado pelos guias afetados e B
antes de reutilizar as receitas/demo. Nenhum ensaio autoriza operação destrutiva
em instalação real. Não fixar prazo de calendário sem conhecer os ambientes de
validação; unidade de progresso é item aceito com evidência.
