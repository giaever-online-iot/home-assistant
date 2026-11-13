package main

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
)

func TestResolveTarget(t *testing.T) {
	r, ok := resolveTarget("hacs")
	if !ok || r.Name != "hacs" || r.Dest != "/config/custom_components/hacs" {
		t.Errorf("resolveTarget(hacs) = %+v, %v", r, ok)
	}
	if _, ok := resolveTarget("nope"); ok {
		t.Error("resolveTarget(nope) should be !ok")
	}
}

func TestInstallTargets(t *testing.T) {
	if got := installTargets(); !reflect.DeepEqual(got, []string{"hacs", "hass-ingress"}) {
		t.Errorf("installTargets() = %v, want [hacs hass-ingress]", got)
	}
}

func TestParseInstallArgs(t *testing.T) {
	cases := []struct {
		args             []string
		target           string
		noRestart, force bool
		wantErr          bool
	}{
		{[]string{"hacs"}, "hacs", false, false, false},
		{[]string{"hacs", "--no-restart"}, "hacs", true, false, false},
		{[]string{"--force", "hacs"}, "hacs", false, true, false},
		{[]string{"hacs", "--no-restart", "--force"}, "hacs", true, true, false},
		{[]string{}, "", false, false, true},                  // no target
		{[]string{"--no-restart"}, "", false, false, true},    // flags only, no target
		{[]string{"hacs", "extra"}, "", false, false, true},   // two targets
		{[]string{"--bogus", "hacs"}, "", false, false, true}, // unknown flag
	}
	for _, tc := range cases {
		target, noRestart, force, err := parseInstallArgs(tc.args)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseInstallArgs(%v) err=%v wantErr=%v", tc.args, err, tc.wantErr)
			continue
		}
		if tc.wantErr {
			continue
		}
		if target != tc.target || noRestart != tc.noRestart || force != tc.force {
			t.Errorf("parseInstallArgs(%v) = (%q,%v,%v), want (%q,%v,%v)",
				tc.args, target, noRestart, force, tc.target, tc.noRestart, tc.force)
		}
	}
}

func TestParseHAVersion(t *testing.T) {
	cases := []struct {
		raw         string
		year, month int
		ok          bool
	}{
		{"2026.11.0", 2026, 11, true},
		{"2026.11.1\n", 2026, 11, true},
		{"2026.12.0b1", 2026, 12, true},
		{"2026.12.0.dev20261110", 2026, 12, true},
		{"2026.11.0.dev20260915", 2026, 11, true},
		{"2026.10.3", 2026, 10, true},
		{"2026.1.0", 2026, 1, true},
		{"2027.1.0", 2027, 1, true},
		{"0.118.5", 0, 118, true},
		{"2026.11", 2026, 11, true},
		{"2026.11.1\r\n", 2026, 11, true}, // Review Focus 1
		{"  2026.10.3  ", 2026, 10, true}, // Review Focus 1
		{"", 0, 0, false},
		{"garbage", 0, 0, false},
		{"v2026.11.0", 0, 0, false},
		{"2026", 0, 0, false},
		{"2026.x.0", 0, 0, false},
		{".11.0", 0, 0, false},
		{"+2026.11.0", 0, 0, false}, // Review Focus 2: Atoi would accept the sign
		{"2026.-1.0", 0, 0, false},  // Review Focus 2
	}
	for _, tc := range cases {
		y, m, ok := parseHAVersion(tc.raw)
		if y != tc.year || m != tc.month || ok != tc.ok {
			t.Errorf("parseHAVersion(%q) = (%d, %d, %v), want (%d, %d, %v)",
				tc.raw, y, m, ok, tc.year, tc.month, tc.ok)
		}
	}
}

func TestMarketplaceBuiltIn(t *testing.T) {
	cases := []struct {
		year, month int
		want        bool
	}{
		{2026, 10, false},
		{2026, 11, true},
		{2026, 12, true},
		{2025, 12, false},
		{2027, 1, true},
		{0, 118, false},
	}
	for _, tc := range cases {
		if got := marketplaceBuiltIn(tc.year, tc.month); got != tc.want {
			t.Errorf("marketplaceBuiltIn(%d, %d) = %v, want %v", tc.year, tc.month, got, tc.want)
		}
	}
}

