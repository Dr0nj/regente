# Plano interno — capacidade e recuperação do Regente

Data: 07/11/2026. Prioridade imediata. Este documento define o trabalho; o único
registro de status é a trilha PERF-00–06 em [roadmap](../roadmap.md).
Responsabilidade: manutenção do servidor Go/API/storage/scheduler; agente, UI e
operação participam dos testes e contratos afetados. Não há compromisso artificial
de prazo: cada incremento avança pelo gate de evidência abaixo.

## Objetivo e limites

Estabilizar a carga diária de 28.800 jobs com execução, auditoria, agentes e UI,
preservando identidade de operações, snapshots congelados, ODAT/carry-over,
condições AND/OR, recursos, permissões e trilha auditável. Construir a evolução para
100 mil/1 milhão sem alegar capacidade ainda não demonstrada. Publicar este plano
não altera binários nem configurações de produção.

Referências obrigatórias na implementação: [condições e eventos](../conditions-events.md),
[execução durável](../durable-execution.md), [limites de capacidade](../capacity-guarantees.md)
e [arquitetura](../architecture-future.md). Confirmar os pontos de código na main
vigente; não reaplicar cegamente patches de checkouts anteriores.

## Evidência que motiva a prioridade

- VPS examinada com 2 vCPUs: aproximadamente 70% steal, restante ocupado, RAM
  disponível próxima de 5 GB e pouca espera por disco. O histórico mostrou mais
  de três horas de uso real próximo de 100% antes do steal elevado. Restrição do
  provedor é hipótese compatível, não causa inicial comprovada da carga.
- Em 07/11, 28.774 instâncias WAITING; fila control em 10.000 pendências; agente
  repetindo ACK com HTTP 503. Coletor vivo, confirmando lentamente.
- Das últimas 1.000 entradas control examinadas: 839 request.intent de ACK,
  147 request.result de ACK e 14 entradas de ticket. Resultados 200 persistidos
  coexistem com os 503 recebidos pelo agente. Isso é coerente com o middleware
  que registra intenção antes do handler e pode trocar a resposta por 503 ao
  falhar a auditoria posterior. A distribuição não prova o resultado de cada retry.
- Benchmark sintético existente de normalização com 28.800 jobs, Windows/i7:
  baseline cb9022e via overlay = 18,7068 s (1 iteração); implementação indexada
  local ad48190 = 11,2464 ms em média (3 iterações). Perfil baseline atribuiu
  98,76% acumulado a deriveUpstreamView. Não inclui leitura de YAML, ACL,
  serialização, rede ou o servidor inteiro; não é previsão da VPS.
- FileStore.List normaliza definições; definitions e workspaceView o usam.
  wsWeb chama workspaceView antes do upgrade. Na janela de 5.000 linhas do nginx
  examinada, houve 25 /ws/web com 499 e cinco /api/definitions com 504.
- A investigação não capturou CPU profile do processo vivo. Atribuição percentual
  da saturação por rotina continua pendente em PERF-00.

## PERF-00 — medir desde o início

Adicionar suporte opt-in a pprof em listener administrativo separado em loopback,
com acesso por SSH e limites operacionais. O binário examinado não oferece esse
endpoint: túnel/firewall não substituem implementação. Se habilitar exigir trocar
binário/reiniciar, planejar uma janela e registrar a ruptura da baseline.

Medir execução normal, boot, leitores/reconexões, fila cheia, exportação e scheduler.
Perfis sintéticos de saturação ficam em laboratório; produção usa janelas limitadas
e carga existente. Não publicar heap/goroutines com dados privados. CPU profile,
heap e goroutines têm papéis diferentes; mutex/block profiling só com custo avaliado.
Usar serviço real regente-server. Visualizador pprof em porta local livre, sem
disputar a 8080 do produto.

Registrar versão/SHA, hardware, dataset, throughput, p50/p95/p99 de despacho,
CPU user/system/steal, RSS/GC, I/O, idade e profundidade de filas, último progresso
concluído, contato real do agente e falhas por classe. Separar /health de capacidade
de admitir/concluir; flag online antiga não comprova comunicação atual.

