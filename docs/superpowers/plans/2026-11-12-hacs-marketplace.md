# `install hacs` on HA 2026.11+ (Marketplace no-op) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On Home Assistant 2026.11 and later, `home-assistant.install hacs` prints Settings → Marketplace guidance and exits 0 with no download and no restart; below 2026.11 it keeps the legacy `get.hacs.xyz` install and adds a "frozen" notice.

**Architecture:** `Recipe` in `cmd/launcher/install.go` gains an optional `Gate` hook and a required `Installed` message (D7, D8). The `hacs` recipe's gate reads `/config/.HA_VERSION` through the existing `docker.Client.ReadFile` (D2), parses it with a pure function and checks a pure year/month threshold (D4). `runInstall` runs the gate after preflight and before the dir probe (D6) and writes to an `io.Writer` (D7) so the tests can assert exact text through a fake `docker.Runner`.

**Tech Stack:** Go 1.23 module (toolchain go1.27 locally), stdlib only; the existing `internal/docker` client (`NewWithRunner`), snapcraft YAML.

**Spec:** `docs/superpowers/specs/2026-11-12-hacs-marketplace-design.md` (commit cd166ff, D1–D10). Read it before Task 1.

## Global Constraints

- Branch `feat/hacs-marketplace`, which branches from `main` at 2af8ed9. Work in the worktree `.claude/worktrees/hacs-marketplace`. Do not push.
- It must stay mergeable with PR #6 (`feat/addons`). PR #6 edits `snap/snapcraft.yaml` in a different hunk (after line 28), adds an import and later functions in `cmd/launcher/main.go`, and does not touch `cmd/launcher/install.go`. Our only `main.go` edit is the one `runInstall(...)` call line. Do not touch any other line of `main.go`.
- `internal/docker` does not change (D2). `parseInstallArgs` does not change (D5, D9). `hacs` stays a registered target (D9).
- The threshold is `marketplaceYear = 2026`, `marketplaceMonth = 11`, compared numerically as year and month only (D4).
- A missing or unparsable version counts as 2026.11 or later: guidance, exit 0, nothing run (D3).
- `--force` never bypasses the gate. `--no-restart` stays accepted (D5).
- All tests live in `cmd/launcher/install_test.go` (spec, Testing). New non-test code for the version logic goes in a new file `cmd/launcher/marketplace.go`.
- User-facing text is copied verbatim from the spec. The only text this plan adds is the version-less guidance line (see *Spec gaps*, G1).
- Before every commit, run `gofmt -w` on the Go files the task touched. Adding `Installed`/`Gate` to the registry entries re-aligns their keys.
- Run the package tests with `go test ./cmd/launcher/`. Run the full suite only under the lock: `flock -w 3600 /tmp/claude-1000/chim-suites.lock go test ./...`.

## Spec gaps (flagged to the orchestrator; the plan's choice is marked so it can be reversed)

- **G1. The "(found <version>)" clause in the unparsable case.** The spec prints the Marketplace guidance, which contains `(found <version>)`, after the "Could not read…" line, but there is no version to show. **Plan choice:** `marketplaceGuidance("")` leaves the clause out, so the line reads `HACS is built into Home Assistant 2026.11 and later as the Marketplace; nothing to install.` One function argument changes this if the orchestrator picks otherwise.
- **G2. M6 as specified cannot go red.** The spec says comparing the version as a string turns the `2026.1.0` row red. It does not: `"2026.1.0" >= "2026.11"` is false (`.` sorts before `1`), which is the right answer. String comparison only goes wrong for months 2–9 (`"2026.9.0" >= "2026.11"` is true). The `2026.1.0` row of `TestParseHAVersion` also tests the parser, which a string compare in the gate would not touch. **Plan choice:** add a `2026.9.0` row to the gate table (`TestHACSGate`) and point M6 at it (Task 7).
- **G3. The manual legacy command says `docker exec homeassistant …`.** On a snap host that runs Docker as the docker snap, a non-root user may need `sudo`. The plan keeps the spec's exact copy; this is noted only.

- **G4. `internal/ingress/ingress_test.go` is not gofmt-clean on `main`.** This is not ours. It is listed as a follow-up, and Task 7 expects it.

