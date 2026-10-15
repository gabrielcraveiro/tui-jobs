# ADR 0003 — Memória: RSS da árvore de processos

- **Status:** Aceito
- **Data:** 2026-10-15

## Contexto

O pedido é ver quanta RAM cada job gasta. As fontes disponíveis dão números
diferentes para o mesmo processo: para o `ecovias-bot`, o systemd informa
`MemoryCurrent=35M` (cgroup), e o `/proc` informa `VmRSS=50M`. O cgroup conta
páginas de cache e cobra biblioteca compartilhada de quem tocou primeiro; o
RSS é o número que o htop mostra.

Job do cron roda segundos a cada tantos minutos: o RAM "de agora" quase
sempre é zero, e o que interessa é o pico.

## Decisão

- **RSS em todas as fontes**, para os números serem comparáveis entre si e
  com o htop.
- **Job do `bw run` rodando:** soma do RSS dos descendentes do processo do
  `bw` (sem o próprio `bw`).
- **Serviço systemd rodando:** soma do RSS dos processos do cgroup do serviço
  (`ControlGroup` + `cgroup.procs`).
- **Execução que terminou:** pico de RSS que o kernel devolve no `wait4`
  (`ru_maxrss`), gravado no `.json` da execução. Não precisa de amostragem.
- O `/proc` é lido uma vez por atualização e serve a todos os jobs.

## Consequências

- Somar RSS conta duas vezes a memória compartilhada entre processos do mesmo
  job (bibliotecas). Para bots de um ou dois processos a diferença é pequena.
- O pico é o do maior processo da árvore que o job esperou, não a soma da
  árvore no mesmo instante.
- Processo que se desliga do job (double fork, `nohup ... &`) vira filho do
  init e não entra na conta do job.
- O `MemoryCurrent` do systemd fica de fora; quem quiser o número do cgroup
  usa `systemctl --user status`.

## Alternativas consideradas

- **PSS** (`/proc/<pid>/smaps_rollup`): divide a memória compartilhada entre
  os processos, então a soma é exata. Mais lento de ler e um número que pouca
  gente conhece.
- **`memory.current` do cgroup**: inclui cache de arquivo e não existe para
  job do cron, que não tem cgroup próprio.
- **Amostrar o pico enquanto o job roda**: exigiria um processo amostrando; o
  kernel já entrega o pico de graça no fim.
