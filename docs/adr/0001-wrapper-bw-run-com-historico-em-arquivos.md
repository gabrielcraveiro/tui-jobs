# ADR 0001 — Wrapper `bw run` com histórico em arquivos

- **Status:** Aceito
- **Data:** 2026-10-15

## Contexto

Os bots e jobs rodam no WSL de três formas:

1. **cron** (scrap de pokémon, atualização de preço, nuvio, daily-mix...). O cron
   só registra que disparou o comando (`journalctl -u cron` mostra `CMD (...)`).
   Ele não guarda exit code, duração nem saída. Os logs ficam espalhados
   (`/tmp`, `~/.config/...`) e alguns jobs nem redirecionam a saída.
2. **serviço systemd de usuário** (bot de trânsito `ecovias-bot`). O systemd já
   guarda estado, reinícios e logs no journal.
3. **processo solto** iniciado à mão ou por script (`mypscan.bot`).

Sem exit code e sem histórico, um monitor só consegue dizer "agendado", nunca
"falhou" ou "não rodou". Além disso, quando o Windows hiberna ou o WSL desliga,
o cron simplesmente não dispara e nada avisa.

## Decisão

- Jobs do cron e processos manuais rodam através de `bw run <job> -- <comando>`.
  O wrapper grava início, fim, exit code e saída, espelha a saída no
  stdout/stderr (os `>> arquivo.log` existentes continuam funcionando) e sai
  com o exit code do comando.
- O histórico fica em `~/.local/state/botwatcher/<job>/`: um `.json` de
  metadados e um `.log` de saída por execução, mantendo as 50 mais recentes.
- Serviços em `~/.config/systemd/user` são lidos direto do systemd
  (`systemctl --user show` e `journalctl --user`), sem wrapper.
- `bw ls` (e a TUI, na próxima entrega) cruza `crontab -l`, systemd e histórico.
- Go, binário único. Única dependência: `robfig/cron/v3`, para interpretar a
  agenda (próxima execução e detecção de atraso). Escrever um parser de cron
  com ranges, passos, listas e a regra "dia do mês OU dia da semana" não vale
  o risco.
- Se o histórico não puder ser gravado, o `bw run` avisa e roda o comando
  mesmo assim: o job importa mais que o monitoramento.

## Consequências

- Toda execução tem exit code, duração e saída, inclusive a de bots manuais.
- O estado "atrasado" aparece quando a agenda previa uma execução que não
  aconteceu (folga de 2 minutos).
- Uma execução sem fim cujo processo sumiu aparece como "interrompida"
  (kill -9, WSL desligado). A checagem usa boot ID + PID. Reaproveitamento de
  PID dentro do mesmo boot pode dar um falso "rodando"; é raro o suficiente
  para aceitar.
- Sem banco: `ls` e `cat` bastam para inspecionar o histórico.
- Cada linha do crontab precisa ser editada para incluir `bw run`. Linhas sem
  wrapper aparecem como "not wrapped".
- O `.log` de um bot de longa duração cresce sem limite. Rotação fica para
  depois, se virar problema.

## Alternativas consideradas

- **SQLite** (`modernc.org/sqlite`): consultas mais ricas, mas é uma
  dependência pesada e os logs iriam para arquivo de qualquer forma. Para ~10
  jobs com 50 execuções cada, arquivos bastam.
- **Migrar tudo para systemd timers**: exit code, journal e `Persistent=true`
  (recupera execução perdida) de graça, mas exige reescrever cada job como
  unit file. Continua possível: o `bw` já lê serviços systemd.
- **healthchecks.io self-hosted**: resolve alerta de falha, mas é web e não
  mostra os logs. O pedido é só monitorar pelo terminal, sem notificação.
- **TUI com ito (Lua)**: a página é um playground em WASM, sem licença nem
  versão publicadas. Arriscado para uma ferramenta de monitoramento. A TUI vai
  usar Bubble Tea (Go), que é maduro.