**Pre-check (done while writing this plan, 2026-11-12):** every code block was applied to a throwaway copy of cd166ff in the scratchpad, outside the repo. Each task's test failed as its Step 2 says and passed after Step 3. `go vet` and `go build` were clean, and M1–M6 (with the edits as written in Task 7) each turned the listed tests red. M2 and M3 also turn the legacy-path tests red, which is expected. That pre-check is no substitute for Task 7.

## Review Focus

1. **`.HA_VERSION` with CRLF or padding** (`"2026.11.1\r\n"`, `"  2026.10.3  "`) must parse. Pinned by extra rows in `TestParseHAVersion` (Task 1).
2. **Signs and odd digits:** `strconv.Atoi` alone accepts `+2026` and `-1`. `"+2026.11.0"` and `"2026.-1.0"` must be unparsable (digits-only rule). Pinned by extra rows in Task 1.
3. **Months 2–9 of 2026**, where a string compare would wrongly say "Marketplace". `2026.9.0` must take the legacy path. Pinned by the `2026.9.0` row in `TestHACSGate` (Task 5, G2).
4. **A leftover `custom_components/hacs` on 2026.11+** must give the guidance, not "already installed" (D6). Pinned by case c (Task 5).
5. **A gate error must stop the install**, with no probe and no `Install`. `ReadFile` never errors today, so nothing in the real recipes exercises it. Pinned by `TestRunInstallGateError` (Task 5), which uses a temporary registry entry.

---

### Task 1: `parseHAVersion`

**Files:**
- Create: `cmd/launcher/marketplace.go`
- Test: `cmd/launcher/install_test.go`

**Interfaces:**
- Produces: `func parseHAVersion(raw string) (year, month int, ok bool)`.

- [ ] **Step 1: Write the failing test.** Append it to `cmd/launcher/install_test.go`:

```go
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
```

- [ ] **Step 2: Run it and see it fail.**

Run: `go test ./cmd/launcher/ -run TestParseHAVersion`
Expected: build failure, `undefined: parseHAVersion`.

- [ ] **Step 3: Implement it in the new file `cmd/launcher/marketplace.go`:**

```go
package main

import (
	"strconv"
	"strings"
)

// parseHAVersion reads the year and month from a Home Assistant version, as
// written to /config/.HA_VERSION ("2026.11.1", "2026.12.0b1"). Fields 0 and 1
// must be non-empty ASCII digits; anything after field 1 is ignored.
func parseHAVersion(raw string) (year, month int, ok bool) {
	fields := strings.Split(strings.TrimSpace(raw), ".")
	if len(fields) < 2 || !allDigits(fields[0]) || !allDigits(fields[1]) {
		return 0, 0, false
	}
	year, errY := strconv.Atoi(fields[0])
	month, errM := strconv.Atoi(fields[1])
	if errY != nil || errM != nil { // overflow
		return 0, 0, false
	}
	return year, month, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run it and see it pass.**

Run: `go test ./cmd/launcher/ -run TestParseHAVersion -v`
Expected: `--- PASS: TestParseHAVersion`, then `ok`.

- [ ] **Step 5: Commit.**

```bash
git add cmd/launcher/marketplace.go cmd/launcher/install_test.go
git commit -m "feat(install): parse Home Assistant's year.month version (W-5)"
```

### Task 2: `marketplaceBuiltIn`

**Files:**
- Modify: `cmd/launcher/marketplace.go`
- Test: `cmd/launcher/install_test.go`

**Interfaces:**
- Produces: `const marketplaceYear = 2026`, `const marketplaceMonth = 11`, and `func marketplaceBuiltIn(year, month int) bool`.

- [ ] **Step 1: Write the failing test:**

```go
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
```

- [ ] **Step 2: Run it and see it fail.**

Run: `go test ./cmd/launcher/ -run TestMarketplaceBuiltIn`
Expected: build failure, `undefined: marketplaceBuiltIn`.

- [ ] **Step 3: Implement it.** Add to `marketplace.go`:

```go
// HACS moved into Home Assistant core as the Marketplace in 2026.11; on that
// version and later get.hacs.xyz refuses and the loader blocks the hacs domain.
const (
	marketplaceYear  = 2026
	marketplaceMonth = 11
)

