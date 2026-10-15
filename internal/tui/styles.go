package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
)

// palette troca de cor conforme o fundo do terminal: as cores do tema escuro
// somem num terminal claro, e o contrário também.
type palette struct {
	accent, dim, border, selectedBg, warn color.Color
	state                                 map[monitor.State]color.Color
}

func newPalette(dark bool) palette {
	pick := lipgloss.LightDark(dark)
	green := pick(lipgloss.Color("#1a7f37"), lipgloss.Color("#3fb950"))
	red := pick(lipgloss.Color("#cf222e"), lipgloss.Color("#f85149"))
	yellow := pick(lipgloss.Color("#9a6700"), lipgloss.Color("#d29922"))
	blue := pick(lipgloss.Color("#0969da"), lipgloss.Color("#58a6ff"))
	gray := pick(lipgloss.Color("#6e7781"), lipgloss.Color("#8b949e"))
	return palette{
		accent:     pick(lipgloss.Color("#8250df"), lipgloss.Color("#bc8cff")),
		dim:        gray,
		border:     pick(lipgloss.Color("#d0d7de"), lipgloss.Color("#30363d")),
		selectedBg: pick(lipgloss.Color("#eaeef2"), lipgloss.Color("#262c36")),
		warn:       yellow,
		state: map[monitor.State]color.Color{
			monitor.OK:          green,
			monitor.Failed:      red,
			monitor.Interrupted: red,
			monitor.Running:     blue,
			monitor.Restarting:  yellow,
			monitor.Late:        yellow,
			monitor.Stopped:     gray,
			monitor.Never:       gray,
			monitor.Disabled:    gray,
			monitor.Unwatched:   gray,
		},
	}
}

func (p palette) fg(c color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}
