package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
)

// nameWidth corta nomes longos: linha sem "bw run" usa o comando inteiro como nome.
const nameWidth = 32

func printJobs(w io.Writer, jobs []monitor.Job, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "JOB\tSCHEDULE\tLAST RUN\tDURATION\tNEXT\tSTATUS")
	for _, j := range jobs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			truncate(j.Name, nameWidth),
			schedule(j),
			clock(j.Started, now),
			duration(j),
			clock(j.Next, now),
			status(j),
		)
	}
	return tw.Flush()
}

func schedule(j monitor.Job) string {
	if j.Source == monitor.Cron {
		return j.Schedule
	}
	return string(j.Source)
}

// clock mostra só a hora para hoje e inclui a data nos outros dias.
func clock(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	t, now = t.Local(), now.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("Jan 02 15:04")
}

func duration(j monitor.Job) string {
	if j.Started.IsZero() || j.Duration <= 0 {
		return "-"
	}
	d := j.Duration.Round(time.Second)
	if d < time.Hour {
		return d.String()
	}
	// Segundos não importam em jobs que rodam há horas: "2h13m" em vez de "2h13m5s".
	return strings.TrimSuffix(d.Truncate(time.Minute).String(), "0s")
}

func status(j monitor.Job) string {
	var label string
	switch j.State {
	case monitor.OK:
		label = "✓ ok"
	case monitor.Failed:
		label = fmt.Sprintf("✗ exit %d", j.ExitCode)
	case monitor.Running:
		label = "▶ running"
	case monitor.Restarting:
		label = "↻ restarting"
	case monitor.Stopped:
		label = "■ stopped"
	case monitor.Interrupted:
		label = "✗ interrupted"
	case monitor.Late:
		label = "⚠ late"
	case monitor.Never:
		label = "· never ran"
	case monitor.Disabled:
		label = "– disabled"
	case monitor.Unwatched:
		label = "? not wrapped"
	}
	if j.Restarts > 0 {
		label += fmt.Sprintf(" (%d restarts)", j.Restarts)
	}
	return label
}

func truncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}