func marketplaceBuiltIn(year, month int) bool {
	return year > marketplaceYear || (year == marketplaceYear && month >= marketplaceMonth)
}
```

- [ ] **Step 4: Run it and see it pass.**

Run: `go test ./cmd/launcher/ -run 'TestMarketplaceBuiltIn|TestParseHAVersion' -v`
Expected: both `--- PASS`, then `ok`.

- [ ] **Step 5: Commit.**

```bash
git add cmd/launcher/marketplace.go cmd/launcher/install_test.go
git commit -m "feat(install): marketplaceBuiltIn threshold at 2026.11 (W-5)"
```

### Task 3: fake docker runner + `runInstall` writes to an `io.Writer`

A refactor: existing behaviour is pinned by tests before the gate changes it.

**Files:**
- Modify: `cmd/launcher/install.go:106-144` (`runInstall`)
- Modify: `cmd/launcher/main.go:230` (the one call line)
- Test: `cmd/launcher/install_test.go`

**Interfaces:**
- Produces: `func runInstall(cli *docker.Client, cfg config.Config, args []string, out io.Writer) error`.
- Test helpers, used by Tasks 4 and 5: `type fakeRunner` (implements `docker.Runner`, with fields `outputs`, `errs`, `checks map[string]…` keyed by the args joined with spaces, and `calls []string`); `(*fakeRunner).count(prefix string) int`; `haFake(version string, dirs ...string) *fakeRunner`; `runInstallFake(t *testing.T, f *fakeRunner, args ...string) (string, error)`; and the call keys `keyRunning`, `keyVersion`, `prefixProbe`, `prefixHACS`, `prefixIngressInstall`, `keyRestart`.

The keys match exactly what `internal/docker/docker.go` sends: `Running` → `Output("inspect","-f","{{.State.Running}}",name)`; `ReadFile` → `Output("exec",name,"cat",path)` (any error → `""`, nil); `ExecCheck` → `Check("exec",name,cmd…)`; `Exec` → `Stream("exec",name,cmd…)`; `Restart` → `Stream("restart",name)`.

- [ ] **Step 1: Write the helpers and the failing tests.** Change the import block of `install_test.go` to:

```go
import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
)
```

Then append:

```go
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
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./cmd/launcher/ -run TestRunInstall`
Expected: build failure, `too many arguments in call to runInstall`.

- [ ] **Step 3: Implement it.** In `install.go`, add `"io"` to the imports and change the signature to `func runInstall(cli *docker.Client, cfg config.Config, args []string, out io.Writer) error`. Change the three writes: `fmt.Printf("%s is already installed …` → `fmt.Fprintf(out, …)` (same text for now), `fmt.Printf("Restarting …` → `fmt.Fprintf(out, …)`, and `fmt.Print(recipe.Activation)` → `fmt.Fprint(out, recipe.Activation)`. In `main.go`, change only the call line:

```go
		return runInstall(cli, cfg, os.Args[2:], os.Stdout)
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go test ./cmd/launcher/ -v -run 'TestRunInstall|TestParseInstallArgs|TestResolveTarget|TestInstallTargets'`
Expected: every test `--- PASS`, then `ok`. Also run `go build ./...` and expect no output.

- [ ] **Step 5: Commit.**

```bash
git add cmd/launcher/install.go cmd/launcher/install_test.go cmd/launcher/main.go
git commit -m "refactor(install): runInstall writes to an io.Writer; fake docker runner pins the flow (W-5)"
```

### Task 4: per-recipe `Installed` message (case g, D8)

**Files:**
- Modify: `cmd/launcher/install.go` (`Recipe`, both registry entries, the probe branch of `runInstall`)
- Test: `cmd/launcher/install_test.go`

**Interfaces:**
- Consumes: `fakeRunner`, `haFake`, `runInstallFake`, `keyVersion`, `prefixIngressInstall`, `keyRestart` (Task 3).
- Produces: `Recipe.Installed string`, which every recipe must set. `runInstall` prints it with `fmt.Fprintln(out, recipe.Installed)` when the probe finds `Dest`.

- [ ] **Step 1: Write the failing tests:**

```go
func TestInstalledWording(t *testing.T) {
	want := map[string]string{
		"hacs":         "hacs is already installed. It updates from its own page in the Home Assistant UI, but gets no new releases. Use --force to reinstall.",
		"hass-ingress": `hass-ingress is already installed. This copy does not update itself: run "home-assistant.install hass-ingress --force" to refresh it from upstream.`,
	}
	for name, r := range installRegistry {
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
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./cmd/launcher/ -run 'TestInstalledWording|TestRunInstallIngressAlreadyInstalledWording'`
Expected: build failure, `r.Installed undefined (type Recipe has no field or method Installed)`.

- [ ] **Step 3: Implement it.** Add the field to `Recipe`, after `Activation`:

```go
	Installed  string // printed when Dest already exists (no --force)
```

Set `Installed` on both registry entries to the exact strings in the test. Replace the probe branch's print with `fmt.Fprintln(out, recipe.Installed)`. This removes the shared `"(it self-updates from the Home Assistant UI)"` text.

- [ ] **Step 4: Run them and see them pass.**

Run: `go test ./cmd/launcher/ -v`
Expected: every test `--- PASS` (Task 3's `TestRunInstallIngressAlreadyInstalled` still passes, because the new text starts with `hass-ingress is already installed`), then `ok`.

- [ ] **Step 5: Commit.**

```bash
git add cmd/launcher/install.go cmd/launcher/install_test.go
git commit -m "fix(install): per-recipe already-installed text; hass-ingress does not self-update (W-5)"
```

### Task 5: `Recipe.Gate` and the hacs gate (cases a–f)

**Files:**
- Modify: `cmd/launcher/marketplace.go` (the gate and its copy)
- Modify: `cmd/launcher/install.go` (the `Recipe.Gate` field, `Gate: hacsGate` on the hacs entry, the gate step in `runInstall`)
- Test: `cmd/launcher/install_test.go`

**Interfaces:**
- Consumes: `parseHAVersion` (Task 1), `marketplaceBuiltIn` (Task 2), the Task 3 helpers, `Recipe.Installed` (Task 4).
- Produces: `Recipe.Gate func(c *docker.Client) (proceed bool, notice string, err error)` (nil means proceed with no notice); `func hacsGate(c *docker.Client) (bool, string, error)`; `const haVersionPath = "/config/.HA_VERSION"`; `const hacsFrozenNotice`; `func marketplaceGuidance(found string) string` (where `found == ""` leaves out the `(found …)` clause, per G1).

- [ ] **Step 1: Write the failing tests.** The expected text is written out literally rather than built from the functions under test:

```go
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
		proceed, notice, err := hacsGate(docker.NewWithRunner(haFake(tc.version)))
		if err != nil {
			t.Errorf("hacsGate(%q) err = %v", tc.version, err)
			continue
		}
		if proceed != tc.proceed {
			t.Errorf("hacsGate(%q) proceed = %v, want %v", tc.version, proceed, tc.proceed)
		}
		if tc.notice != "" && notice != tc.notice {
			t.Errorf("hacsGate(%q) notice =\n%s\nwant\n%s", tc.version, notice, tc.notice)
		}
		if tc.noticeHasIn != "" && !strings.Contains(notice, tc.noticeHasIn) {
			t.Errorf("hacsGate(%q) notice lacks %q:\n%s", tc.version, tc.noticeHasIn, notice)
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
		Gate: func(*docker.Client) (bool, string, error) { return false, "", errors.New("boom") },
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
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./cmd/launcher/ -run 'TestHACSGate|TestRunInstall'`
Expected: build failure, `undefined: hacsGate` and `unknown field Gate in struct literal of type Recipe`.

- [ ] **Step 3: Implement it.** Add `Gate` to `Recipe`:

```go
	// Gate, if set, runs after preflight and before the dir probe. proceed=false
	// prints notice and exits 0 with nothing installed, whatever --force says.
	Gate func(c *docker.Client) (proceed bool, notice string, err error)
```

Set `Gate: hacsGate,` on the `hacs` entry. In `runInstall`, insert this between the `preflightContainer` call and the `if !force {` probe (D6):

```go
	if recipe.Gate != nil {
		proceed, notice, err := recipe.Gate(cli)
		if err != nil {
			return err
		}
		if notice != "" {
			fmt.Fprintln(out, notice)
		}
		if !proceed {
			return nil
		}
	}
```

Add to `marketplace.go` (and add `"fmt"`, `internal/docker` and `internal/dockerargs` to its imports):

```go
const haVersionPath = "/config/.HA_VERSION"

const hacsFrozenNotice = "Note: HACS is frozen: it moved into Home Assistant core (the Marketplace) in 2026.11 and gets no further releases. Upgrading Home Assistant migrates it automatically."

const hacsLegacyByHand = `On an older Home Assistant, run the legacy installer by hand: docker exec homeassistant bash -c "wget -O - https://get.hacs.xyz | bash"`

// marketplaceGuidance tells the user where HACS went. found is the running
// version; "" (unknown) leaves out the "(found …)" clause.
func marketplaceGuidance(found string) string {
	clause := ""
	if found != "" {
		clause = " (found " + found + ")"
	}
	return "HACS is built into Home Assistant 2026.11 and later as the Marketplace" + clause + "; nothing to install.\n" +
		"  1. Home Assistant → Settings → Marketplace (administrator accounts only).\n" +
		"  2. Accept the one-time risk notice. Until you do, nothing can be installed or updated.\n" +
		"An existing HACS install is migrated to the Marketplace automatically on the first start of 2026.11 or later."
}

// hacsGate reads the running HA's version from the config volume (the same
// file get.hacs.xyz checks). 2026.11+ or unknown: guidance, do not proceed
// (D1, D3). Older: proceed with the frozen notice.
func hacsGate(c *docker.Client) (bool, string, error) {
	raw, err := c.ReadFile(dockerargs.ContainerName, haVersionPath)
	if err != nil {
		return false, "", err
	}
	v := strings.TrimSpace(raw)
	year, month, ok := parseHAVersion(v)
	if !ok {
		return false, fmt.Sprintf("Could not read Home Assistant's version from %s (got %q); assuming 2026.11 or later.\n", haVersionPath, v) +
			marketplaceGuidance("") + "\n" + hacsLegacyByHand, nil
	}
	if marketplaceBuiltIn(year, month) {
		return false, marketplaceGuidance(v), nil
	}
	return true, hacsFrozenNotice, nil
}
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go test ./cmd/launcher/ -v`
Expected: every test `--- PASS`, including `TestResolveTarget`, `TestInstallTargets` (still `[hacs hass-ingress]`, because the cleanup removes `gate-err`) and `TestParseInstallArgs`. Then `ok`.

- [ ] **Step 5: Commit.**

```bash
git add cmd/launcher/marketplace.go cmd/launcher/install.go cmd/launcher/install_test.go
git commit -m "feat(install): hacs is a Marketplace no-op on HA 2026.11+, frozen legacy install below (W-5)"
```

### Task 6: legacy Activation wording, snapcraft description, superseded note

**Files:**
- Modify: `cmd/launcher/install.go:34-37` (the hacs `Activation`)
- Modify: `snap/snapcraft.yaml:21-23`
- Modify: `docs/superpowers/specs/2026-06-28-install-subcommand-design.md` (a blockquote under line 1 only)
- Test: `cmd/launcher/install_test.go`

- [ ] **Step 1: Write the failing test:**

```go
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
```

- [ ] **Step 2: Run it and see it fail.**

Run: `go test ./cmd/launcher/ -run TestHACSActivationWording`
Expected: `--- FAIL: TestHACSActivationWording`, `Activation = "…HACS updates itself from the UI thereafter.\n", want it to end with …`.

- [ ] **Step 3: Implement it.**

In `install.go`, replace the line `"HACS updates itself from the UI thereafter.\n",` with:

```go
			"HACS gets no further releases; upgrading Home Assistant to 2026.11 or later moves it to the built-in Marketplace.\n",
```

In `snap/snapcraft.yaml`, replace lines 21–23, which are exactly:

```
  Install HACS (the Home Assistant Community Store):

    home-assistant.install hacs
```

with:

```
  HACS (the Home Assistant Community Store) is built into Home Assistant
  2026.11 and later as Settings → Marketplace. On older Home Assistant
  versions, `home-assistant.install hacs` still installs it.
```

In `docs/superpowers/specs/2026-06-28-install-subcommand-design.md`, insert this between line 1 (the title) and line 2 (blank), with a blank line before it. Nothing else in that file changes:

```markdown
> **Superseded in part (2026-11-12, W-5):** HACS moved into Home Assistant core
> as the Marketplace in 2026.11. `install hacs` is now a version-gated no-op on
> 2026.11 and later. See `2026-11-12-hacs-marketplace-design.md`.
```

- [ ] **Step 4: Run the checks and see them pass.**

Run: `go test ./cmd/launcher/ -v`
Expected: every test `--- PASS` (this includes the existing `snapcraft_test.go` guards, which parse `snap/snapcraft.yaml`).

Run: `python3 -c 'import yaml; d=yaml.safe_load(open("snap/snapcraft.yaml")); assert "Settings → Marketplace" in d["description"]; assert "Install HACS" not in d["description"]; print("yaml ok")'`
Expected: `yaml ok`.

Run: `git diff --stat HEAD -- docs/superpowers/specs/2026-06-28-install-subcommand-design.md snap/snapcraft.yaml`
Expected: the spec shows 4 insertions and no deletions; snapcraft.yaml shows 3 insertions and 3 deletions.

- [ ] **Step 5: Commit.**

```bash
git add cmd/launcher/install.go cmd/launcher/install_test.go snap/snapcraft.yaml docs/superpowers/specs/2026-06-28-install-subcommand-design.md
git commit -m "docs(install): HACS is the Marketplace on 2026.11+ — activation text, store description, superseded note (W-5)"
```

### Task 7: final verification and mutations M1–M6

No new code. Every command's output goes into the task report.

- [ ] **Step 1: Static checks.**

Run: `gofmt -l cmd/ internal/` → expect exactly one line, `internal/ingress/ingress_test.go`. That file is already unformatted on `main` (2af8ed9) and is out of scope (see G4): do not reformat it here. Any other file listed is a failure.
Run: `go vet ./...` → expect no output.
Run: `go build ./...` → expect no output.

- [ ] **Step 2: Full suite under the lock.**

Run: `flock -w 3600 /tmp/claude-1000/chim-suites.lock go test ./...`
Expected: an `ok` line for every package, and no `FAIL`.

- [ ] **Step 3: Mutations.** For each one: make the single edit, run `go test ./cmd/launcher/`, confirm `FAIL` and the named tests in the output, then revert with `git checkout -- <file>` and confirm `git diff --exit-code` is clean. If a mutation stays green, stop and report it. Do not weaken the mutation to make it go red.

| # | File | Exact edit | Must go red |
|---|---|---|---|
| M1 | `cmd/launcher/marketplace.go` | `month >= marketplaceMonth` → `month > marketplaceMonth` | `TestMarketplaceBuiltIn` (2026,11), `TestHACSGate` (2026.11.1), `TestRunInstallHACSMarketplace` |
| M2 | `cmd/launcher/install.go` | delete the whole `if recipe.Gate != nil { … }` block | `TestRunInstallHACSMarketplace` (a, b, c), `TestRunInstallHACSUnknownVersion` (f), `TestRunInstallGateError` |
| M3 | `cmd/launcher/install.go` | move the `if recipe.Gate != nil { … }` block to just after the `if !force { … }` probe block | `TestRunInstallHACSMarketplace` case c (also a and b: the probe is recorded) |
| M4 | `cmd/launcher/install.go` | `if recipe.Gate != nil {` → `if recipe.Gate != nil && !force {` | `TestRunInstallHACSMarketplace` case b |
| M5 | `cmd/launcher/marketplace.go` | in `hacsGate`'s `if !ok {` branch, `return false, fmt.Sprintf(` → `return true, fmt.Sprintf(` (proceed on an unknown version; it keeps `fmt` used, so it compiles) | `TestHACSGate` ("garbage", ""), `TestRunInstallHACSUnknownVersion` |
| M6 | `cmd/launcher/marketplace.go` | in `hacsGate`, `if marketplaceBuiltIn(year, month) {` → `if v >= "2026.11" && year >= 0 && month >= 0 {` (a string compare; the `>= 0` terms only keep `year`/`month` used so it compiles) | `TestHACSGate` row `2026.9.0` (see G2: the spec's `2026.1.0` row cannot catch this) |

- [ ] **Step 4: Mergeability with PR #6.**

Run: `git fetch origin && git merge-tree --write-tree HEAD origin/feat/addons >/dev/null && echo clean`
Expected: `clean`. A conflict means stop and report; do not resolve it on this branch.

- [ ] **Step 5: The branch diff stays in scope.**

Run: `git diff --stat origin/main...HEAD`
Expected: only `cmd/launcher/install.go`, `cmd/launcher/install_test.go`, `cmd/launcher/marketplace.go`, `cmd/launcher/main.go` (1 line), `snap/snapcraft.yaml`, the two specs, the research doc and this plan.

No commit in this task unless a step found something to fix, and any fix goes back through the owning task's test first.

---

**Device check (acceptance, after deploy; not part of this plan's build):** this follows the spec's *Device check* section, steps 1–3, on a box running HA 2026.11.x.
