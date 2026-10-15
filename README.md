# tui-jobs

`bw` (BotWatcher) shows the state of cron jobs, systemd user services and
long-running bots on one screen.

Cron does not record exit codes or output. `bw run` wraps a command and records
each run. `bw` and `bw ls` join the crontab, the systemd user services and the
run history. Design decisions:
[ADR 0001](docs/adr/0001-wrapper-bw-run-com-historico-em-arquivos.md),
[ADR 0002](docs/adr/0002-tui-com-bubble-tea-e-polling.md),
[ADR 0003](docs/adr/0003-memoria-rss-da-arvore-de-processos.md).

## Install

Requires Go 1.26 or later and Linux (`bw` reads `/proc` and uses `crontab` and `systemctl`).

```sh
GOBIN=$HOME/.local/bin go install github.com/gabrielcraveiro/tui-jobs/cmd/bw@latest
```

From a clone:

```sh
GOBIN=$HOME/.local/bin go install ./cmd/bw
```

## Commands

### `bw`

Opens the interactive view. The job list is on the left. The details and the
log of the selected job are on the right. If the terminal is less than 100
columns wide, the list is on top and the log is below.

The view refreshes every 3 seconds. The log follows new lines until you scroll
up. Lines without `bw run` and disabled lines go to the end of the list.

The list shows the memory of the jobs that run now. The header shows the
total. The details show the memory now, or the peak of the last run.

| Key              | Action                                              |
|------------------|-----------------------------------------------------|
| `↑` `↓` / `k` `j`| Select a job.                                       |
| `home` `end`     | Select the first or the last job.                   |
| `tab` / `enter`  | Move the focus to the log.                          |
| `↑` `↓` `pgup` `pgdn` | Scroll the log (focus on the log).             |
| `esc`            | Move the focus back to the list.                    |
| `r`              | Run the job now. `bw` asks for confirmation first.  |
| `q` / `ctrl+c`   | Quit.                                               |

`r` does what the scheduler does:

- Crontab line with `bw run`: `bw` runs the line with `sh` from `HOME`, in a
  new session. The job continues if you close the view.
- systemd service: `bw` restarts the service.
- Manual job or line without `bw run`: `bw` does not run it and shows why.

`r` uses the environment of your terminal, not the environment of cron. A job
can work from the view and fail in cron.

### `bw run <job> -- <command> [args...]`

Runs the command and records the run: start, end, exit code, peak memory and
output.

- `<job>`: letters, digits, `.`, `_` or `-`. The name is also the history directory.
- `--` is mandatory. It separates the flags of the command from `bw`.
- `bw` copies the output of the command to its own stdout and stderr.
- `bw` exits with the exit code of the command. A command killed by signal N
  gives `128+N`. A command that cannot start gives `127`.
- `bw` sends SIGTERM and SIGHUP to the command.
- `bw` records the end of the run when the command exits. If the command leaves
  a background process that holds the output, `bw` copies that output to the
  log until the process closes it, as cron does.
- If `bw` cannot write the history, it prints a warning and runs the command.

Example:

```sh
bw run transito -- python bot.py
```

### `bw ls`

Lists all jobs. The sources are, in this order:

1. Lines of `crontab -l`. A line without `bw run` shows as `not wrapped`.
   A commented line that uses `bw run` shows as `disabled`.
2. Services in `~/.config/systemd/user`.
3. Jobs that exist only in the history (started by hand with `bw run`).

| Status        | Meaning                                                             |
|---------------|---------------------------------------------------------------------|
| `ok`          | The last run exited with 0.                                         |
| `exit N`      | The last run exited with N. A systemd service is in `failed`.      |
| `running`     | The run or service is active now.                                   |
| `late`        | The schedule expected a run more than 2 minutes ago. It did not occur. |
| `interrupted` | The run has no end and its process is gone (kill -9, WSL shutdown). |
| `restarting`  | The service crashed. systemd will start it again.                   |
| `stopped`     | The service is not active.                                          |
| `never ran`   | The job has no runs in the history.                                 |

The `MEMORY` column shows RSS, the same value as `htop`:

| Value      | Meaning                                                              |
|------------|----------------------------------------------------------------------|
| `50M`      | The job runs now. Sum of the processes below `bw run`, or of the processes in the cgroup of the systemd service. |
| `89M peak` | The job does not run now. Peak RSS of the last run.                  |
| `-`        | No value.                                                            |

If one source fails (for example, no crontab), `bw ls` prints a warning and
shows the other sources.

### `bw logs <job>`

Prints the output of the last run. If the job has no history and a systemd user
service has the same name, `bw logs` prints the last 200 lines of its journal.

## Wrap the crontab

Use the full path: the PATH of cron does not include `~/.local/bin`.

Before:

```crontab
0 6 * * * /home/me/nuvio/run_daily.sh >> /home/me/.config/nuvio/shuffle.log 2>&1
```

After:

```crontab
0 6 * * * /home/me/.local/bin/bw run nuvio-shuffle -- /home/me/nuvio/run_daily.sh >> /home/me/.config/nuvio/shuffle.log 2>&1
```

For lines with `cd` or `export`, put `bw run` before the last command:

```crontab
*/15 * * * * cd /home/me/bot && /home/me/.local/bin/bw run paper -- python paper.py >> paper.log 2>&1
```

## History

Default directory: `~/.local/state/botwatcher`. Set `BOTWATCHER_DIR` or
`XDG_STATE_HOME` to change it. If `HOME`, `BOTWATCHER_DIR` and `XDG_STATE_HOME`
are all empty, `bw run` runs the command and records nothing, and `bw ls` and
`bw logs` fail.

```
<dir>/<job>/20261015T124501.123456Z-4242.json   # metadata of the run
<dir>/<job>/20261015T124501.123456Z-4242.log    # stdout + stderr
```

`bw` keeps the last 50 runs of each job. Only the owner can read the files:
logs can contain tokens.

## Development

```sh
go test -short ./...   # fast: skips the tests that wait for background processes
go test ./...
go vet ./...
```
