package format_test

import (
	"testing"
	"time"

	"github.com/gabrielcraveiro/tui-jobs/internal/format"
	"github.com/gabrielcraveiro/tui-jobs/internal/monitor"
)

func TestDuration(t *testing.T) {
	tests := map[time.Duration]string{
		0:                                     "-",
		-time.Second:                          "-",
		37*time.Second + 400*time.Millisecond: "37s",
		4*time.Minute + 12*time.Second:        "4m12s",
		2*time.Hour + 13*time.Minute + 5*time.Second: "2h13m",
		26 * time.Hour: "26h0m",
	}
	for d, want := range tests {
		if got := format.Duration(d); got != want {
			t.Errorf("Duration(%v) = %q; want %q", d, got, want)
		}
	}
}

func TestClock(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 3, 0, 0, time.Local)
	tests := []struct {
		at   time.Time
		want string
	}{
		{time.Time{}, "-"},
		{time.Date(2026, 10, 15, 7, 40, 0, 0, time.Local), "07:40"},
		{time.Date(2026, 10, 14, 21, 58, 0, 0, time.Local), "Oct 14 21:58"},
		{time.Date(2025, 10, 15, 7, 40, 0, 0, time.Local), "Oct 15 07:40"},
	}
	for _, tt := range tests {
		if got := format.Clock(tt.at, now); got != tt.want {
			t.Errorf("Clock(%v) = %q; want %q", tt.at, got, tt.want)
		}
	}
}

func TestLabel(t *testing.T) {
	tests := []struct {
		job  monitor.Job
		want string
	}{
		{monitor.Job{State: monitor.OK}, "ok"},
		{monitor.Job{State: monitor.Failed, ExitCode: 2}, "exit 2"},
		{monitor.Job{State: monitor.Running, Restarts: 3}, "running (3 restarts)"},
		{monitor.Job{State: monitor.Unwatched}, "not wrapped"},
		{monitor.Job{State: monitor.Never}, "never ran"},
	}
	for _, tt := range tests {
		if got := format.Label(tt.job); got != tt.want {
			t.Errorf("Label(%s) = %q; want %q", tt.job.State, got, tt.want)
		}
	}
}

func TestBytesAndMemory(t *testing.T) {
	bytes := map[int64]string{0: "-", 512 << 10: "512K", 85 << 20: "85M", 1288 << 20: "1.3G"}
	for n, want := range bytes {
		if got := format.Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q; want %q", n, got, want)
		}
	}
	memory := []struct {
		job  monitor.Job
		want string
	}{
		{monitor.Job{Memory: 50 << 20, PeakMemory: 90 << 20}, "50M"},
		{monitor.Job{PeakMemory: 90 << 20}, "90M peak"},
		{monitor.Job{}, "-"},
	}
	for _, tt := range memory {
		if got := format.Memory(tt.job); got != tt.want {
			t.Errorf("Memory(%+v) = %q; want %q", tt.job, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := format.Truncate("pokescrap/run.sh", 8); got != "pokescr…" {
		t.Errorf("Truncate = %q; want %q", got, "pokescr…")
	}
	if got := format.Truncate("short", 8); got != "short" {
		t.Errorf("Truncate = %q; want the text unchanged", got)
	}
}
