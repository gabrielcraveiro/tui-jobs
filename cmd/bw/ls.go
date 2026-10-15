package main

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/gabrielcraveiro/tui-jobs/internal/format"
	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
)

// nameWidth corta nomes longos: linha sem "bw run" usa o comando como nome.
const nameWidth = 32

func printJobs(w io.Writer, jobs []monitor.Job, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "JOB\tSCHEDULE\tLAST RUN\tDURATION\tMEMORY\tNEXT\tSTATUS")
	for _, j := range jobs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s %s\n",
			format.Truncate(j.Name, nameWidth),
			format.Schedule(j),
			format.Clock(j.Started, now),
			format.Duration(j.Duration),
			format.Memory(j),
			format.Clock(j.Next, now),
			format.Icon(j.State),
			format.Label(j),
		)
	}
	return tw.Flush()
}
