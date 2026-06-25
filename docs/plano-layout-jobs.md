# Plano — Layout de jobs (grade pros soltos, fluxo pros dependentes)

> Refina o item do roadmap (§Identidade visual / UI). Escopo POR FOLDER.

## Diagnóstico (o que já funciona vs. o gap)

O canvas posiciona cada folder como uma "lane" e roda **dagre TB** por folder em
`V2Preview.tsx › layoutFolderInner` (constantes `NODE_W`/`NODE_H`, `NODE_GAP_X=36`,
`NODE_GAP_Y=28`). `composeColumns` empilha as folders lado a lado.

- ✅ **Dependentes já ficam certos:** dagre TB faz layout em camadas — A na linha 1,
  B e C lado a lado na linha 2, cadeia A→B→C em 3 linhas. Nada a mudar aqui.
- ❌ **Soltos quebram:** jobs sem aresta interna caem todos no **rank 0** do dagre →
  uma **fila horizontal infinita** que estica pro lado.

**Conclusão:** a correção é cirúrgica — tirar os SOLTOS do dagre e dispô-los numa
GRADE; deixar os CONECTADOS no dagre como estão.

## Regras

Por folder, particiona os jobs em:
- **Conectados** — têm ≥1 aresta INTERNA (upstream/downstream dentro da folder).
- **Soltos** — sem nenhuma aresta interna.

1. **Conectados** → dagre TB (como hoje). Vira a "zona de fluxos".
2. **Soltos** → GRADE: `effectiveCols` colunas, preenchendo da esquerda pra direita,
   quebra pra baixo ao encher a linha. Job nº (k) → linha `floor(k/cols)`, coluna `k%cols`.
3. **Alargamento** (cap soft): `effectiveCols = max(columns, ceil(qtdSoltos / maxRows))`.
   Mantém a altura ≤ ~`maxRows` linhas; passou disso, cresce em LARGURA.
4. **Composição na folder:** zona de fluxos (dagre) no topo; GRADE de soltos
   abaixo, com um gap vertical (`~2×ranksep`). Folder só com soltos = só a grade.
5. **Isolamento:** tudo calculado por folder; a folder X não afeta a Y (já é assim —
   `layoutFolderInner` roda por team e `composeColumns` empilha).

### Análise dos parâmetros (melhor cenário)

| Param | Default | Range | Por quê |
|---|---|---|---|
| `columns` | **10** | 4–20 | 10 colunas × (~`NODE_W`+gap) é largo mas pannável; honra o pedido. 6–8 se quiser mais compacto. |
| `maxRows` | **30** | 10–80 | 50 vira ~6000px de scroll vertical; 30 mantém usável. Abaixo do limiar (`columns×maxRows`=300 soltos) os dois se comportam igual — só difere em folders gigantes (raras; aí o ViewPoint/ScaleMonitor é o caminho). |

Fórmula completa por folder:
```
cols = max(columns, ceil(standalone.length / maxRows))
rows = ceil(standalone.length / cols)
x(k) = (k % cols) * (NODE_W + NODE_GAP_X)
y(k) = floor(k / cols) * (NODE_H + NODE_GAP_Y)   // + offset abaixo da zona de fluxos
```

## Implementação

### Fase 1 — algoritmo (core, sem config) — pequeno e isolado
**Arquivo:** `app/src/v2/V2Preview.tsx › layoutFolderInner`.
1. Calcular `innerEdges` (já existe). Derivar `connectedIds = Set(source/target das innerEdges)`.
2. `connected = members.filter(in connectedIds)`; `standalone = members.filter(!in connectedIds)`.
3. Rodar dagre SÓ em `connected` (como hoje) → posições + bounding box da zona de fluxos
   (`flowH` = altura, `flowW` = largura).
4. Grade dos `standalone`: aplicar a fórmula acima; `gridY` começa em `flowH + GAP` (abaixo
   dos fluxos), `gridX` em 0.
5. Unir as posições; recomputar `width = max(flowW, gridW)`, `height = flowH + GAP + gridH`.
   Retornar como hoje (origem 0,0).
- **Edge cases:** folder só com soltos (flowH=0, sem gap) · só com conectados (sem grade) ·
  job que é só upstream de outro já conta como conectado.
- **Determinismo:** ordenar `standalone` por id/label (estável) pra a grade não "pular" entre renders.

### Fase 2 — configuração
- **Global (default):** `layout_columns` e `layout_max_rows` em `ServerSettings` (settings-api +
  tabela settings) e na aba **Geral** do SettingsDialog (2 inputs numéricos). `layoutFolderInner`
  recebe `{columns, maxRows}`.
- **Override por folder (opcional):** persistir em `.regente-folder.yaml` (já existe stub por
  folder no storage). UI: um campo no header da lane/folder ou no FolderManagerDialog. Resolve
  `cfg = folderOverride ?? globalDefault`.

### Fase 3 — polimento
- Botão "Auto-organizar" por folder (re-aplica o layout se o usuário arrastou nós).
- Minimap (`NavMinimap`) refletir a grade (item separado do roadmap).
- (Opcional) animação/transição suave ao re-gridar.

## Testes
- Unit do particionador + grade (função pura): N soltos → posições esperadas; alargamento
  (N > columns×maxRows → cols cresce); folder só-soltos / só-conectados; determinismo.
- Validação visual ao vivo: folder com 25 soltos (grade 10×3) + uma cadeia A→B→C; folder com
  600 soltos (alarga pra ~20 cols, ~30 linhas).

## Risco / esforço
Baixo. Fase 1 é uma função isolada (sem tocar dispatch/dados/edges); o dagre dos conectados
fica intacto. Fase 2 é plumbing de config conhecido (settings + 2 inputs). Reversível.
