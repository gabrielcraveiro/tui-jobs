// Package monitor cruza crontab, serviços systemd e histórico do bw run e diz
// o estado de cada job.
package monitor

import (
	"cmp"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/gabrielcraveiro/tui-jobs/internal/crontab"
	"github.com/gabrielcraveiro/tui-jobs/internal/runs"
	"github.com/gabrielcraveiro/tui-jobs/internal/systemd"
)

type State string

const (
	OK          State = "ok"
	Failed      State = "failed"
	Running     State = "running"
	Restarting  State = "restarting"  // serviço caiu e o systemd vai subir de novo
	Stopped     State = "stopped"     // serviço parado
	Interrupted State = "interrupted" // o bw run sumiu sem gravar o fim (kill -9, WSL desligado)
	Late        State = "late"        // a agenda previa uma execução que não aconteceu
	Never       State = "never"
	Disabled    State = "disabled"  // linha comentada no crontab
	Unwatched   State = "unwatched" // linha do crontab sem "bw run"
)

type Source string

const (
	Cron    Source = "cron"
	Systemd Source = "systemd"
	Manual  Source = "manual" // rodado com bw run fora do crontab
)

// lateGrace é a folga antes de marcar atraso: o cron dispara no minuto cheio
// e o bw run grava o início alguns segundos depois.
const lateGrace = 2 * time.Minute

// History é o histórico de execuções do bw run.
type History interface {
	Jobs() ([]string, error)
	List(job string, limit int) ([]runs.Run, error)
}

// Sources reúne o que o monitor cruza para montar a lista de jobs.
type Sources struct {
	Crontab []crontab.Entry
	Units   []systemd.Unit
	History History
	Alive   func(runs.Run) bool // diz se o processo de uma execução sem fim ainda existe
}

// Job é uma linha do monitor.
type Job struct {
	Name        string
	Source      Source
	Command     string        // linha do crontab ou comando do bw run; vazio para systemd
	Description string        // descrição do serviço systemd
	Schedule    string        // agenda do crontab; vazio para manual e systemd
	Started     time.Time     // início da última execução; zero se nunca rodou
	Duration    time.Duration // da última execução, ou há quanto tempo roda
	Next        time.Time     // próxima execução agendada; zero se não há
	State       State
	ExitCode    int       // da última execução que terminou
	Restarts    int       // reinícios automáticos feitos pelo systemd
	LastRun     *runs.Run // nil para systemd e para job que nunca rodou
}

// Build monta a lista: primeiro os jobs do crontab, na ordem do arquivo;
// depois os serviços systemd; por fim os que só existem no histórico
// (rodados à mão com bw run).
func Build(src Sources, now time.Time) ([]Job, error) {
	var jobs []Job
	seen := map[string]bool{}

	for _, entry := range src.Crontab {
		if entry.Job != "" && seen[entry.Job] {
			continue
		}
		job, err := cronJob(entry, src, now)
		if err != nil {
			return nil, err
		}
		if entry.Job != "" {
			seen[entry.Job] = true
		}
		jobs = append(jobs, job)
	}

	for _, unit := range src.Units {
		if !seen[unit.Name] {
			seen[unit.Name] = true
			jobs = append(jobs, unitJob(unit, now))
		}
	}

	names, err := src.History.Jobs()
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if seen[name] {
			continue
		}
		job := Job{Name: name, Source: Manual}
		if err := job.loadLastRun(src, nil, now); err != nil {
			return nil, err
		}
		if job.LastRun != nil {
			job.Command = strings.Join(job.LastRun.Command, " ")
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func cronJob(e crontab.Entry, src Sources, now time.Time) (Job, error) {
	job := Job{
		Name:     cmp.Or(e.Job, e.Comment, commandLabel(e.Command)),
		Source:   Cron,
		Command:  e.Command,
		Schedule: e.Spec,
	}
	if e.Schedule != nil && !e.Disabled {
		job.Next = e.Schedule.Next(now)
	}
	switch {
	case e.Job == "":
		job.State = Unwatched
	case e.Disabled:
		job.State = Disabled
	default:
		if err := job.loadLastRun(src, e.Schedule, now); err != nil {
			return Job{}, err
		}
	}
	return job, nil
}

// commandLabel resume uma linha sem "bw run" ao script que ela roda
// ("pokescrap/run.sh"): o caminho completo não cabe na tela e o resto da
// linha (export, python3, redirecionamento) não identifica o job.
func commandLabel(command string) string {
	beforeRedirect, _, _ := strings.Cut(command, ">")
	fields := strings.Fields(beforeRedirect)
	for _, field := range slices.Backward(fields) {
		if strings.Contains(field, "/") {
			dir, file := path.Split(path.Clean(field))
			return path.Join(path.Base(dir), file)
		}
	}
	return command
}

// loadLastRun preenche o job com a última execução do histórico e decide o
// estado. Atraso vem antes de falha: se o cron nem disparou, o problema é
// maior que o último exit code.
func (j *Job) loadLastRun(src Sources, schedule cron.Schedule, now time.Time) error {
	list, err := src.History.List(j.Name, 1)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		j.State = Never
		return nil
	}
	last := &list[0]
	j.LastRun, j.Started, j.Duration, j.ExitCode = last, last.Start, last.Duration(now), last.ExitCode

	switch {
	case last.End == nil && src.Alive(*last):
		j.State = Running
	case last.End == nil:
		j.State = Interrupted
	case schedule != nil && schedule.Next(last.Start).Add(lateGrace).Before(now):
		j.State = Late
	case last.ExitCode != 0:
		j.State = Failed
	default:
		j.State = OK
	}
	return nil
}

func unitJob(u systemd.Unit, now time.Time) Job {
	job := Job{
		Name:        u.Name,
		Source:      Systemd,
		Description: u.Description,
		Started:     u.Since,
		ExitCode:    u.ExitStatus,
		Restarts:    u.Restarts,
	}
	switch {
	case u.SubState == "auto-restart":
		job.State = Restarting
	case u.ActiveState == "failed":
		job.State = Failed
	case u.ActiveState == "active", u.ActiveState == "activating", u.ActiveState == "reloading":
		job.State = Running
	default:
		job.State = Stopped
	}

	switch {
	case u.Since.IsZero():
	case job.State == Running:
		job.Duration = now.Sub(u.Since)
	case u.Until.After(u.Since):
		job.Duration = u.Until.Sub(u.Since)
	}
	return job
}