Gate: baseline reproduzível e perfis comparáveis; definir orçamento de latência,
memória, CPU e recuperação antes de medir o ganho final, sem inventar SLA retrospectivo.

## PERF-01 — índice e snapshot consistente

Pontos de entrada: internal/domain/conditions.go, internal/storage/file.go,
internal/api/ws.go e web_events.go, leitores de definições e reload do scheduler.

1. Revisar a correção indexada existente. Mapear base da condição para todos os
   produtores, incluindo ações set-condition; preservar ordem/deduplicação e
   regra atual de referência de data. Não introduzir prioridade, DateRange ou
   outras semânticas apenas porque aparecem em pseudocódigo.
2. Snapshot imutável por workspace e geração. Campos privados e consultas limitadas;
   cópia profunda somente nas fronteiras que precisam devolver objetos mutáveis.
   Nenhuma cópia de todo o grafo por consumidor. Leitores autorizados filtram a visão
   sem alterar o snapshot; resultado filtrado nunca é compartilhado entre permissões
   incompatíveis. Revogação continua efetiva.
3. Geração monotônica atualizada nas gravações/sync conhecidos. Alterações externas
   precisam de detecção e reconciliação periódica, incluindo perda de eventos,
   criação, exclusão e renome. SHA ou mtime isolados não identificam conteúdo.
   Digest determinístico quando necessário, fora do caminho de cada requisição.
4. Construir sobre revisão capturada consistentemente: lock/coordenador de escrita,
   cópia estável ou revalidação robusta antes de publicar. Mudança durante leitura
   invalida a construção; não publicar mistura de revisões. Atualização deve validar
   e publicar o conjunto completo.
5. Singleflight por workspace/geração e comparação de geração na publicação.
   Uma construção antiga terminando depois não pode substituir a nova. O contexto
   da construção pertence ao serviço, com timeout; o cancelamento de um leitor só
   abandona sua espera. Shutdown cancela os trabalhos. Limitar construções e memória.
6. WebSocket usa snapshot pronto; refresh de definições não bloqueia seu handshake
   com reconstrução completa. Autenticação/ACL continuam obrigatórias. Cold start e
   falha de construção têm resposta/readiness explícitas. Não despachar silenciosamente
   sobre definição inválida ou revisão diferente da admitida.
7. Manter apenas geração atual e snapshots em uso; liberar antigos ao término dos
   leitores. Não criar cache ilimitado de revisões, usuários ou grafos.

Gate: múltiplos produtores/data/ordem; mudança local sem SHA; alteração durante build;
build antigo após novo; dez leitores com um build por geração; primeiro leitor
cancelado sem prejudicar demais; ACL/revogação; erro e recuperação; ausência de
retenção crescente. WebSocket quente não chama List/Normalize. Medir lookup,
rebuild, leitura, normalização, filtro, serialização e envio separadamente.

## PERF-02 — concluir e recuperar operações sob pressão

Pontos: internal/api/security_audit.go, execution_lab.go, protocolo do agente,
internal/execution e internal/db/audit.go. Mapear identidades duráveis já existentes
por ACK/result/output/cancel; não criar outro registro paralelo de idempotência.

- Consultar replay autenticado/autorizado usando identidade e conteúdo da operação;
  chave igual com conteúdo diferente é conflito. Operação aplicada recupera recibo
  persistido, sem uma nova intenção obrigatória por retry.
- Para mudança inteiramente transacional, persistir estado, evidência obrigatória e
  decisão recuperável na mesma transação. Separar auditoria obrigatória da operação
  de observações de transporte. Middleware posterior não pode converter resultado
  já comprometido em falha apenas por lotação de uma segunda fila.
- Reservar duravelmente o orçamento necessário antes da admissão; definir quantidade
  e bytes por classe, concorrência, liberação e recuperação. Reservar espaço para
  conclusão/recibos/cancelamento/reconciliação de trabalho já admitido. Novas admissões
  cedem primeiro. Conservar limites no SQLite e Postgres; migrar conforme contrato.
