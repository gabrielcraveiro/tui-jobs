// Package format transforma jobs do monitor em texto curto, igual no "bw ls"
// e na TUI.
package format

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
)

// Clock mostra só a hora para hoje e inclui a data nos outros dias.
// Horário zero vira "-".
func Clock(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	t, now = t.Local(), now.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("Jan 02 15:04")
}

// Duration arredonda para segundos. Duração zero ou negativa vira "-".
func Duration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	d = d.Round(time.Second)
	if d < time.Hour {
		return d.String()
	}
	// Segundos não importam em jobs que rodam há horas: "2h13m" em vez de "2h13m5s".
	return strings.TrimSuffix(d.Truncate(time.Minute).String(), "0s")
}

// Icon devolve o símbolo de um estado. Todos ocupam uma coluna.
func Icon(s monitor.State) string {
	switch s {
	case monitor.OK:
		return "✓"
	case monitor.Failed, monitor.Interrupted:
		return "✗"
	case monitor.Running:
		return "▶"
	case monitor.Restarting:
		return "↻"
	case monitor.Stopped:
		return "■"
	case monitor.Late:
		return "⚠"
	case monitor.Never:
		return "·"
	case monitor.Disabled:
		return "–"
	default:
		return "?"
	}
}

// Label descreve o estado do job: "ok", "exit 2", "running (3 restarts)".
func Label(j monitor.Job) string {
	var label string
	switch j.State {
	case monitor.Failed:
		label = fmt.Sprintf("exit %d", j.ExitCode)
	case monitor.Never:
		label = "never ran"
	case monitor.Unwatched:
		label = "not wrapped"
	default:
		label = string(j.State)
	}
	if j.Restarts > 0 {
		label += fmt.Sprintf(" (%d restarts)", j.Restarts)
	}
	return label
}

// Schedule devolve a agenda do cron ou, fora do cron, a origem do job.
func Schedule(j monitor.Job) string {
	if j.Source == monitor.Cron {
		return j.Schedule
	}
	return string(j.Source)
}

// Bytes resume memória no estilo do htop: "512K", "85M", "1.2G". Zero vira "-".
func Bytes(n int64) string {
	const kib, mib, gib = 1 << 10, 1 << 20, 1 << 30
	switch {
	case n <= 0:
		return "-"
	case n < mib:
		return fmt.Sprintf("%dK", n/kib)
	case n < gib:
		return fmt.Sprintf("%dM", n/mib)
	default:
		return fmt.Sprintf("%.1fG", float64(n)/gib)
	}
}

// Memory descreve a memória do job: o RSS atual se está rodando, senão o
// pico da última execução ("85M peak"). Sem medida vira "-".
func Memory(j monitor.Job) string {
	switch {
	case j.Memory > 0:
		return Bytes(j.Memory)
	case j.PeakMemory > 0:
		return Bytes(j.PeakMemory) + " peak"
	default:
		return "-"
	}
}

// Truncate corta s em width colunas, terminando com "…".
func Truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}
