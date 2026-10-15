// Package tui é a tela interativa do bw: lista de jobs de um lado e, do
// outro, detalhes e log do job selecionado, atualizando sozinha.
package tui

import (
	"slices"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
)

// Backend é o que a tela precisa do resto do bw. As chamadas rodam fora da
// goroutine da tela, então precisam aceitar uso concorrente.
type Backend interface {
	// Jobs devolve a lista atual. Erro junto com lista não vazia é aviso
	// (uma fonte fora do ar); erro sem lista é falha.
	Jobs(now time.Time) ([]monitor.Job, error)
	Log(job monitor.Job) (string, error)
	RunNow(job monitor.Job) error
}

// refreshEvery é curto o bastante para ver um job começar e longo o bastante
// para não pesar: cada rodada chama crontab e systemctl.
const refreshEvery = 3 * time.Second

// Run abre a tela e só retorna quando o usuário sai.
func Run(b Backend) error {
	_, err := tea.NewProgram(newModel(b)).Run()
	return err
}

type pane int

const (
	listPane pane = iota
	logPane
)

type (
	tickMsg time.Time
	jobsMsg struct {
		jobs []monitor.Job
		err  error
		at   time.Time
	}
	logMsg struct {
		job  string
		text string
		err  error
	}
	runMsg struct {
		job string
		err error
	}
)

type model struct {
	backend Backend
	colors  palette

	jobs    []monitor.Job
	loadErr error
	now     time.Time

	cursor int
	focus  pane
	log    viewport.Model
	logJob string // job cujo log está no viewport

	confirming bool   // esperando "y" para rodar o job selecionado
	notice     string // resultado da última ação; some na próxima tecla

	width, height int
}

func newModel(b Backend) model {
	log := viewport.New()
	log.SoftWrap = true
	return model{backend: b, colors: newPalette(true), log: log}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.loadJobs(), tick(), tea.RequestBackgroundColor)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeLog()
		return m, nil
	case tea.BackgroundColorMsg:
		m.colors = newPalette(msg.IsDark())
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.loadJobs(), tick())
	case jobsMsg:
		return m.setJobs(msg)
	case logMsg:
		m.setLog(msg)
		return m, nil
	case runMsg:
		if msg.err != nil {
			m.notice = "✗ " + msg.err.Error()
		} else {
			m.notice = "started " + msg.job
		}
		return m, m.loadJobs()
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.confirming {
		m.confirming = false
		job, ok := m.selected()
		if key != "y" || !ok {
			m.notice = "cancelled"
			return m, nil
		}
		m.notice = "starting " + job.Name + "…"
		return m, m.runNow(job)
	}

	m.notice = ""
	switch key {
	case "q":
		return m, tea.Quit
	case "tab":
		m.focus = 1 - m.focus
		return m, nil
	case "esc":
		m.focus = listPane
		return m, nil
	case "r":
		return m.askRun(), nil
	}

	if m.focus == logPane {
		var cmd tea.Cmd
		m.log, cmd = m.log.Update(msg)
		return m, cmd
	}
	switch key {
	case "up", "k":
		return m.moveCursor(-1)
	case "down", "j":
		return m.moveCursor(1)
	case "home":
		return m.moveCursor(-len(m.jobs))
	case "end":
		return m.moveCursor(len(m.jobs))
	case "enter":
		m.focus = logPane
	}
	return m, nil
}

func (m model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if len(m.jobs) == 0 {
		return m, nil
	}
	next := min(max(m.cursor+delta, 0), len(m.jobs)-1)
	if next == m.cursor {
		return m, nil
	}
	m.cursor = next
	m.log.SetContent("")
	return m, m.loadLog()
}

func (m model) askRun() model {
	job, ok := m.selected()
	if !ok {
		return m
	}
	if reason := whyNotRunnable(job); reason != "" {
		m.notice = reason
		return m
	}
	m.confirming = true
	return m
}

// whyNotRunnable explica por que "rodar agora" não vale para o job; vazio
// quando vale.
func whyNotRunnable(j monitor.Job) string {
	switch {
	case j.Source == monitor.Systemd:
		return ""
	case j.Source == monitor.Manual:
		return "manual job: start it again where it runs"
	case j.State == monitor.Unwatched:
		return "not wrapped: add bw run to this crontab line first"
	case j.State == monitor.Disabled:
		return "disabled in the crontab"
	}
	return ""
}