// fakeRunner is a docker.Runner that answers from canned maps (keyed by the
// args joined with spaces) and records every call the same way.
type fakeRunner struct {
	outputs map[string]string
	errs    map[string]error
	checks  map[string]bool
	calls   []string
}

func (f *fakeRunner) record(args []string) string {
	k := strings.Join(args, " ")
	f.calls = append(f.calls, k)
	return k
}
func (f *fakeRunner) Output(args ...string) (string, error) {
	k := f.record(args)
	return f.outputs[k], f.errs[k]
}
func (f *fakeRunner) Stream(args ...string) error             { f.record(args); return nil }
func (f *fakeRunner) StreamIn(_ string, args ...string) error { f.record(args); return nil }
func (f *fakeRunner) Check(args ...string) (bool, error) {
	k := f.record(args)
	return f.checks[k], f.errs[k]
}

// count returns how many recorded calls start with prefix.
func (f *fakeRunner) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

const (
	keyRunning           = "inspect -f {{.State.Running}} homeassistant"
	keyVersion           = "exec homeassistant cat /config/.HA_VERSION"
	prefixProbe          = "exec homeassistant test -d "
	prefixHACS           = "exec homeassistant bash -c wget -O - https://get.hacs.xyz | bash"
	prefixIngressInstall = "exec homeassistant bash -c set -e;"
	keyRestart           = "restart homeassistant"
)

// haFake is a running homeassistant container. version is the content of
// /config/.HA_VERSION ("" = the file is missing, so cat fails); dirs are the
// directories that `test -d` finds.
func haFake(version string, dirs ...string) *fakeRunner {
	f := &fakeRunner{
		outputs: map[string]string{keyRunning: "true"},
		errs:    map[string]error{},
		checks:  map[string]bool{},
	}
	if version == "" {
		f.errs[keyVersion] = errors.New("exit status 1")
	} else {
		f.outputs[keyVersion] = version
	}
	for _, d := range dirs {
		f.checks[prefixProbe+d] = true
	}
	return f
}

func runInstallFake(t *testing.T, f *fakeRunner, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := runInstall(docker.NewWithRunner(f), config.Config{}, args, &buf)
	return buf.String(), err
}

func TestRunInstallIngressFresh(t *testing.T) {
	f := haFake("2026.11.1")
	out, err := runInstallFake(t, f, "hass-ingress")
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if n := f.count(prefixIngressInstall); n != 1 {
		t.Errorf("ingress install exec'd %d times, want 1; calls %q", n, f.calls)
	}
	if n := f.count(keyRestart); n != 1 {
		t.Errorf("restart called %d times, want 1", n)
	}
	if !strings.Contains(out, "Restarting Home Assistant to load hass-ingress") {
		t.Errorf("output lacks the restart line:\n%s", out)
	}
	if !strings.HasSuffix(out, installRegistry["hass-ingress"].Activation) {
		t.Errorf("output does not end with the Activation text:\n%s", out)
	}
}

func TestRunInstallIngressNoRestart(t *testing.T) {
	f := haFake("2026.11.1")
	out, err := runInstallFake(t, f, "hass-ingress", "--no-restart")
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if n := f.count(prefixIngressInstall); n != 1 {
		t.Errorf("ingress install exec'd %d times, want 1", n)
	}
	if n := f.count(keyRestart); n != 0 {
		t.Errorf("restart called %d times, want 0", n)
	}
	if !strings.HasSuffix(out, installRegistry["hass-ingress"].Activation) {
		t.Errorf("output does not end with the Activation text:\n%s", out)
	}
}

func TestRunInstallIngressAlreadyInstalled(t *testing.T) {
	f := haFake("2026.11.1", "/config/custom_components/ingress")
	out, err := runInstallFake(t, f, "hass-ingress")
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if n := f.count(prefixIngressInstall) + f.count(keyRestart); n != 0 {
		t.Errorf("install/restart ran %d times, want 0; calls %q", n, f.calls)
	}
	if !strings.Contains(out, "hass-ingress is already installed") {
		t.Errorf("output lacks the already-installed line:\n%s", out)
	}
}
