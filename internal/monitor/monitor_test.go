package monitor_test

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/gabrielcraveiro/tui-jobs/internal/crontab"
	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
	"github.com/gabrielcraveiro/tui-jobs/internal/runs"
	"github.com/gabrielcraveiro/tui-jobs/internal/systemd"
)

// fakeHistory guarda as execuções de cada job da mais recente para a mais antiga.
type fakeHistory map[string][]runs.Run

func (f fakeHistory) Jobs() ([]string, error) {
	return slices.Sorted(maps.Keys(f)), nil
}

func (f fakeHistory) List(job string, limit int) ([]runs.Run, error) {
	return f[job][:min(limit, len(f[job]))], nil
}

const alivePID = 100

func alive(r runs.Run) bool { return r.PID == alivePID }

var now = time.Date(2026, 10, 15, 10, 3, 0, 0, time.UTC)

func at(hour, minute, second int) time.Time {
	return time.Date(2026, 10, 15, hour, minute, second, 0, time.UTC)
}

func finished(start time.Time, took time.Duration, exitCode int) runs.Run {
	end := start.Add(took)
	return runs.Run{Start: start, End: &end, ExitCode: exitCode}
}

func TestBuild(t *testing.T) {
	entries := crontab.Parse(`
*/15 * * * * bw run scrap -- ./run.sh
0 * * * * bw run watch -- ./watch.py
*/10 * * * * bw run prices -- ./prices.sh
0 6 * * * /opt/nuvio/run_daily.sh >> /tmp/x.log 2>&1
0 7 * * * /opt/daily-mix/run.sh # daily mix
#OFF 0 20 * * 0 bw run reconcile -- ./reconcile.py
@reboot bw run addon -- ./addon.sh
`)
	units := []systemd.Unit{
		{Name: "ecovias-bot", ActiveState: "active", SubState: "running", Since: at(10, 0, 0)},
		{Name: "flaky", ActiveState: "activating", SubState: "auto-restart", Restarts: 4},
	}
	history := fakeHistory{
		// Rodou 09:45; a das 10:00 não aconteceu e já passou a folga.
		"scrap":    {finished(at(9, 45, 1), 37*time.Second, 0)},
		"watch":    {finished(at(10, 0, 2), 4*time.Second, 0)},
		"prices":   {finished(at(10, 0, 1), time.Second, 1)},
		"transito": {{Start: at(8, 0, 0), PID: alivePID}},
		"zombie":   {{Start: at(7, 0, 0), PID: 999}},
	}

	jobs, err := monitor.Build(monitor.Sources{
		Crontab: entries,
		Units:   units,
		History: history,
		Alive:   alive,
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	want := []struct {
		name   string
		source monitor.Source
		state  monitor.State
	}{
		{"scrap", monitor.Cron, monitor.Late},
		{"watch", monitor.Cron, monitor.OK},
		{"prices", monitor.Cron, monitor.Failed},
		{"nuvio/run_daily.sh", monitor.Cron, monitor.Unwatched},
		{"daily mix", monitor.Cron, monitor.Unwatched},
		{"reconcile", monitor.Cron, monitor.Disabled},
		{"addon", monitor.Cron, monitor.Never},
		{"ecovias-bot", monitor.Systemd, monitor.Running},
		{"flaky", monitor.Systemd, monitor.Restarting},
		{"transito", monitor.Manual, monitor.Running},
		{"zombie", monitor.Manual, monitor.Interrupted},
	}
	if len(jobs) != len(want) {
		for _, j := range jobs {
			t.Logf("got %s %s %s", j.Name, j.Source, j.State)
		}
		t.Fatalf("Build returned %d jobs; want %d", len(jobs), len(want))
	}
	for i, w := range want {
		if j := jobs[i]; j.Name != w.name || j.Source != w.source || j.State != w.state {
			t.Errorf("job %d = %s/%s/%s; want %s/%s/%s", i, j.Name, j.Source, j.State, w.name, w.source, w.state)
		}
	}

	scrap := jobs[0]
	if !scrap.Next.Equal(at(10, 15, 0)) || scrap.Duration != 37*time.Second {
		t.Errorf("scrap Next=%v Duration=%v; want 10:15 and 37s", scrap.Next, scrap.Duration)
	}
	if prices := jobs[2]; prices.ExitCode != 1 {
		t.Errorf("prices ExitCode = %d; want 1", prices.ExitCode)
	}
	if bot := jobs[7]; bot.Duration != 3*time.Minute {
		t.Errorf("ecovias-bot Duration = %v; want 3m (running since 10:00)", bot.Duration)
	}
	if transito := jobs[9]; transito.Duration != 2*time.Hour+3*time.Minute {
		t.Errorf("transito Duration = %v; want time since start", transito.Duration)
	}
}

// fakeMemory devolve RSS fixo por pid e por cgroup.
type fakeMemory struct {
	byPID    map[int]int64
	byCgroup map[string]int64
}

func (f fakeMemory) Descendants(pid int) int64 { return f.byPID[pid] }

func (f fakeMemory) Cgroup(path string) int64 { return f.byCgroup[path] }

func TestBuildMemory(t *testing.T) {
	entries := crontab.Parse("0 * * * * bw run watch -- ./watch.py\n")
	watch := finished(at(10, 0, 2), 4*time.Second, 0)
	watch.PeakRSS = 90 << 20
	history := fakeHistory{
		"watch":    {watch},
		"transito": {{Start: at(8, 0, 0), PID: alivePID}},
	}
	units := []systemd.Unit{
		{Name: "ecovias-bot", ActiveState: "active", Since: at(10, 0, 0), Cgroup: "/app.slice/ecovias-bot.service"},
		{Name: "stopped-bot", ActiveState: "inactive", Cgroup: "/app.slice/stopped-bot.service"},
	}
	memory := fakeMemory{
		byPID:    map[int]int64{alivePID: 50 << 20},
		byCgroup: map[string]int64{"/app.slice/ecovias-bot.service": 40 << 20, "/app.slice/stopped-bot.service": 1},
	}

	jobs, err := monitor.Build(monitor.Sources{
		Crontab: entries, Units: units, History: history, Alive: alive, Memory: memory,
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]monitor.Job{}
	for _, j := range jobs {
		byName[j.Name] = j
	}
	tests := []struct {
		name         string
		memory, peak int64
	}{
		{"watch", 0, 90 << 20},       // terminou: só o pico
		{"transito", 50 << 20, 0},    // bw run rodando: filhos do bw
		{"ecovias-bot", 40 << 20, 0}, // serviço rodando: processos do cgroup
		{"stopped-bot", 0, 0},        // parado: sem medida
	}
	for _, tt := range tests {
		j := byName[tt.name]
		if j.Memory != tt.memory || j.PeakMemory != tt.peak {
			t.Errorf("%s: Memory=%d PeakMemory=%d; want %d and %d", tt.name, j.Memory, j.PeakMemory, tt.memory, tt.peak)
		}
	}
}

func TestBuildNotLateWithinGrace(t *testing.T) {
	entries := crontab.Parse("*/15 * * * * bw run scrap -- ./run.sh\n")
	// Rodou 09:45; a das 10:00 ainda está dentro da folga às 10:01.
	history := fakeHistory{"scrap": {finished(at(9, 45, 1), time.Second, 0)}}

	jobs, err := monitor.Build(monitor.Sources{Crontab: entries, History: history, Alive: alive}, at(10, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].State != monitor.OK {
		t.Errorf("state = %s; want ok while inside the grace period", jobs[0].State)
	}
}