- Capacidade reservada não garante disco funcional. Se a persistência falha, não
  confirmar sucesso não durável. Efeito externo ambíguo continua UNCERTAIN e segue
  o protocolo existente; não reexecutar automaticamente efeitos não idempotentes.
- Inventariar operações já admitidas ao migrar uma fila cheia. Definir recuperação
  limitada e observável, sem apagar trilha, forjar recibos ou desabilitar assinaturas.
- Tratar retry/backoff do agente como proteção complementar, sem esconder falha
  permanente nem mudar identidade da operação. Resposta recuperável tem contrato
  explícito para versões de agentes já instaladas.

Gate: capacidade quase cheia/cheia, chamadas concorrentes, sucesso seguido de conexão
perdida, crash antes/depois do commit, reinício, replay, payload conflitante, ACL,
disco indisponível e efeitos incertos. Nenhuma operação de banco parcialmente
comprometida; nenhum crescimento ilimitado de intenções causado pelo mesmo retry.
Definir igualdade entre commits concluídos e recibos recuperáveis; não confundir com
efeitos externos que aguardam reconciliação. Testar migração e retorno compatível.

## PERF-03 — exportar prefixos duráveis em lote

Ponto: internal/audit/durable.go e coletor. Lote limitado em registros, bytes e tempo;
verificar assinaturas e continuidade. Coletor só confirma prefixo após persistência
durável, deduplica reenvios pela identidade/cadeia e rejeita divergência.

Reutilizar cadeia/outbox durável e cursor confirmado. Não criar estados selecionado/
enviado apenas por conveniência: eles não provam recepção e podem ser reconstruíveis.
Formalizar propriedade do exportador em HA. Validar stream, sequência e hash do ACK,
sem aceitar prefixo além do lote, sem lacunas; só depois avançar monotonicamente em
transação. Crash depois do recebimento e antes do ACK local implica replay seguro.
Compatibilidade de protocolo/versões deve prever rollout e fallback explícitos.

Gate: ACK perdido, lote parcial, resposta inválida, hash divergente, lacuna, duplicata,
reinício de produtor/coletor e concorrência. Drenagem sustentada superior à produção
no cenário acordado; medir lag e previsão de saturação. Verificar custo de fsync,
locks e CPU separadamente; coletor no mesmo host continua no mesmo domínio de falha.

## PERF-04 — scheduler incremental com reconciliação

Pontos: scheduler.go/tickOnce, condições, recursos, capacidade de agentes, daily e
runtime durável. Índices da execução derivam dos snapshots congelados das instâncias,
não apenas do workspace vivo: preservar ODAT, carry-over, data da condição, AND/OR,
$TIME, Force, Confirm, retries/cyclic e On/Do.

Manter candidatos por horário e consumidores por condição/escopo, além dos bloqueados
por agente/recurso. Mudanças Add/Delete, disponibilidade/capacidade, hold/release,
confirm, cancel e resultados despertam candidatos relevantes. Deduplicar fila;
notificações duráveis ou recuperáveis via outbox/reconciliação. Eventos antigos não
podem despachar snapshots novos ou ultrapassar geração/fence da execução.

Avaliação limitada em itens/tempo, fairness e backpressure; claim/reserva final
transacionais e liderança preservada. Explicar bloqueio usa a mesma semântica.
Reconciliar páginas com cursor estável/checkpoint e orçamento, recuperando eventos
perdidos e reinícios sem monopolizar CPU. Cache de snapshot validado por identidade
e checksum; mudança de status não exige desserializar todo o plano novamente.

Gate: comparação contra avaliador atual em modo shadow sem efeitos duplicados;
paridade em condições/tempo/agentes/recursos; eventos duplicados/perdidos/reordenados;
virada, carry-over, restart/failover e starvation. Quiescência não dispara varredura
completa a cada tick; reconciliação ainda garante progresso eventual verificável.

## PERF-05/06 — aceite, implantação e escala

Cada incremento: testes dirigidos de contrato, regressões afetadas, documentação de
operação e comparação de métricas. Mudanças de comportamento seguem os gates do
projeto. A exceção [skip ci] desta publicação é exclusivamente de planejamento.

