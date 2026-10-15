package systemd

import (
	"testing"
	"time"
)

// Saída real do systemd 255: as chaves não vêm na ordem pedida.
const showOutput = `Result=success
NRestarts=0
ExecMainStatus=0
Id=ecovias-bot.service
Description=Bot do Telegram Status Ecovias Imigrantes
ActiveState=active
SubState=running
ActiveEnterTimestamp=@1792069539
InactiveEnterTimestamp=

Id=flaky.service
Description=Crashing bot
ActiveState=activating
SubState=auto-restart
ActiveEnterTimestamp=@1792069000
InactiveEnterTimestamp=@1792069100
ExecMainStatus=1
NRestarts=7
`

func TestParseShow(t *testing.T) {
	units := parseShow(showOutput)

	if len(units) != 2 {
		t.Fatalf("parseShow returned %d units; want 2", len(units))
	}
	bot := units[0]
	if bot.Name != "ecovias-bot" || bot.ActiveState != "active" || bot.SubState != "running" {
		t.Errorf("unit 0 = %+v; want ecovias-bot active/running", bot)
	}
	if !bot.Since.Equal(time.Unix(1792069539, 0)) || !bot.Until.IsZero() {
		t.Errorf("unit 0 Since=%v Until=%v; want Since from the timestamp and zero Until", bot.Since, bot.Until)
	}
	flaky := units[1]
	if flaky.Restarts != 7 || flaky.ExitStatus != 1 || flaky.SubState != "auto-restart" {
		t.Errorf("unit 1 = %+v; want 7 restarts, exit 1, auto-restart", flaky)
	}
}

func TestParseShowEmpty(t *testing.T) {
	if units := parseShow(""); len(units) != 0 {
		t.Errorf("parseShow(\"\") = %v; want no units", units)
	}
}
