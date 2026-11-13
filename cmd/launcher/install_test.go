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

func TestInstalledWording(t *testing.T) {
	want := map[string]string{
		"hacs":         "hacs is already installed. It updates from its own page in the Home Assistant UI, but gets no new releases. Use --force to reinstall.",
		"hass-ingress": `hass-ingress is already installed. This copy does not update itself: run "home-assistant.install hass-ingress --force" to refresh it from upstream.`,
	}
	if len(want) != len(installRegistry) {
		t.Errorf("installRegistry has %d recipes, want map covers %d; add the new recipe's Installed text to want", len(installRegistry), len(want))
	}
	for name, r := range installRegistry {
		if r.Installed == "" {
			t.Errorf("%s: Installed is empty; every recipe must set it (D7)", name)
		}
		if r.Installed != want[name] {
			t.Errorf("%s Installed = %q, want %q", name, r.Installed, want[name])
		}
	}
}

// Case g: the per-recipe text replaces the shared self-update claim, and a
// recipe without a Gate never reads .HA_VERSION.
func TestRunInstallIngressAlreadyInstalledWording(t *testing.T) {
	f := haFake("2026.11.1", "/config/custom_components/ingress")
	out, err := runInstallFake(t, f, "hass-ingress")
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if want := installRegistry["hass-ingress"].Installed + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if !strings.Contains(out, "--force") || strings.Contains(out, "self-update") {
		t.Errorf("output must point at --force and not claim a self-update: %q", out)
	}
	if n := f.count(keyVersion); n != 0 {
		t.Errorf(".HA_VERSION read %d times for hass-ingress, want 0", n)
	}
}

const wantGuidance2026_11_1 = "HACS is built into Home Assistant 2026.11 and later as the Marketplace (found 2026.11.1); nothing to install.\n" +
	"  1. Home Assistant → Settings → Marketplace (administrator accounts only).\n" +
	"  2. Accept the one-time risk notice. Until you do, nothing can be installed or updated.\n" +
	"An existing HACS install is migrated to the Marketplace automatically on the first start of 2026.11 or later."

const wantFrozen = "Note: HACS is frozen: it moved into Home Assistant core (the Marketplace) in 2026.11 and gets no further releases. Upgrading Home Assistant migrates it automatically."

const wantUnknownGarbage = `Could not read Home Assistant's version from /config/.HA_VERSION (got "garbage"); assuming 2026.11 or later.` + "\n" +
	"HACS is built into Home Assistant 2026.11 and later as the Marketplace; nothing to install.\n" +
	"  1. Home Assistant → Settings → Marketplace (administrator accounts only).\n" +
	"  2. Accept the one-time risk notice. Until you do, nothing can be installed or updated.\n" +
	"An existing HACS install is migrated to the Marketplace automatically on the first start of 2026.11 or later.\n" +
	`On an older Home Assistant, run the legacy installer by hand: docker exec homeassistant bash -c "wget -O - https://get.hacs.xyz | bash"`

func TestHACSGate(t *testing.T) {
	cases := []struct {
		version     string // "" = file missing
		proceed     bool
		notice      string // exact, when set
		noticeHasIn string // substring, when notice is unset
	}{
		{version: "2026.11.1", proceed: false, notice: wantGuidance2026_11_1},
		{version: "2027.1.0", proceed: false, noticeHasIn: "(found 2027.1.0); nothing to install."},
		{version: "2026.12.0b1", proceed: false, noticeHasIn: "(found 2026.12.0b1)"},
		{version: "2026.10.3", proceed: true, notice: wantFrozen},
		{version: "2026.9.0", proceed: true, notice: wantFrozen}, // Review Focus 3 / M6 (G2)
		{version: "2026.1.0", proceed: true, notice: wantFrozen},
		{version: "garbage", proceed: false, notice: wantUnknownGarbage},
		{version: "", proceed: false, noticeHasIn: `(got ""); assuming 2026.11 or later.`},
	}
	for _, tc := range cases {
		for _, force := range []bool{false, true} { // hacsGate ignores force (D6, W-5 D5)
			proceed, notice, err := hacsGate(docker.NewWithRunner(haFake(tc.version)), force)
			if err != nil {
				t.Errorf("hacsGate(%q, %v) err = %v", tc.version, force, err)
				continue
			}
			if proceed != tc.proceed {
				t.Errorf("hacsGate(%q, %v) proceed = %v, want %v", tc.version, force, proceed, tc.proceed)
			}
			if tc.notice != "" && notice != tc.notice {
				t.Errorf("hacsGate(%q, %v) notice =\n%s\nwant\n%s", tc.version, force, notice, tc.notice)
			}
			if tc.noticeHasIn != "" && !strings.Contains(notice, tc.noticeHasIn) {
				t.Errorf("hacsGate(%q, %v) notice lacks %q:\n%s", tc.version, force, tc.noticeHasIn, notice)
			}
		}
	}
}

// Cases a, b, c: on 2026.11+ nothing is probed, run or restarted, whatever
// --force says and whatever is left in custom_components/hacs.
func TestRunInstallHACSMarketplace(t *testing.T) {
	cases := []struct {
		name string
		args []string
		dirs []string
	}{
		{"a plain", []string{"hacs"}, nil},
		{"b --force", []string{"hacs", "--force"}, nil},
		{"c dir present", []string{"hacs"}, []string{"/config/custom_components/hacs"}},
	}
	for _, tc := range cases {
		f := haFake("2026.11.1", tc.dirs...)
		out, err := runInstallFake(t, f, tc.args...)
		if err != nil {
			t.Errorf("%s: runInstall: %v", tc.name, err)
			continue
		}
		if out != wantGuidance2026_11_1+"\n" {
			t.Errorf("%s: output =\n%s", tc.name, out)
		}
		if strings.Contains(out, "already installed") {
			t.Errorf("%s: says already installed", tc.name)
		}
		if n := f.count(prefixHACS) + f.count(keyRestart) + f.count(prefixProbe); n != 0 {
			t.Errorf("%s: exec/restart/probe ran %d times, want 0; calls %q", tc.name, n, f.calls)
		}
	}
}

