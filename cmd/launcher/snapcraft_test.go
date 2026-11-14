package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// snapcraftApp returns the indented key/value lines of one app in
// snap/snapcraft.yaml. The recipe is plain two-space-indented YAML, so a line
// scan is enough and avoids adding a YAML dependency for a single guard.
func snapcraftApp(t *testing.T, app string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "snap", "snapcraft.yaml"))
	if err != nil {
		t.Fatalf("read snapcraft.yaml: %v", err)
	}
	inApps, inApp := false, false
	fields := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "apps:"):
			inApps = true
		case inApps && strings.HasPrefix(line, "  "+app+":"):
			inApp = true
		case inApp && strings.HasPrefix(line, "    "):
			k, v, _ := strings.Cut(strings.TrimSpace(line), ":")
			fields[k] = strings.TrimSpace(v)
		case inApp:
			return fields // first line not indented under the app ends it
		}
	}
	if !inApp {
		t.Fatalf("app %q not found under apps: in snapcraft.yaml", app)
	}
	return fields
}

// The daemon starts as soon as network.target is reached, before dockerd has
// its socket up, and its docker calls exit 1 until then. With systemd's
// default 100 ms restart delay that burns StartLimitBurst (5 in 10 s) and the
// unit is left failed for the whole boot. A restart delay keeps the retries
// below the limit so the daemon comes up once docker is ready.
func TestDaemonAppRestartsWithDelay(t *testing.T) {
	daemon := snapcraftApp(t, "daemon")
	if got := daemon["restart-condition"]; got != "always" {
		t.Errorf("daemon restart-condition = %q, want %q", got, "always")
	}
	if got := daemon["restart-delay"]; got != "10s" {
		t.Errorf("daemon restart-delay = %q, want %q", got, "10s")
	}
}

// T15 (D15): the configure hook prunes empty maps only after validate has
// accepted the config, before the daemon restart, and never lets pruning
// reject a `snap set`.
func TestConfigureHookPrunesAfterValidate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "snap", "hooks", "configure"))
	if err != nil {
		t.Fatalf("read the configure hook: %v", err)
	}
	hook := string(raw)
	validate := strings.Index(hook, "\n\"$SNAP/bin/launcher\" validate\n")
	prune := strings.Index(hook, "\n\"$SNAP/bin/launcher\" prune-config || true\n")
	restart := strings.Index(hook, "snapctl restart")
	if validate < 0 || prune < 0 || restart < 0 {
		t.Fatalf("hook lacks a validate line, a guarded prune-config line or the restart (%d, %d, %d):\n%s", validate, prune, restart, hook)
	}
	if !(validate < prune && prune < restart) {
		t.Errorf("order is validate %d, prune-config %d, restart %d; want validate < prune-config < restart", validate, prune, restart)
	}
}
