# ADR 0002 — TUI com Bubble Tea v2, atualização por polling

- **Status:** Aceito
- **Data:** 2026-10-15

## Contexto

O `bw ls` mostra uma foto do momento. O pedido é monitorar "leve, mas bonito":
ver a lista de jobs, o estado de cada um e o log do job selecionado sem rodar
comando de novo a cada vez.

## Decisão

- `bw` sem argumentos abre a TUI: lista de jobs de um lado e, do outro,
  detalhes e log do job selecionado. Abaixo de 100 colunas, a lista fica em
  cima e o log embaixo.
- Stack Charm v2: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2` e o
  `viewport` de `charm.land/bubbles/v2`. O binário fica com ~6 MB.
- Atualização por polling a cada 3 segundos: `crontab -l`, `systemctl --user
  show` e leitura do histórico. As chamadas rodam fora da goroutine da tela.
  O log segue o fim, a não ser que o usuário role para cima.
- O fundo do terminal decide a paleta (clara ou escura).
- "Rodar agora" (`r`) sempre pede confirmação e faz o que o agendador faria:
  - linha do crontab com `bw run`: `sh -c <linha>` a partir do `HOME`, numa
    sessão própria, para o job continuar se a TUI fechar;
  - serviço systemd: `systemctl --user restart`;
  - job manual ou linha sem `bw run`: recusado, com a explicação na tela.
- Linhas sem `bw run` e desligadas ficam no fim da lista da TUI: não mudam de
  estado e empurrariam os jobs vivos para fora da tela. O `bw ls` mantém a
  ordem do crontab.

## Consequências

- Continua um binário único, sem daemon nem serviço extra.
- A cada 3 segundos a TUI executa dois processos curtos (`crontab` e
  `systemctl`). O custo é desprezível para uma tela aberta de vez em quando.
- "Rodar agora" usa o ambiente do terminal, não o do cron (o PATH do cron é
  menor). Um job pode funcionar pela TUI e falhar no cron. O `%` do crontab,
  que o cron troca por quebra de linha, não é traduzido.
- Ao testar a TUI apareceu um problema no `bw run`: um processo deixado em
  background pelo job herdava a saída e prendia o estado em "running". O
  `bw run` agora grava o fim quando o processo principal sai e continua
  copiando a saída restante para o log até ela fechar, como o cron faz.
  Fechar a saída antes (`WaitDelay`) mataria esse processo com SIGPIPE.

## Alternativas consideradas

- **tview**: widgets prontos (tabela, árvore), mas visual mais datado e
  menos controle sobre o layout.
- **fsnotify em vez de polling**: mais uma dependência e mais estados para
  tratar (diretório criado depois, arquivo trocado por rename). Com 3
  segundos de intervalo o ganho é pequeno.
- **ito (Lua)**: ver ADR 0001.
