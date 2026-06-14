# regente-agent

Braço executor local do Regente. Rode **um agente em cada máquina** que deve
executar jobs (seu laptop, um server on-prem, uma VM/EC2, etc.).

Conecta ao `regente-server` via WebSocket **outbound** — sem abrir portas na
máquina do agente (atravessa NAT/firewall). Mesmo modelo de runner do GitHub
Actions / GitLab / Control-M Agent. Executa jobs localmente, devolve o resultado
e **streama stdout/stderr em tempo real** (aparece no detalhe da instance).

## Executores

| jobType | O que faz | Params (actionConfig) |
|---|---|---|
| `COMMAND` | Comando no shell do SO (`powershell -Command` no Windows, `sh -c` no Linux) | `command`, `cwd?` |
| `SCRIPT` | Executa um script; interpretador pela extensão (`.ps1`/`.bat`/`.sh`) | `scriptPath`, `args?`, `cwd?` |
| `HTTP` | Chamada REST com validação de status | `method`, `url`, `headers?`, `body?`, `expectStatus?` |

> `SSH` (comando remoto agentless) **não** usa o agente — roda no próprio server.

## Build

```bash
go build -o regente-agent .      # Linux/macOS
go build -o regente-agent.exe .  # Windows
```

## Rodar (foreground)

```bash
./regente-agent \
  -server ws://SEU-SERVER:8080/ws/agent \
  -token  rgta_...        # Settings → Agentes → Criar token \
  -id     meu-host \
  -caps   COMMAND,SCRIPT,HTTP
```

Token: gere um **token por agente** na UI (Settings → Agentes → Criar token). O
`dev-token` legado também funciona em dev. Sem token válido, o handshake é negado.

## Instalar como serviço

### Linux (systemd)

```bash
sudo SERVER=ws://host:8080/ws/agent TOKEN=rgta_xxx ID=$(hostname) \
     CAPS=COMMAND,SCRIPT,HTTP USER=$USER ./deploy/install-linux.sh
journalctl -u regente-agent -f     # logs
```

### Windows (Tarefa Agendada — roda no boot, reinicia sozinho)

PowerShell **como Administrador**:

```powershell
.\deploy\install-windows.ps1 -Server ws://host:8080/ws/agent -Token rgta_xxx `
                             -Id $env:COMPUTERNAME -Caps COMMAND,SCRIPT,HTTP
Get-ScheduledTask RegenteAgent     # status
```

## Dispatch: como o server escolhe o agente

- Job com **agente específico** (campo "Agente (onde roda)" no JobConfigDrawer) →
  vai direto pra ele.
- Senão → o server escolhe um agente online cuja **capability** bate com o jobType
  (`PickAgent`). Por isso `-caps` deve incluir os jobTypes que o agente aceita.

## Protocolo WebSocket

```jsonc
// server → agent
{ "event":"dispatch", "instanceId":"...", "jobType":"COMMAND", "params":{...}, "timeout":300 }
// agent → server (streaming durante a execução)
{ "event":"output", "instanceId":"...", "chunk":"linha de stdout/stderr" }
// agent → server (final)
{ "event":"result", "instanceId":"...", "exitCode":0, "output":"saída completa" }
// agent → server (a cada 30s)
{ "event":"heartbeat" }
```
