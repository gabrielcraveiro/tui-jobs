package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
)

type fakeBackend struct {
	jobs []monitor.Job
	logs map[string]string
	ran  []string
}

func (f *fakeBackend) Jobs(time.Time) ([]monitor.Job, error) { return f.jobs, nil }

func (f *fakeBackend) Log(j monitor.Job) (string, error) { return f.logs[j.Name], nil }

func (f *fakeBackend) RunNow(j monitor.Job) error {
	f.ran = append(f.ran, j.Name)
	return nil
}

var now = time.Date(2026, 10, 15, 10, 3, 0, 0, time.Local)

func sampleBackend() *fakeBackend {
	return &fakeBackend{
		jobs: []monitor.Job{
			{Name: "pokescrap", Source: monitor.Cron, Schedule: "*/15 * * * *", Command: "bw run pokescrap -- ./run.sh",
				State: monitor.OK, Started: now.Add(-3 * time.Minute), Duration: 37 * time.Second, PeakMemory: 90 << 20},
			{Name: "ecovias-bot", Source: monitor.Systemd, Description: "Bot do Telegram",
				State: monitor.Running, Started: now.Add(-time.Hour), Duration: time.Hour, Memory: 50 << 20},
			{Name: "nuvio/run_daily.sh", Source: monitor.Cron, Schedule: "0 6 * * *", Command: "/opt/nuvio/run_daily.sh",
				State: monitor.Unwatched},
			{Name: "prices", Source: monitor.Cron, Schedule: "*/10 * * * *", Command: "bw run prices -- ./p.sh",
				State: monitor.Failed, ExitCode: 2, Started: now.Add(-2 * time.Minute)},
		},
		logs: map[string]string{
			"pokescrap":   "scraped 42 cards\n",
			"ecovias-bot": "Application started\n",
		},
	}
}

// start abre a tela num terminal width x height com jobs e log carregados.
func start(t *testing.T, b Backend, width, height int) model {
	t.Helper()
	m := send(t, newModel(b), tea.WindowSizeMsg{Width: width, Height: height})
	jobs, _ := b.Jobs(now)
	return send(t, m, jobsMsg{jobs: jobs, at: now})
}

// send entrega msg e, como o loop do Bubble Tea, entrega de volta o que os
// comandos de carga produzem. Tick e quit ficam de fora: não voltam ao modelo.
func send(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(model)
	if cmd == nil {
		return m
	}
	switch out := cmd().(type) {
	case jobsMsg, logMsg, runMsg:
		return send(t, m, out)
	}
	return m
}

func key(name string) tea.KeyPressMsg {
	switch name {
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	}
	return tea.KeyPressMsg{Code: []rune(name)[0], Text: name}
}

func screen(m model) string {
	return m.View().Content
}

func TestShowsJobsSummaryAndSelectedLog(t *testing.T) {
	out := screen(start(t, sampleBackend(), 120, 30))

	for _, want := range []string{"4 jobs", "▶ 1 running", "✗ 1 failing", "pokescrap", "ecovias-bot", "prices", "✓ ok", "scraped 42 cards", "50M in use", "peak 90M RAM"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen must contain %q", want)
		}
	}
}

func TestMovingSelectionShowsThatJobsLog(t *testing.T) {
	m := start(t, sampleBackend(), 120, 30)

	out := screen(send(t, m, key("down")))

	if !strings.Contains(out, "Application started") || strings.Contains(out, "scraped 42 cards") {
		t.Error("after moving down the log must be the one of ecovias-bot only")
	}
	if !strings.Contains(out, "systemd service · Bot do Telegram") {
		t.Error("details must describe the systemd service")
	}
	if !strings.Contains(out, "50M RAM now") {
		t.Error("details must show the memory of the running service")
	}
}

func TestSelectionFollowsJobWhenOrderChanges(t *testing.T) {
	b := sampleBackend()
	m := send(t, start(t, b, 120, 30), key("down"))
	reversed := slices.Clone(b.jobs)
	slices.Reverse(reversed)

	m = send(t, m, jobsMsg{jobs: reversed, at: now})

	if job, _ := m.selected(); job.Name != "ecovias-bot" {
		t.Errorf("selected = %q after refresh; want ecovias-bot", job.Name)
	}
}

func TestRunNowAsksBeforeRunning(t *testing.T) {
	b := sampleBackend()
	m := send(t, start(t, b, 120, 30), key("r"))

	if !strings.Contains(screen(m), "run pokescrap now?") {
		t.Fatal("r must ask for confirmation")
	}
	if len(b.ran) != 0 {
		t.Fatal("r must not run the job before confirmation")
	}

	m = send(t, m, key("y"))

	if !slices.Equal(b.ran, []string{"pokescrap"}) {
		t.Errorf("ran = %v; want [pokescrap]", b.ran)
	}
	if !strings.Contains(screen(m), "started pokescrap") {
		t.Error("screen must confirm that the job started")
	}
}

