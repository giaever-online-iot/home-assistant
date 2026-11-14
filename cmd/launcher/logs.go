package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
)

// logsReplayFloor caps how far back the daemon's log follow reaches when
// neither the cursor nor HA's start time is newer (D16).
const logsReplayFloor = 10 * time.Minute

// logsSince is where the daemon's `docker logs -f --since` starts (D16):
// min(daemonStart, max(cursor, startedAt, daemonStart − 10 min)), with a nil
// time ignored. A fresh cursor (a `snap set` restart) means no replay; HA
// started after the cursor (a reboot) shows HA's whole boot log once; the
// floor caps a stale or missing cursor; and the outer min keeps --since out
// of the future, so a clock that comes up behind cannot silence the journal.
func logsSince(cursor, startedAt *time.Time, daemonStart time.Time) time.Time {
	since := daemonStart.Add(-logsReplayFloor)
	for _, t := range []*time.Time{cursor, startedAt} {
		if t != nil && t.After(since) {
			since = *t
		}
	}
	if since.After(daemonStart) {
		since = daemonStart
	}
	return since
}

// logsCursorPath is $SNAP_COMMON/daemon-logs-cursor (it survives refreshes),
// or "" outside a snap.
func logsCursorPath() string {
	common := os.Getenv("SNAP_COMMON")
	if common == "" {
		return ""
	}
	return filepath.Join(common, "daemon-logs-cursor")
}

// readCursor is the time in the cursor file, or nil when the path is "" or
// the file is missing or unparseable.
func readCursor(path string) *time.Time {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(raw)))
	if err != nil {
		return nil
	}
	return &t
}

// writeCursor records t (RFC3339Nano, UTC). Best-effort: the write is atomic (temp file, then rename); a failed write only
// costs a capped replay on the next start.
func writeCursor(path string, t time.Time) {
	if path == "" {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(t.UTC().Format(time.RFC3339Nano)+"\n"), 0o644); err != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// followHALogs follows HA's log from logsSince and records the cursor when
// the follow returns, for any reason.
func followHALogs(cli *docker.Client, cursorPath string, daemonStart time.Time, now func() time.Time) error {
	var startedAt *time.Time
	if t, err := cli.StartedAt(dockerargs.ContainerName); err == nil {
		startedAt = &t
	}
	err := cli.FollowLogs(dockerargs.ContainerName, logsSince(readCursor(cursorPath), startedAt, daemonStart))
	writeCursor(cursorPath, now())
	return err
}
