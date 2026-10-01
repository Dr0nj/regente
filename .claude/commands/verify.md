---
description: Executa verificação com cobertura explícita (quick ou full)
---

Leia [o contrato de verificação](../../docs/verification.md).

Execute `bash scripts/verify.sh --quick` por padrão; no Windows,
`python scripts/verify.py --quick`. Se o pedido exigir full, execute
`bash scripts/verify.sh --full` em Linux/amd64 com os pré-requisitos documentados.
Nunca substitua full por quick silenciosamente.

Informe perfil, resultado, gates omitidos e caminho do relatório. Falha ou
pré-requisito ausente não é sucesso. Corrija a causa e execute novamente o gate
pertinente; não mascare saída, remova gate ou apresente skip como aprovação.
CI acrescenta matriz Node/Windows; release e smoke systemd têm resultado próprio.
