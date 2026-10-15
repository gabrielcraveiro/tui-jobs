package crontab_test

import (
	"testing"

	"github.com/gabrielcraveiro/tui-jobs/internal/crontab"
)

const sample = `
# m h dom mon dow command
MAILTO=""
PATH=/usr/local/bin:/usr/bin:/bin
*/15 * * * * /home/u/.local/bin/bw run pokescrap -- /home/u/pokescrap/run.sh
0 * * * * export $(grep -v '^#' /x/.env | xargs) && /x/.venv/bin/python3 /x/watch.py >> /tmp/watch.log 2>&1
40 7 * * 1-5 cd /x && bw run paper -- python paper.py >> paper.log 2>&1 # paper bot
#DESLIGADO 0 20 * * 0 cd /x && bw run reconcile -- python reconcile.py
#DESLIGADO */15 * * * * cd /x && python old.py
@reboot bw run addon -- ./addon.sh
61 * * * * bw run invalid -- ./never.sh
* * * * *
*/5 * * * * notbw run x -- ./y.sh
`

func TestParse(t *testing.T) {
	want := []crontab.Entry{
		{Spec: "*/15 * * * *", Job: "pokescrap"},
		{Spec: "0 * * * *", Job: ""},
		{Spec: "40 7 * * 1-5", Job: "paper", Comment: "paper bot"},
		{Spec: "0 20 * * 0", Job: "reconcile", Disabled: true},
		{Spec: "@reboot", Job: "addon"},
		{Spec: "*/5 * * * *", Job: ""},
	}

	got := crontab.Parse(sample)

	if len(got) != len(want) {
		for _, e := range got {
			t.Logf("got %+v", e)
		}
		t.Fatalf("Parse returned %d entries; want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Spec != w.Spec || g.Job != w.Job || g.Comment != w.Comment || g.Disabled != w.Disabled {
			t.Errorf("entry %d = {Spec:%q Job:%q Comment:%q Disabled:%v}; want %+v",
				i, g.Spec, g.Job, g.Comment, g.Disabled, w)
		}
		if (g.Schedule == nil) != (g.Spec == "@reboot") {
			t.Errorf("entry %d (%s): Schedule must be nil only for @reboot", i, g.Spec)
		}
	}
}

func TestParseKeepsCommandIntact(t *testing.T) {
	got := crontab.Parse("0 6 * * *\t/x/run.sh  --flag 'a  b'\n")
	if len(got) != 1 || got[0].Command != "/x/run.sh  --flag 'a  b'" {
		t.Errorf("Parse = %+v; want the command with its original spacing", got)
	}
}