func TestRunNowCancelled(t *testing.T) {
	b := sampleBackend()
	m := send(t, start(t, b, 120, 30), key("r"))

	m = send(t, m, key("n"))

	if len(b.ran) != 0 || !strings.Contains(screen(m), "cancelled") {
		t.Errorf("any key other than y must cancel; ran = %v", b.ran)
	}
}

func TestRunNowRestartsService(t *testing.T) {
	b := sampleBackend()
	m := send(t, send(t, start(t, b, 120, 30), key("down")), key("r"))

	if !strings.Contains(screen(m), "restart ecovias-bot?") {
		t.Fatal("for a systemd service, r must ask to restart it")
	}
	send(t, m, key("y"))
	if !slices.Equal(b.ran, []string{"ecovias-bot"}) {
		t.Errorf("ran = %v; want [ecovias-bot]", b.ran)
	}
}

func TestUnwatchedJobsGoToTheEnd(t *testing.T) {
	m := start(t, sampleBackend(), 120, 30)

	var names []string
	for _, j := range m.jobs {
		names = append(names, j.Name)
	}

	want := []string{"pokescrap", "ecovias-bot", "prices", "nuvio/run_daily.sh"}
	if !slices.Equal(names, want) {
		t.Errorf("order = %v; want %v", names, want)
	}
}

func TestRunNowRefusesUnwrappedLine(t *testing.T) {
	b := sampleBackend()
	m := send(t, start(t, b, 120, 30), key("end"))

	m = send(t, m, key("r"))

	if m.confirming || len(b.ran) != 0 {
		t.Error("a crontab line without bw run must not run from the TUI")
	}
	if !strings.Contains(screen(m), "not wrapped") {
		t.Error("screen must explain why the job cannot run")
	}
}

// O log pode trazer texto de terceiros (mensagem de Telegram, página
// raspada). Nada nele pode virar comando para o terminal.
func TestLogCannotControlTheTerminal(t *testing.T) {
	hostile := "hello \x1b]52;c;ZXZpbA==\x07world\x1b]0;pwned\x07 \x1b[2J\x1b[31mred\x1b[0m" +
		" \u009b2J\u009d0;c1\a \x1b lone-esc\b\r\tdone\n"
	b := sampleBackend()
	b.logs["pokescrap"] = hostile

	out := screen(start(t, b, 120, 30))

	for _, word := range []string{"hello", "world", "red", "done"} {
		if !strings.Contains(out, word) {
			t.Errorf("screen must keep the text %q", word)
		}
	}
	for _, seq := range []string{"\x1b]", "\x1b[2J", "\a", "\b", "\r", "\u009b", "\u009d", "ZXZpbA", "pwned"} {
		if strings.Contains(out, seq) {
			t.Errorf("screen must not contain %q", seq)
		}
	}
	for _, r := range cleanLog(hostile) {
		if r != '\n' && unicode.IsControl(r) {
			t.Errorf("cleanLog kept control character %U", r)
		}
	}
}

func TestQuit(t *testing.T) {
	m := start(t, sampleBackend(), 120, 30)
	for _, k := range []tea.KeyPressMsg{key("q"), {Code: 'c', Mod: tea.ModCtrl}} {
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%s: no command; want quit", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s: command does not quit", k)
		}
	}
}

// A tela precisa ocupar exatamente o terminal: uma linha a mais quebra as
// bordas, e uma linha larga demais faz o terminal quebrar a linha.
func TestLayoutFitsTerminal(t *testing.T) {
	busy := sampleBackend()
	for i := range 30 {
		busy.jobs = append(busy.jobs, monitor.Job{
			Name: fmt.Sprintf("job-with-a-very-long-name-%02d", i), Source: monitor.Cron,
			Schedule: "0 1,8,12,16,20,22 * * *", Command: strings.Repeat("x", 300), State: monitor.Late,
		})
	}
	busy.logs["pokescrap"] = strings.Repeat("a long line\twith tabs and \r carriage returns ", 20) + "\n" +
		strings.Repeat("line\n", 500)

	sizes := [][2]int{{200, 50}, {120, 30}, {100, 20}, {99, 30}, {80, 24}, {60, 12}, {50, 12}}
	for _, b := range []*fakeBackend{sampleBackend(), busy} {
		for _, size := range sizes {
			width, height := size[0], size[1]
			m := start(t, b, width, height)
			for _, state := range []model{m, send(t, m, key("tab")), send(t, m, key("r"))} {
				lines := strings.Split(screen(state), "\n")
				if len(lines) != height {
					t.Errorf("%dx%d (%d jobs): screen has %d lines; want %d", width, height, len(b.jobs), len(lines), height)
				}
				for i, line := range lines {
					if w := lipgloss.Width(line); w > width {
						t.Errorf("%dx%d (%d jobs): line %d has width %d", width, height, len(b.jobs), i, w)
					}
				}
			}
		}
	}
}

func TestTooSmallTerminal(t *testing.T) {
	if out := screen(start(t, sampleBackend(), 40, 10)); !strings.Contains(out, "terminal too small") {
		t.Errorf("screen = %q; want a message about the size", out)
	}
}
