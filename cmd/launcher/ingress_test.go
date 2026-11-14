package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
)

const (
	keyTestConfigYAML  = "exec homeassistant test -e /config/configuration.yaml"
	keyCatConfigYAML   = "exec homeassistant cat /config/configuration.yaml"
	keyWriteConfigYAML = "exec -i homeassistant sh -c cat > /config/configuration.yaml"
	includeYAMLLine    = "ingress: !include snap-ingress.yaml"
)

// T13a (D8a): configuration.yaml is never created, and never written from a
// failed read.
func TestEnsureIncludeHardenedRead(t *testing.T) {
	cases := []struct {
		name     string
		exists   bool
		content  string // "" with exists = cat fails
		changed  bool
		written  string // stdin of the configuration.yaml write; "" = no write
		wantWarn string
	}{
		{name: "absent", exists: false,
			wantWarn: "warning: configuration.yaml not found yet; the ingress include is added on the next reconcile\n"},
		{name: "read error", exists: true, content: "",
			wantWarn: "warning: could not read /config/configuration.yaml (exit status 1); the ingress include was not checked\n"},
		{name: "present without the include", exists: true, content: "default_config:\nhttp:\n  server_port: 8123",
			changed: true, written: "default_config:\nhttp:\n  server_port: 8123\n" + includeYAMLLine + "\n"},
		{name: "include present", exists: true, content: "default_config:\n" + includeYAMLLine},
		{name: "foreign ingress key", exists: true, content: "ingress:\n  foo:\n    url: http://x",
			wantWarn: "warning: /config/configuration.yaml already defines its own `ingress:` key — leaving it untouched.\n"},
	}
	for _, tc := range cases {
		f := haFake("2026.11.1")
		f.checks[keyTestConfigYAML] = tc.exists
		if tc.exists && tc.content == "" {
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
