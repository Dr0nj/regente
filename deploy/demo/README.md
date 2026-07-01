# Hospedar a demo do Regente pra amigos testarem

Objetivo: um **link https público** onde amigos entram, **criam e executam jobs de
verdade**, sem você abrir sua máquina nem seu GitHub além do necessário.

## Como funciona (a arquitetura da demo)

```
  amigos (browser)
        │  https://<algo>.trycloudflare.com
        ▼
  Cloudflare Tunnel  ──►  regente-server (seu PC, :9091)
                              │   • serve o SPA buildado  (mesma porta → sem CORS)
                              │   • API + WebSocket        (mesma porta)
                              │   • GitOps DIRETO ──► github.com/Dr0nj/regente-workspace
                              ▼
                          agente em CONTAINER Docker (descartável)
                              • jobs COMMAND/SCRIPT/HTTP rodam AQUI DENTRO
                              • sem volumes do host, não-root, cap-drop, limites de CPU/RAM
```

Decisões desta demo (as que você escolheu):
- **Cloudflare Tunnel** — grátis, sem conta, sem cartão. O link é efêmero
  (`*.trycloudflare.com`) e muda a cada vez que você sobe — **não precisa rebuildar** o
  front, porque ele usa `window.location.origin` (qualquer URL do túnel funciona).
- **Execução em Docker isolado** — jobs rodam de verdade, mas presos num container
  descartável. Os comandos dos amigos **não tocam sua máquina/arquivos**.
- **GitOps direto no `regente-workspace` real** — jobs criados viram commit direto no
  `main`. Simples e fluido (sem PR no meio). Ver "Segurança" abaixo.

## Pré-requisitos

- **Go 1.25+** e **Node/npm** (build do server e do front)
- **Docker Desktop LIGADO** (o agente sandbox)
- **cloudflared** no PATH: `winget install --id Cloudflare.cloudflared`
- Um **GitHub PAT** com permissão de **push** em `Dr0nj/regente-workspace`
  (fine-grained: Contents = Read and write nesse repo). O script reusa o token salvo
  em `%LOCALAPPDATA%\regente-lab\github-token.txt` se existir; senão, ele pergunta.

## Subir

Na raiz do repo, no PowerShell:

```powershell
.\deploy\demo\host-demo.ps1
```

Se aparecer **"a execução de scripts foi desabilitada neste sistema"** (política padrão do
Windows), rode de uma destas formas — **nenhuma precisa de admin**:

```powershell
# opção A — só desta vez, sem mudar nada no sistema:
powershell -ExecutionPolicy Bypass -File .\deploy\demo\host-demo.ps1

# opção B — libera scripts locais pro seu usuário de vez (roda 1x e pronto):
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
.\deploy\demo\host-demo.ps1
```

O script: builda o front (`@origin`), builda e sobe o server em `:9091` servindo tudo
numa origem só, builda e sobe o agente em Docker, e abre o Cloudflare Tunnel. **Copie o
link `https://<...>.trycloudflare.com`** que aparecer e mande pros amigos.

Login inicial: **admin / admin** (ele obriga a trocar a senha na 1ª vez).

### Convidar os amigos

Em **Configurações → Usuários**, crie uma conta por pessoa e escolha o papel:
- **operator** — cria e executa jobs (o que você quer pra quem vai testar de verdade)
- **viewer** — só observa (bom pra quem só vai dar feedback visual)

Assim cada amigo entra com o próprio login (não compartilhe o admin).

## Parar

Feche a janela do `cloudflared` (Ctrl+C) e:

```powershell
docker rm -f regente-sandbox
Get-Process regente-server -ErrorAction SilentlyContinue | Stop-Process -Force
```

## Segurança — leia antes de convidar gente

Você está expondo um orquestrador que **executa comandos**. Mitigações já embutidas:
- Execução **dentro do container** (sem mount do host, não-root, `--cap-drop ALL`,
  `--security-opt no-new-privileges`, limites de PID/CPU/RAM). O que os amigos rodam
  fica preso ali; derrubar/reciclar é `docker rm -f regente-sandbox`.
- **Contas por pessoa** com RBAC (operator/viewer), em vez do admin compartilhado.
- Token de API **aleatório** por sessão (o script gera um a cada run).

Pontos de atenção (porque você escolheu escrita direta no repo real):
- Jobs criados **commitam direto no `main`** do seu `regente-workspace`. Se algum amigo
  bagunçar, é `git revert` no repo. Se preferir revisão no meio, troque no script
  `-git-write-mode direct` por `pr-required` (aí cada mudança vira PR pra você aprovar —
  precisa de PAT com permissão de PR).
- O container tem saída de rede (jobs HTTP e `COMMAND` podem acessar a internet). Se
  quiser cortar isso, rode o agente com `--network none` (mas aí jobs HTTP/rede param).
- O link `trycloudflare.com` é **público**: quem tiver a URL vê a tela de login. A
  proteção é o login/RBAC — só entregue contas a quem você confia.

Convide poucos, de confiança, e derrube a demo quando terminar.