// Case d: below 2026.11 the legacy path runs, with the frozen notice first.
func TestRunInstallHACSLegacy(t *testing.T) {
	f := haFake("2026.10.3")
	out, err := runInstallFake(t, f, "hacs")
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if !strings.HasPrefix(out, wantFrozen+"\n") {
		t.Errorf("output does not start with the frozen notice:\n%s", out)
	}
	if n := f.count(prefixHACS); n != 1 {
		t.Errorf("get.hacs.xyz exec'd %d times, want 1; calls %q", n, f.calls)
	}
	if n := f.count(keyRestart); n != 1 {
		t.Errorf("restart called %d times, want 1", n)
	}
	if !strings.HasSuffix(out, installRegistry["hacs"].Activation) {
		t.Errorf("output does not end with the Activation text:\n%s", out)
	}
}

// Case e: --no-restart still works on the legacy path.
func TestRunInstallHACSLegacyNoRestart(t *testing.T) {
	f := haFake("2026.10.3")
	if _, err := runInstallFake(t, f, "hacs", "--no-restart"); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if n := f.count(prefixHACS); n != 1 {
		t.Errorf("get.hacs.xyz exec'd %d times, want 1", n)
	}
	if n := f.count(keyRestart); n != 0 {
		t.Errorf("restart called %d times, want 0", n)
	}
}

// Below 2026.11 with HACS already present: frozen notice, then the hacs text.
func TestRunInstallHACSLegacyAlreadyInstalled(t *testing.T) {
	f := haFake("2026.10.3", "/config/custom_components/hacs")
	out, err := runInstallFake(t, f, "hacs")
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if want := wantFrozen + "\n" + installRegistry["hacs"].Installed + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if n := f.count(prefixHACS) + f.count(keyRestart); n != 0 {
		t.Errorf("exec/restart ran %d times, want 0", n)
	}
}

// Case f: a missing .HA_VERSION counts as 2026.11+ (D3).
func TestRunInstallHACSUnknownVersion(t *testing.T) {
	f := haFake("")
	out, err := runInstallFake(t, f, "hacs")
	if err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if !strings.HasPrefix(out, `Could not read Home Assistant's version from /config/.HA_VERSION (got ""); assuming 2026.11 or later.`+"\n") {
		t.Errorf("output lacks the could-not-read line:\n%s", out)
	}
	if n := f.count(prefixHACS) + f.count(keyRestart) + f.count(prefixProbe); n != 0 {
		t.Errorf("exec/restart/probe ran %d times, want 0; calls %q", n, f.calls)
	}
}

// Review Focus 5: a gate error is returned and nothing runs after it.
func TestRunInstallGateError(t *testing.T) {
	installRegistry["gate-err"] = Recipe{
		Name: "gate-err", Dest: "/config/custom_components/x", Installed: "x is already installed.",
		Gate: func(*docker.Client, bool) (bool, string, error) { return false, "", errors.New("boom") },
		Install: func(*docker.Client) error {
			t.Error("Install ran after a gate error")
			return nil
		},
	}
	t.Cleanup(func() { delete(installRegistry, "gate-err") })
	f := haFake("2026.11.1")
	if _, err := runInstallFake(t, f, "gate-err"); err == nil || err.Error() != "boom" {
		t.Errorf("err = %v, want boom", err)
	}
	if n := f.count(prefixProbe) + f.count(keyRestart); n != 0 {
		t.Errorf("probe/restart ran %d times, want 0", n)
	}
}

// D6: runInstall hands --force to the gate.
func TestRunInstallPassesForceToGate(t *testing.T) {
	var got []bool
	installRegistry["gate-force"] = Recipe{
		Name: "gate-force", Dest: "/config/custom_components/x", Installed: "x is already installed.",
		Gate: func(_ *docker.Client, force bool) (bool, string, error) {
			got = append(got, force)
			return false, "", nil
		},
		Install: func(*docker.Client) error {
			t.Error("Install ran after the gate said no")
			return nil
		},
	}
	t.Cleanup(func() { delete(installRegistry, "gate-force") })
	for _, args := range [][]string{{"gate-force"}, {"gate-force", "--force"}} {
		if _, err := runInstallFake(t, haFake("2026.11.1"), args...); err != nil {
			t.Fatalf("runInstall(%v): %v", args, err)
		}
	}
	if want := []bool{false, true}; !reflect.DeepEqual(got, want) {
		t.Errorf("gate saw force = %v, want %v", got, want)
	}
}

func TestHACSActivationWording(t *testing.T) {
	a := installRegistry["hacs"].Activation
	for _, keep := range []string{
		"  1. Home Assistant → Settings → Devices & services → Add integration → HACS.\n",
		"  2. Acknowledge the prompts, then complete the GitHub device authorization.\n",
	} {
		if !strings.Contains(a, keep) {
			t.Errorf("Activation lost step %q", keep)
		}
	}
	want := "HACS gets no further releases; upgrading Home Assistant to 2026.11 or later moves it to the built-in Marketplace.\n"
	if !strings.HasSuffix(a, want) || strings.Contains(a, "updates itself") {
		t.Errorf("Activation = %q, want it to end with %q", a, want)
	}
}