// setJobs mantém selecionado o mesmo job, mesmo que a ordem mude.
func (m model) setJobs(msg jobsMsg) (tea.Model, tea.Cmd) {
	m.loadErr, m.now = msg.err, msg.at
	if msg.jobs == nil && msg.err != nil {
		return m, nil // falha: continua mostrando a última lista boa
	}
	selected, _ := m.selected()
	m.jobs = sortForScreen(msg.jobs)
	m.cursor = min(m.cursor, max(len(m.jobs)-1, 0))
	for i, j := range m.jobs {
		if j.Name == selected.Name {
			m.cursor = i
			break
		}
	}
	m.resizeLog() // empilhado, a altura da lista depende do número de jobs
	return m, m.loadLog()
}

// sortForScreen põe no fim o que o bw não acompanha (linha sem bw run ou
// desligada): não muda de estado e empurraria os jobs vivos para fora da
// tela. O resto mantém a ordem do crontab.
func sortForScreen(jobs []monitor.Job) []monitor.Job {
	passive := func(j monitor.Job) bool {
		return j.State == monitor.Unwatched || j.State == monitor.Disabled
	}
	sorted := slices.Clone(jobs)
	slices.SortStableFunc(sorted, func(a, b monitor.Job) int {
		switch {
		case passive(a) == passive(b):
			return 0
		case passive(a):
			return 1
		default:
			return -1
		}
	})
	return sorted
}

// tabSpaces troca o tab, que o terminal desenha com largura variável e o
// layout mede como uma coluna.
var tabSpaces = strings.NewReplacer("\t", "    ")

// cleanLog deixa no log só texto imprimível. O log vem de bots que gravam
// dado de fora (mensagem do Telegram, página raspada): uma sequência de
// escape ali mexeria no terminal de quem olha, por exemplo trocando o título
// ou escrevendo no clipboard (OSC 52). O Strip remove a sequência inteira,
// para o conteúdo dela não vazar como texto; o que sobra de controle (ESC
// solto, BEL, \r de barra de progresso, C1) é descartado.
func cleanLog(text string) string {
	text = tabSpaces.Replace(ansi.Strip(text))
	return strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}

func (m *model) setLog(msg logMsg) {
	job, ok := m.selected()
	if !ok || job.Name != msg.job {
		return // resposta atrasada de um job que não está mais selecionado
	}
	text := msg.text
	if msg.err != nil {
		text = "cannot read log: " + msg.err.Error()
	}
	// Segue o fim do log, a não ser que o usuário tenha rolado para cima.
	follow := m.logJob != msg.job || m.log.AtBottom()
	m.log.SetContent(cleanLog(strings.TrimRight(text, "\n")))
	if follow {
		m.log.GotoBottom()
	}
	m.logJob = msg.job
}

func (m *model) resizeLog() {
	l := m.layout()
	w, h := innerSize(l.sideW, l.sideH)
	_, logH := sideRows(h)
	m.log.SetWidth(w)
	m.log.SetHeight(logH)
}

func (m model) selected() (monitor.Job, bool) {
	if m.cursor < 0 || m.cursor >= len(m.jobs) {
		return monitor.Job{}, false
	}
	return m.jobs[m.cursor], true
}

func (m model) loadJobs() tea.Cmd {
	b := m.backend
	return func() tea.Msg {
		now := time.Now()
		jobs, err := b.Jobs(now)
		return jobsMsg{jobs: jobs, err: err, at: now}
	}
}

func (m model) loadLog() tea.Cmd {
	job, ok := m.selected()
	if !ok {
		return nil
	}
	b := m.backend
	return func() tea.Msg {
		text, err := b.Log(job)
		return logMsg{job: job.Name, text: text, err: err}
	}
}

func (m model) runNow(job monitor.Job) tea.Cmd {
	b := m.backend
	return func() tea.Msg {
		return runMsg{job: job.Name, err: b.RunNow(job)}
	}
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}
