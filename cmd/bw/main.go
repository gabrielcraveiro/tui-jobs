// Comando bw: monitora jobs do cron, serviços systemd do usuário e bots
// rodados à mão com "bw run".
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gabrielcraveiro/tui-jobs/internal/crontab"
	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
	"github.com/gabrielcraveiro/tui-jobs/internal/procs"
	"github.com/gabrielcraveiro/tui-jobs/internal/runner"
	"github.com/gabrielcraveiro/tui-jobs/internal/runs"
	"github.com/gabrielcraveiro/tui-jobs/internal/systemd"
	"github.com/gabrielcraveiro/tui-jobs/internal/tui"
)

const usage = `bw watches cron jobs, systemd user services and long-running bots.

Usage:
  bw
      Open the interactive view: job list, details and live log.
  bw run <job> -- <command> [args...]
      Run the command and record the run: start, end, exit code and output.
      bw exits with the exit code of the command.
  bw ls
      List all jobs and their state.
  bw logs <job>
      Print the output of the last run, or the journal of a systemd service.

Environment:
  BOTWATCHER_DIR  Directory for the run history.
                  Default: $XDG_STATE_HOME/botwatcher, else ~/.local/state/botwatcher.
`

// logLines é quanto do journal o "bw logs" mostra para serviços systemd.
const logLines = 200

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		store, ok := openStore(stderr)
		if !ok {
			return 1
		}
		if err := tui.Run(backend{store: store}); err != nil {
			fmt.Fprintf(stderr, "bw: %v\n", err)
			return 1
		}
		return 0
	}
	switch args[0] {
	case "run":
		// Sem diretório de histórico o job ainda roda, só não fica registrado.
		store, _ := openStore(stderr)
		return cmdRun(args[1:], store, stdout, stderr)
	case "ls":
		store, ok := openStore(stderr)
		if !ok {
			return 1
		}
		return cmdLs(store, stdout, stderr)
	case "logs":
		store, ok := openStore(stderr)
		if !ok {
			return 1
		}
		return cmdLogs(args[1:], store, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "bw: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// openStore abre o histórico ou explica no stderr por que não deu.
func openStore(stderr io.Writer) (*runs.Store, bool) {
	dir, err := runs.DefaultDir()
	if err != nil {
		fmt.Fprintf(stderr, "bw: %v\n", err)
		return nil, false
	}
	return runs.NewStore(dir), true
}

// cmdRun exige o "--" para que flags do comando nunca sejam lidas como flags
// do bw.
func cmdRun(args []string, store *runs.Store, stdout, stderr io.Writer) int {
	if len(args) < 3 || args[1] != "--" {
		fmt.Fprintln(stderr, "bw: usage: bw run <job> -- <command> [args...]")
		return 2
	}
	return runner.Exec(store, args[0], args[2:], stdout, stderr)
}

func cmdLs(store *runs.Store, stdout, stderr io.Writer) int {
	now := time.Now()
	jobs, warning, err := loadJobs(store, now)
	if warning != nil {
		fmt.Fprintf(stderr, "bw: %v\n", warning)
	}
	if err != nil {
		fmt.Fprintf(stderr, "bw: %v\n", err)
		return 1
	}
	if err := printJobs(stdout, jobs, now); err != nil {
		fmt.Fprintf(stderr, "bw: %v\n", err)
		return 1
	}
	return 0
}

// cmdLogs prefere o histórico do bw run; sem histórico, tenta o journal de um
// serviço systemd com o mesmo nome.
func cmdLogs(args []string, store *runs.Store, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "bw: usage: bw logs <job>")
		return 2
	}
	job := args[0]

	list, err := store.List(job, 1)
	if err != nil {
		fmt.Fprintf(stderr, "bw: %v\n", err)
		return 1
	}
	if len(list) > 0 {
		if err := copyFile(stdout, store.LogPath(list[0])); err != nil {
			fmt.Fprintf(stderr, "bw: %v\n", err)
			return 1
		}
		return 0
	}

	units, err := systemd.Load()
	if err != nil {
		fmt.Fprintf(stderr, "bw: %v\n", err)
	}
	for _, unit := range units {
		if unit.Name == job {
			if err := systemd.WriteLogs(stdout, job, logLines); err != nil {
				fmt.Fprintf(stderr, "bw: journalctl: %v\n", err)
				return 1
			}
			return 0
		}
	}
	fmt.Fprintf(stderr, "bw: no runs for job %q\n", job)
	return 1
}

// loadJobs junta todas as fontes. Uma fonte fora do ar (sem crontab, sem
// systemd de usuário) volta como warning, e o resto da lista continua útil.
// Quem chama decide onde mostrar o aviso: com a TUI aberta, escrever no
// stderr estragaria a tela.
func loadJobs(store *runs.Store, now time.Time) (jobs []monitor.Job, warning, err error) {
	var warnings []error
	entries, err := crontab.Load()
	if err != nil {
		warnings = append(warnings, err)
	}
	units, err := systemd.Load()
	if err != nil {
		warnings = append(warnings, err)
	}
	jobs, err = monitor.Build(monitor.Sources{
		Crontab: entries,
		Units:   units,
		History: store,
		Alive:   runs.Alive,
		Memory:  procs.Read(),
	}, now)
	return jobs, errors.Join(warnings...), err
}

func copyFile(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}
