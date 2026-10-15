// Package crontab lê o crontab do usuário e acha as linhas que usam "bw run".
package crontab

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/robfig/cron/v3"

	"github.com/gabrielcraveiro/tui-jobs/internal/runs"
)

// Entry é uma linha de agendamento do crontab.
type Entry struct {
	Spec     string        // agenda como está na linha: "*/15 * * * *", "@daily", "@reboot"
	Schedule cron.Schedule // nil para @reboot, que não tem próxima execução
	Command  string
	Comment  string // texto depois de " #" no fim da linha
	Job      string // nome passado ao "bw run"; vazio quando a linha não usa o wrapper
	Disabled bool   // linha comentada que usa "bw run"
}

// O bw pode aparecer com caminho completo (/home/x/.local/bin/bw) ou depois
// de "&&", ";" e afins.
var jobPattern = regexp.MustCompile(`(?:^|[\s/;&|(])bw\s+run\s+(` + runs.NamePattern + `)`)

// Load lê o crontab do usuário atual. Usuário sem crontab devolve lista vazia.
func Load() ([]Entry, error) {
	out, err := exec.Command("crontab", "-l").Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && bytes.Contains(exitErr.Stderr, []byte("no crontab")) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("crontab -l: %w", err)
	}
	return Parse(string(out)), nil
}

// Parse extrai as entradas de um crontab. Variáveis, comentários e agendas
// inválidas ficam de fora.
func Parse(text string) []Entry {
	var entries []Entry
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		entry, ok := parseLine(line)
		if !ok {
			entry, ok = parseDisabled(line)
		}
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func parseLine(line string) (Entry, bool) {
	n := 5
	if strings.HasPrefix(line, "@") {
		n = 1
	}
	fields, command, ok := cutFields(line, n)
	if !ok {
		return Entry{}, false
	}
	e := Entry{Spec: strings.Join(fields, " "), Command: command}
	if e.Spec != "@reboot" {
		schedule, err := cron.ParseStandard(e.Spec)
		if err != nil {
			return Entry{}, false
		}
		e.Schedule = schedule
	}
	if i := strings.LastIndex(command, " #"); i >= 0 {
		e.Comment = strings.TrimSpace(command[i+2:])
	}
	if m := jobPattern.FindStringSubmatch(command); m != nil {
		e.Job = m[1]
	}
	return e, true
}

// parseDisabled reconhece um job desligado com "#", como
// "#DESLIGADO */15 * * * * bw run x -- ...". Só vale para linha com "bw run":
// sem esse filtro, qualquer comentário com cara de agenda viraria job.
func parseDisabled(line string) (Entry, bool) {
	rest, commented := strings.CutPrefix(line, "#")
	if !commented || !jobPattern.MatchString(rest) {
		return Entry{}, false
	}
	rest = strings.TrimSpace(strings.TrimLeft(rest, "#"))
	e, ok := parseLine(rest)
	if !ok {
		// Aceita um rótulo antes da agenda, como o "DESLIGADO" acima.
		_, afterLabel, _ := strings.Cut(rest, " ")
		e, ok = parseLine(strings.TrimSpace(afterLabel))
	}
	e.Disabled = ok
	return e, ok
}

// cutFields separa os n primeiros campos da linha e devolve o resto intacto:
// o comando pode ter espaços e aspas que não podem ser normalizados.
func cutFields(line string, n int) (fields []string, rest string, ok bool) {
	rest = line
	for range n {
		rest = strings.TrimLeft(rest, " \t")
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			return nil, "", false
		}
		fields = append(fields, rest[:end])
		rest = rest[end:]
	}
	rest = strings.TrimSpace(rest)
	return fields, rest, rest != ""
}
