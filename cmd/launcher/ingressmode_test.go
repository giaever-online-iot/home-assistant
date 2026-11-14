package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/ingress"
)

const (
	keyStorage      = "exec homeassistant cat /config/.storage/core.config_entries"
	keyWriteIngress = "exec -i homeassistant sh -c cat > /config/snap-ingress.yaml"
	storageYAML     = `{"data": {"entries": [{"domain": "ingress", "data": {"mode": "yaml"}}]}}`
	storageUI       = `{"data": {"entries": [{"domain": "ingress", "data": {"mode": "ui"}}]}}`
)

func TestWarnIngressMode(t *testing.T) {
	cases := []struct {
		name, storage string // "" = the file is missing
		want          string
	}{
		{"absent", "", ""},
		{"yaml", storageYAML, ""},
		{"ui", storageUI, ingress.ModeWarning(ingress.ModeOther, "ui")},
		{"not json", "not json", ingress.ModeWarning(ingress.ModeUnreadable, "")},
	}
	for _, tc := range cases {
		f := haFake("2026.11.1")
		if tc.storage == "" {
			f.errs[keyStorage] = errors.New("exit status 1")
		} else {
			f.outputs[keyStorage] = tc.storage
		}
		var buf bytes.Buffer
		warnIngressMode(docker.NewWithRunner(f), &buf)
		if buf.String() != tc.want {
			t.Errorf("%s: wrote %q, want %q", tc.name, buf.String(), tc.want)
		}
		if n := f.count(keyStorage); n != 1 {
			t.Errorf("%s: storage read %d times, want 1", tc.name, n)
		}
	}
}

// T7: sync checks the mode before it writes, and only with a panel configured.
// The ingress dir exists so this test still holds once the check moves into
// PR #6's applyIngress, which checks only when the dir is there (D12).
// W-13 D19: the restart is asserted on `sync --force`; a plain sync of an
// unchanged file checks the mode the same way and does not restart.
func TestIngressSyncChecksMode(t *testing.T) {
	panels := map[string]config.IngressSpec{"zui": {URL: "http://localhost:8091", Title: "Z-Wave JS UI"}}
	cases := []struct {
		name    string
		ingress map[string]config.IngressSpec
		reads   int
	}{
		{"one panel", panels, 1},
		{"no panels", nil, 0},
	}
	for _, tc := range cases {
		f := haFake("2026.11.1", ingressDir)
		f.outputs[keyStorage] = storageYAML
		if err := runIngress(docker.NewWithRunner(f), config.Config{Ingress: tc.ingress}, []string{"sync", "--force"}, io.Discard, io.Discard); err != nil {
			t.Errorf("%s: runIngress sync: %v", tc.name, err)
			continue
		}
		if n := f.count(keyStorage); n != tc.reads {
			t.Errorf("%s: storage read %d times, want %d; calls %q", tc.name, n, tc.reads, f.calls)
		}
		if n := f.count(keyRestart); n != 1 {
			t.Errorf("%s: restart called %d times, want 1", tc.name, n)
		}
		if tc.reads > 0 && indexOfCall(f.calls, keyStorage) > indexOfCall(f.calls, keyWriteIngress) {
			t.Errorf("%s: the mode check ran after the write; calls %q", tc.name, f.calls)
		}
	}

	cfg := config.Config{Ingress: panels}
	f := haFake("2026.11.1", ingressDir)
	f.outputs[keyStorage] = storageYAML
	f.outputs["exec homeassistant cat "+ingressFilePath] = strings.TrimSpace(ingress.Render(ingress.MergedEntries(cfg)))
	if err := runIngress(docker.NewWithRunner(f), cfg, []string{"sync"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("no-force sync: %v", err)
	}
	if n := f.count(keyStorage); n != 1 {
		t.Errorf("no-force sync: storage read %d times, want 1", n)
	}
	if n := f.count(keyRestart); n != 0 {
		t.Errorf("no-force sync of an unchanged file: restart called %d times, want 0", n)
	}
}

// indexOfCall is the index of the first recorded call starting with prefix, or -1.
func indexOfCall(calls []string, prefix string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

// T8: an unchanged auto-sync (Decide says no write) still checks the mode when
// hass_ingress is installed, and still neither writes nor restarts (D11, D12).
func TestApplyIngressAutoSyncChecksMode(t *testing.T) {
	cfg := config.Config{Ingress: map[string]config.IngressSpec{"zui": {URL: "http://localhost:8091", Title: "Z-Wave JS UI"}}}
	cases := []struct {
		name  string
		dir   bool // custom_components/ingress exists
		reads int
	}{
		{"installed", true, 1},
		{"not installed", false, 0},
	}
	for _, tc := range cases {
		var dirs []string
		if tc.dir {
			dirs = []string{ingressDir}
		}
		f := haFake("2026.11.1", dirs...)
		f.outputs[keyStorage] = storageUI
		f.outputs["exec homeassistant cat "+ingressFilePath] = ingress.Render(ingress.MergedEntries(cfg))
		if _, err := applyIngress(docker.NewWithRunner(f), cfg, false, io.Discard); err != nil {
			t.Errorf("%s: applyIngress: %v", tc.name, err)
			continue
		}
		if n := f.count(keyStorage); n != tc.reads {
			t.Errorf("%s: storage read %d times, want %d; calls %q", tc.name, n, tc.reads, f.calls)
		}
		if n := f.count(keyWriteIngress) + f.count(keyRestart); n != 0 {
			t.Errorf("%s: write/restart ran %d times on an unchanged auto-sync, want 0", tc.name, n)
		}
	}
}
