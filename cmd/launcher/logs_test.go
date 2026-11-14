package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
)

// T16 (D16): S = min(daemonStart, max(cursor, haStartedAt, daemonStart − 10 min)).
func TestLogsSince(t *testing.T) {
	start := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := start.Add(d); return &t }
	cases := []struct {
		name              string
		cursor, startedAt *time.Time
		want              time.Time
	}{
		{"fresh cursor", at(-5 * time.Second), at(-3 * time.Hour), start.Add(-5 * time.Second)},
		{"HA started after the cursor", at(-2 * time.Minute), at(-30 * time.Second), start.Add(-30 * time.Second)},
		{"both older than 10 min", at(-50 * time.Minute), at(-3 * time.Hour), start.Add(-10 * time.Minute)},
		{"cursor in the future", at(time.Hour), at(-3 * time.Hour), start},
		{"HA started in the future", nil, at(time.Hour), start},
		{"all missing", nil, nil, start.Add(-10 * time.Minute)},
	}
	for _, tc := range cases {
		if got := logsSince(tc.cursor, tc.startedAt, start); !got.Equal(tc.want) {
			t.Errorf("%s: logsSince = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCursorFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon-logs-cursor")
	if readCursor(path) != nil {
		t.Error("a missing cursor must read as nil")
	}
	if err := os.WriteFile(path, []byte("yesterday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if readCursor(path) != nil {
		t.Error("an unparseable cursor must read as nil")
	}
	want := time.Date(2026, 11, 14, 12, 0, 0, 123, time.FixedZone("CET", 3600))
	writeCursor(path, want)
	if got := readCursor(path); got == nil || !got.Equal(want) {
		t.Errorf("cursor round trip = %v, want %v", got, want)
	}
	if readCursor("") != nil {
		t.Error(`path "" (no SNAP_COMMON) must read as nil`)
	}
	writeCursor("", want) // must not panic or write anywhere
}

// T17 (D16): the daemon follows from logsSince and records the cursor when
// the follow returns.
func TestFollowHALogs(t *testing.T) {
	start := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	path := filepath.Join(t.TempDir(), "daemon-logs-cursor")
	f := haFake("2026.11.1")
	f.errs["inspect -f {{.State.StartedAt}} homeassistant"] = errors.New("No such container")
	writeCursor(path, start.Add(-20*time.Second))
	if err := followHALogs(docker.NewWithRunner(f), path, start, func() time.Time { return end }); err != nil {
		t.Fatal(err)
	}
	if want := "logs -f --since 2026-11-14T11:59:40Z homeassistant"; f.count(want) != 1 {
		t.Errorf("calls %q, want one %q", f.calls, want)
	}
	if got := readCursor(path); got == nil || !got.Equal(end) {
		t.Errorf("cursor after the follow = %v, want %v", got, end)
	}
}
