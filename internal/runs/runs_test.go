package runs_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/gabrielcraveiro/tui-jobs/internal/runs"
)

func TestBeginFinishList(t *testing.T) {
	store := runs.NewStore(t.TempDir())

	run, logFile, err := store.Begin("scrap", []string{"./run.sh"})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(logFile, "hello")
	logFile.Close()

	list, err := store.List("scrap", 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v; want 1 run", list, err)
	}
	if list[0].End != nil {
		t.Error("run must have no End before Finish")
	}

	if err := store.Finish(run, 3, 4096, ""); err != nil {
		t.Fatal(err)
	}
	list, _ = store.List("scrap", 0)
	if list[0].End == nil || list[0].ExitCode != 3 || list[0].PeakRSS != 4096 {
		t.Errorf("after Finish: End=%v ExitCode=%d PeakRSS=%d; want End set, ExitCode 3, PeakRSS 4096", list[0].End, list[0].ExitCode, list[0].PeakRSS)
	}
	if data, _ := os.ReadFile(store.LogPath(list[0])); string(data) != "hello" {
		t.Errorf("log = %q; want %q", data, "hello")
	}
}

func TestListNewestFirstAndPrune(t *testing.T) {
	store := runs.NewStore(t.TempDir())
	for i := range 4 {
		run, logFile, err := store.Begin("scrap", []string{"cmd"})
		if err != nil {
			t.Fatal(err)
		}
		logFile.Close()
		store.Finish(run, i, 0, "")
	}

	list, _ := store.List("scrap", 2)
	if len(list) != 2 || list[0].ExitCode != 3 || list[1].ExitCode != 2 {
		t.Fatalf("List(2) exit codes = %v; want newest first [3 2]", exitCodes(list))
	}

	if err := store.Prune("scrap", 2); err != nil {
		t.Fatal(err)
	}
	list, _ = store.List("scrap", 0)
	if got := exitCodes(list); len(got) != 2 || got[0] != 3 || got[1] != 2 {
		t.Errorf("after Prune(2) exit codes = %v; want [3 2]", got)
	}
	if _, err := os.Stat(store.LogPath(list[1])); err != nil {
		t.Errorf("kept run lost its log: %v", err)
	}
}

func TestTail(t *testing.T) {
	store := runs.NewStore(t.TempDir())
	run, logFile, _ := store.Begin("bot", []string{"cmd"})
	fmt.Fprint(logFile, "first line\nsecond line\nthird line\n")
	logFile.Close()

	whole, err := store.Tail(*run, 1024)
	if err != nil || whole != "first line\nsecond line\nthird line\n" {
		t.Errorf("Tail(1024) = %q, %v; want the whole log", whole, err)
	}
	// 15 bytes começam no meio de "second line": a linha cortada sai.
	end, _ := store.Tail(*run, 15)
	if end != "third line\n" {
		t.Errorf("Tail(15) = %q; want only the complete last line", end)
	}
}

func TestJobs(t *testing.T) {
	store := runs.NewStore(t.TempDir())
	if jobs, err := store.Jobs(); err != nil || len(jobs) != 0 {
		t.Fatalf("empty store: Jobs = %v, %v", jobs, err)
	}
	for _, job := range []string{"transito", "scrap"} {
		_, logFile, _ := store.Begin(job, []string{"cmd"})
		logFile.Close()
	}
	jobs, _ := store.Jobs()
	if len(jobs) != 2 || jobs[0] != "scrap" || jobs[1] != "transito" {
		t.Errorf("Jobs = %v; want [scrap transito]", jobs)
	}
}

func TestRejectsUnsafeJobName(t *testing.T) {
	store := runs.NewStore(t.TempDir())
	for _, name := range []string{"", "../x", "a/b", "with space", "-flag"} {
		if _, _, err := store.Begin(name, []string{"cmd"}); err == nil {
			t.Errorf("Begin(%q) must fail", name)
		}
		if _, err := store.List(name, 0); err == nil {
			t.Errorf("List(%q) must fail", name)
		}
	}
}

func TestAlive(t *testing.T) {
	store := runs.NewStore(t.TempDir())
	run, logFile, _ := store.Begin("bot", []string{"cmd"})
	logFile.Close()

	if !runs.Alive(*run) {
		t.Error("run of this process must be alive")
	}
	otherBoot := *run
	otherBoot.BootID = "boot-before-restart"
	if runs.Alive(otherBoot) {
		t.Error("run from another boot must be dead even if the PID exists")
	}
	deadPID := *run
	deadPID.PID = 1 << 30 // acima do pid_max do Linux
	if runs.Alive(deadPID) {
		t.Error("run with a missing PID must be dead")
	}
}

func TestDefaultDir(t *testing.T) {
	tests := []struct {
		name, botwatcherDir, xdgState, home string
		want                                string
		wantErr                             bool
	}{
		{name: "BOTWATCHER_DIR wins", botwatcherDir: "/b", xdgState: "/x", home: "/h", want: "/b"},
		{name: "XDG_STATE_HOME", xdgState: "/x", home: "/h", want: "/x/botwatcher"},
		{name: "HOME", home: "/h", want: "/h/.local/state/botwatcher"},
		{name: "nothing set", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BOTWATCHER_DIR", tt.botwatcherDir)
			t.Setenv("XDG_STATE_HOME", tt.xdgState)
			t.Setenv("HOME", tt.home)

			got, err := runs.DefaultDir()

			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("DefaultDir() = %q, %v; want %q, error: %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func exitCodes(list []runs.Run) []int {
	var codes []int
	for _, r := range list {
		codes = append(codes, r.ExitCode)
	}
	return codes
}