Primeira qualificação: 28.800 jobs com topologia/dependências representativas,
agentes reais, auditoria ligada, leitores/reconexões, carga de pico e pelo menos um
ciclo de 24h incluindo virada. Comparar throughput útil, p95/p99 de despacho,
latência de recibos, CPU sem misturar steal, RSS/GC, backlog e tempo de recuperação.
Sem backlog que cresce indefinidamente; nenhum recibo perdido ou efeito duplicado.

Antes do rollout: fixar versões/compatibilidade, backup verificado de DB e demais
artefatos, procedimento de recuperação e critérios de interrupção. Aplicar uma
mudança por vez; observar execução e conclusão, não apenas health. Rollback só com
binário/schema compatíveis; restore exige reconciliar efeitos posteriores. Migração
irreversível precisa de estratégia explícita; não sobrescrever DB vivo para teste.

Depois, patamares 100 mil, intermediários e 1 milhão: documentar hardware, pico,
duração dos jobs, paralelismo, throughput e perfil de leitores. Um milhão/dia é cerca
de 11,6 conclusões/s em média, mas o ensaio deve incluir rajadas e cadeia completa.
Falhar num patamar bloqueia alegação de capacidade, não autoriza reduzir o teste.

## Métricas mínimas e rastreabilidade

Usar convenção Prometheus: durações em segundos como histogramas; contadores com
_total; filas/reservas como gauges; nomes finais alinhados à instrumentação existente.
Evitar labels de job/operação/revisão/usuário de cardinalidade ilimitada.

- Snapshot: hit/miss/build, inflight, geração regressiva rejeitada e duração por etapa.
- Auditoria: rejeições de admissão, reservas em uso, resultado/recibo persistido,
  replay recuperado, falhas, profundidade/idade/bytes por classe, produção e drenagem.
- Exportação: tamanho/latência de lote, avanço de prefixo, gap/hash inválido e replay.
- Scheduler: avaliações incrementais, páginas reconciliadas, idade do checkpoint,
  atraso de candidatos, último ciclo concluído e fila por motivo limitado.
- Operação: throughput de conclusões, p95/p99, contato recente de agentes e capacidade
  real de admitir/concluir separada da liveness.

Cada PERF encerrado referencia commit, testes, dataset e limitações no roadmap;
somente então move de Backlog para Entregue/Changelog. Urgência determina a ordem,
não transforma benchmark sintético ou documentação em aceite produtivo.

## Decisões de implementação para validação local (09/11)

Detalhes operacionais no [runbook](../performance-recovery.md); status exclusivamente
no roadmap. Snapshot privado por FileStore, serialização de publicação com escritas
coordenadas e captura dos bytes efetivamente parseados. Edições externas são
reconciliadas a cada 15s; publicação multi-arquivo exige coordenação/atomicidade.

Schema 33 adiciona sete reservas por tentativa admitida e inventário das tentativas
não terminais anteriores. Estados e evidência usam a mesma transação; heartbeat e
contadores de output são telemetria por projeção SQL exata. ACK/result/output usam
recibos v2 existentes, sem nova identidade artificial nem intenção a cada retry.

Lotes limitados a 128 registros/1 MiB, payload obrigatório até 64 KiB, ACK apenas do
prefixo persistido e sincronizado pelo coletor. Default de um registro até upgrade
do coletor. Scheduler incremental fica opt-in: notificações coalescidas no banco,
256 notificações/página por tick, orçamento de avaliação de 100ms, timers e cache
de até 1.024 planos. Reconciliação espera 30s após uma passagem inteira, não promete
recuperar todo evento perdido em 30s. Claims/fencing/snapshots congelados preservados.

Harness diário exige 28.800 definições distintas executadas, 24h e virada de ODAT;
a massa histórica não substitui efeitos verificados. O grafo sintético usa 900
componentes com fan-out e cadeias curtas, condições permanentes e mix COMMAND/HTTP,
output/retry. Captura perfis e CPU/steal com leitores/reconexões concorrentes.
Condições por data/carry-over continuam exigindo testes de paridade e aceitação da
topologia real. Patamares maiores só avançam após o gate diário e o anterior passar.
