package main

import (
	"bytes"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
	"github.com/gabrielcraveiro/tui-jobs/internal/runs"
	"github.com/gabrielcraveiro/tui-jobs/internal/systemd"
)

// tailBytes é quanto do fim do log a TUI lê a cada atualização.
const tailBytes = 64 << 10

// backend liga a TUI ao crontab, ao systemd e ao histórico.
type backend struct {
	store *runs.Store
}

func (b backend) Jobs(now time.Time) ([]monitor.Job, error) {
	jobs, warning, err := loadJobs(b.store, now)
	if err != nil {
		return nil, err
	}
	return jobs, warning
}

func (b backend) Log(job monitor.Job) (string, error) {
	switch {
	case job.Source == monitor.Systemd:
		var buf bytes.Buffer
		err := systemd.WriteLogs(&buf, job.Name, logLines)
		return buf.String(), err
	case job.LastRun != nil:
		return b.store.Tail(*job.LastRun, tailBytes)
	case job.State == monitor.Unwatched:
		return "This crontab line does not use bw run, so bw has no history for it.\n\n" +
			"Put bw run before the command to record its runs:\n\n" +
			"  bw run <name> -- <command>\n\n" +
			"Use the full path of bw: the PATH of cron does not include ~/.local/bin.", nil
	case job.State == monitor.Disabled:
		return "This job is commented out in the crontab.", nil
	default:
		return "No runs yet.", nil
	}
}

// RunNow faz o que o cron ou o systemd fariam: roda a linha do crontab com sh
// a partir do HOME, como o cron, ou reinicia o serviço.
func (b backend) RunNow(job monitor.Job) error {
	if job.Source == monitor.Systemd {
		return systemd.Restart(job.Name)
	}
	return startDetached(job.Command)
}

// startDetached solta o processo da TUI (sessão própria, sem terminal): o job
// continua se a TUI fechar, e a saída vai para o histórico pelo bw run da linha.
//
// O sh -c é de propósito: command é a linha do crontab do próprio usuário, a
// mesma que o cron entrega ao sh, com "cd ... &&" e redirecionamentos.
func startDetached(command string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir, _ = os.UserHomeDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // evita processo zumbi enquanto a TUI está aberta
	return nil
}
