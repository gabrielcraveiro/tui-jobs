package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUsageErrors(t *testing.T) {
	t.Setenv("BOTWATCHER_DIR", t.TempDir())
	for _, args := range [][]string{
		{},
		{"nope"},
		{"run"},
		{"run", "job", "echo", "hi"}, // sem "--"
		{"run", "job", "--"},
		{"logs"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("bw %v: exit code = %d; want 2", args, code)
		}
	}
}

func TestWithoutHistoryDir(t *testing.T) {
	t.Setenv("BOTWATCHER_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	var stdout, stderr bytes.Buffer

	if code := run([]string{"run", "job", "--", "sh", "-c", "exit 6"}, &stdout, &stderr); code != 6 {
		t.Errorf("bw run: exit code = %d; want 6, the job runs without history", code)
	}
	if !strings.Contains(stderr.String(), "no history directory") {
		t.Errorf("bw run: stderr = %q; want a warning", stderr.String())
	}
	if code := run([]string{"ls"}, &stdout, &stderr); code != 1 {
		t.Errorf("bw ls: exit code = %d; want 1", code)
	}
}

func TestRunThenLogs(t *testing.T) {
	t.Setenv("BOTWATCHER_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run([]string{"run", "hello", "--", "sh", "-c", "echo hi from job; exit 5"}, &stdout, &stderr)
	if code != 5 {
		t.Fatalf("bw run: exit code = %d; want the exit code of the command (5)", code)
	}

	stdout.Reset()
	if code := run([]string{"logs", "hello"}, &stdout, &stderr); code != 0 {
		t.Fatalf("bw logs: exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "hi from job") {
		t.Errorf("bw logs = %q; want the output of the last run", stdout.String())
	}
}
