package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gabrielcraveiro/tui-jobs/internal/format"
	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
)

const (
	minWidth, minHeight = 50, 12
	// stackBelow é a largura abaixo da qual a lista fica em cima e o log embaixo.
	stackBelow = 100
	// detailLines são as linhas de detalhes acima do log: nome e estado,
	// origem, última execução e comando.
	detailLines = 4
	// whenWidth cabe "10:15", "Oct 14" e "12h14m".
	whenWidth = 6
	// scheduleWidth cabe "*/15 * * * *"; agendas maiores são cortadas.
	scheduleWidth = 13
)

// layout guarda o tamanho externo, com borda, de cada painel.
type layout struct {
	stacked      bool
	listW, listH int
	sideW, sideH int
}

func (m model) layout() layout {
	bodyH := m.height - 2 // cabeçalho e rodapé
	if m.width < stackBelow {
		listH := max(min(len(m.jobs)+2, bodyH/2), 3)
		return layout{stacked: true, listW: m.width, listH: listH, sideW: m.width, sideH: bodyH - listH}
	}
	listW := min(max(m.width*2/5, 44), 64)
	return layout{listW: listW, listH: bodyH, sideW: m.width - listW, sideH: bodyH}
}

// innerSize desconta a borda (1 coluna de cada lado) e o padding horizontal
// (1 de cada lado): no Lip Gloss v2, Width e Height incluem os dois.
func innerSize(w, h int) (int, int) {
	return max(w-4, 1), max(h-2, 1)
}

// sideRows divide a altura do painel lateral entre detalhes, separador e log.
// Em terminal baixo os detalhes encolhem antes do log.
func sideRows(h int) (details, logH int) {
	details = min(detailLines, max(h-2, 0))
	return details, max(h-details-1, 1)
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			"terminal too small for bw\nresize it or use bw ls")
	}
	l := m.layout()
	list := m.pane(m.renderList(innerSize(l.listW, l.listH)), l.listW, l.listH, m.focus == listPane)
	side := m.pane(m.renderSide(innerSize(l.sideW, l.sideH)), l.sideW, l.sideH, m.focus == logPane)
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, side)
	if l.stacked {
		body = lipgloss.JoinVertical(lipgloss.Left, list, side)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderFooter())
}

func (m model) pane(content string, w, h int, focused bool) string {
	border := m.colors.border
	if focused {
		border = m.colors.accent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(w).
		Height(h).
		Render(content)
}

func (m model) renderHeader() string {
	badge := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("#ffffff")).
		Background(m.colors.accent).
		Padding(0, 1).
		Render("bw")
	left := badge + " " + m.renderCounts()
	if m.loadErr != nil {
		msg := strings.ReplaceAll(m.loadErr.Error(), "\n", "; ")
		left += "  " + m.colors.fg(m.colors.warn).Render("⚠ "+msg)
	}
	right := m.colors.fg(m.colors.dim).Render(m.now.Format("15:04:05"))
	left = format.Truncate(left, m.width-lipgloss.Width(right)-1)
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

func (m model) renderCounts() string {
	var running, failing, late int
	for _, j := range m.jobs {
		switch j.State {
		case monitor.Running:
			running++
		case monitor.Failed, monitor.Interrupted, monitor.Restarting:
			failing++
		case monitor.Late:
			late++
		}
	}
	parts := []string{m.colors.fg(m.colors.dim).Render(fmt.Sprintf("%d jobs", len(m.jobs)))}
	add := func(n int, state monitor.State, label string) {
		if n > 0 {
			text := fmt.Sprintf("%s %d %s", format.Icon(state), n, label)
			parts = append(parts, m.colors.fg(m.colors.state[state]).Render(text))
		}
	}
	add(running, monitor.Running, "running")
	add(failing, monitor.Failed, "failing")
	add(late, monitor.Late, "late")
	return strings.Join(parts, "  ")
}

func (m model) renderList(w, h int) string {
	if len(m.jobs) == 0 {
		return m.colors.fg(m.colors.dim).Render(format.Truncate("no jobs found", w))
	}
	// Mantém o cursor perto do meio quando a lista não cabe.
	start := min(max(m.cursor-h/2, 0), max(len(m.jobs)-h, 0))
	end := min(start+h, len(m.jobs))
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, m.renderRow(m.jobs[i], i == m.cursor, w))
	}
	return strings.Join(rows, "\n")
}

