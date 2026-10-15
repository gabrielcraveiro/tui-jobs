package runner_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabrielcraveiro/tui-jobs/internal/runner"
	"github.com/gabrielcraveiro/tui-jobs/internal/runs"
)

func TestExecRecordsRunAndMirrorsOutput(t *testing.T) {
	store := runs.NewStore(t.TempDir())
	var stdout, stderr bytes.Buffer

	code := runner.Exec(store, "hello", []string{"sh", "-c", "echo out; echo err >&2"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d; want 0", code)
	}
	if stdout.String() != "out\n" || stderr.String() != "err\n" {
		t.Errorf("stdout=%q stderr=%q; want the command output unchanged", stdout.String(), stderr.String())
	}
	run := lastRun(t, store, "hello")
	if run.End == nil || run.ExitCode != 0 {
		t.Errorf("recorded End=%v ExitCode=%d; want finished with 0", run.End, run.ExitCode)
	}
	log, _ := os.ReadFile(store.LogPath(run))
	if !strings.Contains(string(log), "out") || !strings.Contains(string(log), "err") {
		t.Errorf("log = %q; want stdout and stderr", log)
	}
}

func TestExecExitCodes(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		want    int
		wantErr bool
	}{
		{"non-zero exit", []string{"sh", "-c", "exit 3"}, 3, false},
		{"killed by SIGTERM", []string{"sh", "-c", "kill -TERM $$"}, 128 + 15, false},
		{"command not found", []string{"no-such-command-bw-test"}, 127, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := runs.NewStore(t.TempDir())
			var out bytes.Buffer

			code := runner.Exec(store, "job", tt.command, &out, &out)

			if code != tt.want {
				t.Errorf("exit code = %d; want %d", code, tt.want)
			}
			run := lastRun(t, store, "job")
			if run.ExitCode != tt.want {
				t.Errorf("recorded exit code = %d; want %d", run.ExitCode, tt.want)
			}
			if (run.Error != "") != tt.wantErr {
				t.Errorf("recorded error = %q; want error: %v", run.Error, tt.wantErr)
			}
		})
	}
}

func TestExecRunsCommandWhenHistoryFails(t *testing.T) {
	// Um arquivo no lugar do diretório do histórico faz o registro falhar.
	notADir := filepath.Join(t.TempDir(), "file")
	os.WriteFile(notADir, nil, 0o600)
	marker := filepath.Join(t.TempDir(), "ran")
	var stdout, stderr bytes.Buffer

	code := runner.Exec(runs.NewStore(notADir), "job", []string{"sh", "-c", "touch " + marker + "; exit 4"}, &stdout, &stderr)

	if _, err := os.Stat(marker); err != nil {
		t.Error("command must run even when the history cannot be written")
	}
	if code != 4 {
		t.Errorf("exit code = %d; want 4", code)
	}
	if !strings.Contains(stderr.String(), "cannot record run") {
		t.Errorf("stderr = %q; want a warning", stderr.String())
	}
}

func TestExecWithoutStoreRunsCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := runner.Exec(nil, "job", []string{"sh", "-c", "echo hi; exit 6"}, &stdout, &stderr)

	if code != 6 || stdout.String() != "hi\n" {
		t.Errorf("Exec(nil) = %d, stdout %q; want 6 and the command output", code, stdout.String())
	}
}

func lastRun(t *testing.T, store *runs.Store, job string) runs.Run {
	t.Helper()
	list, err := store.List(job, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("List(%q) = %v, %v; want 1 run", job, list, err)
	}
	return list[0]
}
