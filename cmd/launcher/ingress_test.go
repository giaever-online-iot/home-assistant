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
	keyTestConfigYAML  = "exec homeassistant test -e /config/configuration.yaml"
	keyCatConfigYAML   = "exec homeassistant cat /config/configuration.yaml"
	keyWriteConfigYAML = "exec -i homeassistant sh -c cat > /config/configuration.yaml"
	includeYAMLLine    = "ingress: !include snap-ingress.yaml"
	blankWarn          = "warning: configuration.yaml is empty; the ingress include is added on the next reconcile once it has content\n"
)

// T13a (D8a): configuration.yaml is never created, and never written from a
// failed read.
func TestEnsureIncludeHardenedRead(t *testing.T) {
	cases := []struct {
		name     string
		exists   bool
		catFails bool   // cat exits non-zero on a file that exists
		content  string // what cat prints when it succeeds
		changed  bool
		written  string // stdin of the configuration.yaml write; "" = no write
		wantWarn string
	}{
		{name: "absent", exists: false,
			wantWarn: "warning: configuration.yaml not found yet; the ingress include is added on the next reconcile\n"},
		{name: "read error", exists: true, catFails: true,
			wantWarn: "warning: could not read /config/configuration.yaml (exit status 1); the ingress include was not checked\n"},
		{name: "present but empty", exists: true, content: "",
			wantWarn: blankWarn},
		{name: "present, whitespace only", exists: true, content: "\n  \n",
			wantWarn: blankWarn},
		{name: "present without the include", exists: true, content: "default_config:\nhttp:\n  server_port: 8123",
			changed: true, written: "default_config:\nhttp:\n  server_port: 8123\n" + includeYAMLLine + "\n"},
		{name: "include present", exists: true, content: "default_config:\n" + includeYAMLLine},
		{name: "foreign ingress key", exists: true, content: "ingress:\n  foo:\n    url: http://x",
			wantWarn: "warning: /config/configuration.yaml already defines its own `ingress:` key — leaving it untouched.\n"},
	}
	for _, tc := range cases {
		f := haFake("2026.11.1")
		f.checks[keyTestConfigYAML] = tc.exists
		if tc.exists && tc.catFails {
			f.errs[keyCatConfigYAML] = errors.New("exit status 1")
		} else if tc.exists {
			f.outputs[keyCatConfigYAML] = tc.content
		}
		var buf bytes.Buffer
		changed, err := ensureInclude(docker.NewWithRunner(f), &buf)
		if err != nil {
			t.Errorf("%s: ensureInclude: %v", tc.name, err)
		}
		if changed != tc.changed {
			t.Errorf("%s: changed = %v, want %v", tc.name, changed, tc.changed)
		}
		got, wrote := f.stdin[keyWriteConfigYAML]
		if tc.written == "" && wrote {
			t.Errorf("%s: configuration.yaml was written with %q; it must not be", tc.name, got)
		}
		if tc.written != "" && got != tc.written {
			t.Errorf("%s: configuration.yaml written as %q, want %q", tc.name, got, tc.written)
		}
		if tc.wantWarn != "" && !bytes.Contains(buf.Bytes(), []byte(tc.wantWarn)) {
			t.Errorf("%s: output %q lacks %q", tc.name, buf.String(), tc.wantWarn)
		}
		if tc.wantWarn == "" && tc.written == "" && buf.Len() != 0 {
			t.Errorf("%s: unexpected output %q", tc.name, buf.String())
		}
	}
}

const unchangedMsg = "ingress: panels unchanged; Home Assistant not restarted (use --force to restart anyway)"

// zwaveCfg holds one explicit panel.
var zwaveCfg = config.Config{Ingress: map[string]config.IngressSpec{
	"zwave": {URL: "http://localhost:8091", Title: "Z-Wave JS UI", WorkMode: "ingress"},
}}

// syncFake is a running HA with hass_ingress installed in YAML mode,
// snap-ingress.yaml holding file ("" = absent) and configuration.yaml
// holding configYAML.
func syncFake(file, configYAML string) *fakeRunner {
	f := haFake("2026.11.1", ingressDir)
	f.outputs[keyStorage] = storageYAML
	if file != "" {
		f.outputs["exec homeassistant cat "+ingressFilePath] = strings.TrimSpace(file)
	}
	f.checks[keyTestConfigYAML] = true
	f.outputs[keyCatConfigYAML] = configYAML
	return f
}

// T9 (D7, D8): manual sync is compare-first; --force writes and restarts;
// the include line is checked even when the panel file is unchanged.
func TestIngressSyncCompareFirst(t *testing.T) {
	rendered := ingress.Render(ingress.MergedEntries(zwaveCfg))
	headerless := strings.Join(strings.Split(rendered, "\n")[2:], "\n")
	withInclude := "default_config:\n" + includeYAMLLine
	cases := []struct {
		name                           string
		args                           []string
		file, configYAML               string
		writes, configWrites, restarts int
		unchanged                      bool   // stdout carries unchangedMsg
		stderr                         string // a substring stderr must carry
	}{
		{"unchanged", []string{"sync"}, rendered, withInclude, 0, 0, 0, true, ""},
		{"--force", []string{"sync", "--force"}, rendered, withInclude, 1, 0, 1, false, ""},
		{"include missing, file unchanged", []string{"sync"}, rendered, "default_config:", 0, 1, 1, false, "Added the ingress include; restarting Home Assistant"},
		{"foreign ingress key, file unchanged", []string{"sync"}, rendered, "ingress:\n  foo:\n    url: http://x", 0, 0, 0, true, "already defines its own `ingress:` key"},
		{"panel changed", []string{"sync"}, "old:\n  url: \"http://x\"", withInclude, 1, 0, 1, false, "Wrote 1 ingress panel(s); restarting Home Assistant"},
		{"header-only change", []string{"sync"}, "# an older header\n" + headerless, withInclude, 1, 0, 0, true, ""},
	}
	for _, tc := range cases {
		f := syncFake(tc.file, tc.configYAML)
		var stdout, stderr bytes.Buffer
		if err := runIngress(docker.NewWithRunner(f), zwaveCfg, tc.args, &stdout, &stderr); err != nil {
			t.Errorf("%s: runIngress: %v", tc.name, err)
			continue
		}
		if n := f.count(keyWriteIngress); n != tc.writes {
			t.Errorf("%s: snap-ingress.yaml written %d times, want %d", tc.name, n, tc.writes)
		}
		if n := f.count(keyWriteConfigYAML); n != tc.configWrites {
			t.Errorf("%s: configuration.yaml written %d times, want %d", tc.name, n, tc.configWrites)
		}
		if n := f.count(keyRestart); n != tc.restarts {
			t.Errorf("%s: restart called %d times, want %d", tc.name, n, tc.restarts)
		}
		if got := strings.Contains(stdout.String(), unchangedMsg); got != tc.unchanged {
			t.Errorf("%s: stdout %q; carries the unchanged message = %v, want %v", tc.name, stdout.String(), got, tc.unchanged)
		}
		if tc.stderr != "" && !strings.Contains(stderr.String(), tc.stderr) {
			t.Errorf("%s: stderr %q lacks %q", tc.name, stderr.String(), tc.stderr)
		}
	}
}

func TestIngressSyncRejectsUnknownOption(t *testing.T) {
	f := syncFake("", "")
	err := runIngress(docker.NewWithRunner(f), zwaveCfg, []string{"sync", "--frce"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), `"--frce"`) {
		t.Errorf("err = %v, want an unknown-option error naming --frce", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("a usage error must not touch docker; calls %q", f.calls)
	}
}