// renderRow monta ícone, nome, agenda (se couber) e quando rodou. Cada pedaço
// recebe o fundo da seleção: um fundo aplicado por fora seria apagado pelo
// reset de cor de cada pedaço.
func (m model) renderRow(j monitor.Job, selected bool, w int) string {
	base := lipgloss.NewStyle()
	if selected {
		base = base.Background(m.colors.selectedBg)
	}
	dim := base.Foreground(m.colors.dim)

	nameW := w - 2 - (whenWidth + 1)
	showSchedule := nameW-(scheduleWidth+1) >= 16
	if showSchedule {
		nameW -= scheduleWidth + 1
	}

	row := base.Foreground(m.colors.state[j.State]).Render(format.Icon(j.State)) +
		base.Render(" ") +
		base.Bold(selected).Width(nameW).Render(format.Truncate(j.Name, nameW))
	if showSchedule {
		row += base.Render(" ") + dim.Width(scheduleWidth).Render(format.Truncate(format.Schedule(j), scheduleWidth))
	}
	return row + base.Render(" ") + dim.Width(whenWidth).Render(fmt.Sprintf("%*s", whenWidth, m.when(j)))
}

// when resume, em até 6 colunas, há quanto tempo o job roda ou quando rodou.
func (m model) when(j monitor.Job) string {
	started, now := j.Started.Local(), m.now.Local()
	switch {
	case j.State == monitor.Running:
		return format.Duration(j.Duration)
	case j.Started.IsZero():
		return "-"
	case started.YearDay() == now.YearDay() && started.Year() == now.Year():
		return started.Format("15:04")
	default:
		return started.Format("Jan 02")
	}
}

func (m model) renderSide(w, h int) string {
	job, ok := m.selected()
	if !ok {
		return ""
	}
	details, _ := sideRows(h)
	lines := m.details(job, w)[:details]
	lines = append(lines, m.separator(w), m.log.View())
	return strings.Join(lines, "\n")
}

func (m model) details(j monitor.Job, w int) []string {
	dim := m.colors.fg(m.colors.dim)
	status := m.colors.fg(m.colors.state[j.State]).Render(format.Icon(j.State) + " " + format.Label(j))

	var source string
	switch j.Source {
	case monitor.Cron:
		source = "cron · " + j.Schedule
		if !j.Next.IsZero() {
			source += " · next " + format.Clock(j.Next, m.now)
		}
	case monitor.Systemd:
		source = "systemd service · " + j.Description
	default:
		source = "manual · started with bw run"
	}

	var last string
	switch {
	case j.Started.IsZero():
		last = "no runs recorded"
	case j.State == monitor.Running:
		last = "running since " + format.Clock(j.Started, m.now) + " · " + format.Duration(j.Duration)
	default:
		last = "last run " + format.Clock(j.Started, m.now) + " · took " + format.Duration(j.Duration)
	}

	command := "$ " + j.Command
	if j.Source == monitor.Systemd {
		command = "$ journalctl --user -u " + j.Name
	}

	lines := []string{
		lipgloss.NewStyle().Bold(true).Render(j.Name) + "  " + status,
		source,
		last,
		dim.Render(command),
	}
	for i, line := range lines {
		lines[i] = format.Truncate(line, w)
	}
	return lines
}

// separator divide detalhes e log. Mostra a posição quando o usuário rolou
// para cima, porque aí o log para de seguir o fim.
func (m model) separator(w int) string {
	color := m.colors.border
	if m.focus == logPane {
		color = m.colors.accent
	}
	label := " log "
	if !m.log.AtBottom() {
		label = fmt.Sprintf(" log · %.0f%% ", m.log.ScrollPercent()*100)
	}
	line := "──" + label + strings.Repeat("─", max(w-2-lipgloss.Width(label), 0))
	return m.colors.fg(color).Render(format.Truncate(line, w))
}

func (m model) renderFooter() string {
	key := func(k, desc string) string {
		return m.colors.fg(m.colors.accent).Render(k) + " " + m.colors.fg(m.colors.dim).Render(desc)
	}
	if m.confirming {
		job, _ := m.selected()
		question := "run " + job.Name + " now?"
		if job.Source == monitor.Systemd {
			question = "restart " + job.Name + "?"
		}
		prompt := lipgloss.NewStyle().Bold(true).Foreground(m.colors.warn).Render(question)
		return format.Truncate(prompt+"  "+key("y", "yes")+"  "+key("any key", "no"), m.width)
	}

	help := []string{key("↑↓", "move"), key("tab", "log"), key("r", "run now"), key("q", "quit")}
	if m.focus == logPane {
		help = []string{key("↑↓ pgup pgdn", "scroll"), key("esc", "jobs"), key("r", "run now"), key("q", "quit")}
	}
	right := strings.Join(help, "  ")
	if m.notice == "" {
		return format.Truncate(right, m.width)
	}
	left := format.Truncate(m.colors.fg(m.colors.warn).Render(m.notice), m.width-lipgloss.Width(right)-2)
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 2)
	return format.Truncate(left+strings.Repeat(" ", gap)+right, m.width)
}
