package procs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gabrielcraveiro/tui-jobs/internal/procs"
)

// fakeProc monta um /proc com o formato real de /proc/<pid>/stat.
func fakeProc(t *testing.T, procs map[int]struct {
	name       string
	ppid, rssP int
}) string {
	t.Helper()
	root := t.TempDir()
	for pid, p := range procs {
		dir := filepath.Join(root, fmt.Sprint(pid))
		os.MkdirAll(dir, 0o755)
		// pid (comm) state ppid pgrp session tty tpgid flags minflt cminflt majflt
		// cmajflt utime stime cutime cstime priority nice threads itreal starttime vsize rss ...
		stat := fmt.Sprintf("%d (%s) S %d 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 100 1000 %d 0 0\n",
			pid, p.name, p.ppid, p.rssP)
		os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644)
	}
	os.MkdirAll(filepath.Join(root, "self"), 0o755) // entrada que não é pid
	return root
}

func TestDescendantsAndCgroup(t *testing.T) {
	page := int64(os.Getpagesize())
	proc := fakeProc(t, map[int]struct {
		name       string
		ppid, rssP int
	}{
		1:   {"systemd", 0, 10},
		100: {"bw", 1, 5},                // bw run
		101: {"sh", 100, 1},              // filho do bw
		102: {"python (bot) x", 101, 20}, // neto, nome com espaço e parêntese
		200: {"other", 1, 50},
	})
	cgroups := t.TempDir()
	os.MkdirAll(filepath.Join(cgroups, "app.slice/bot.service"), 0o755)
	os.WriteFile(filepath.Join(cgroups, "app.slice/bot.service/cgroup.procs"), []byte("102\n200\n"), 0o644)

	snap := procs.ReadFrom(proc, cgroups)

	if got, want := snap.Descendants(100), 21*page; got != want {
		t.Errorf("Descendants(100) = %d; want %d (sh + python, without bw)", got, want)
	}
	if got := snap.Descendants(102); got != 0 {
		t.Errorf("Descendants(102) = %d; want 0 for a process without children", got)
	}
	if got, want := snap.Cgroup("/app.slice/bot.service"), 70*page; got != want {
		t.Errorf("Cgroup = %d; want %d", got, want)
	}
	if got := snap.Cgroup("/missing.service"); got != 0 {
		t.Errorf("Cgroup of a missing cgroup = %d; want 0", got)
	}
}

func TestReadRealProc(t *testing.T) {
	// O próprio processo do teste é filho de alguém e tem RSS.
	snap := procs.Read()
	if snap.Descendants(os.Getppid()) <= 0 {
		t.Error("the parent of the test process must have descendants with RSS")
	}
}
