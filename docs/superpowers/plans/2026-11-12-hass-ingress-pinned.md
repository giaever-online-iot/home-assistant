# `install hass-ingress`: pinned 1.4.0 release, Marketplace pointer, YAML-mode check — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `home-assistant.install hass-ingress` installs the pinned hass_ingress 1.4.0 release zip. It verifies size and sha256 before touching `/config`, never silently downgrades a newer copy, and points at Settings → Marketplace on HA 2026.11+. `home-assistant.ingress sync` warns when hass_ingress is not in YAML mode.

**Architecture:** A new `cmd/launcher/hassingress.go` holds the pin constants, a bash install script built from six segments with shell-quoted placeholders (D2–D4, D9), version parsing, and `hassIngressGate`. The gate reads `.HA_VERSION` and the installed `manifest.json` through `docker.Client.ReadFile` (D5, D7, D8). `Recipe.Gate` gains `force bool` (D6). A pure parser in `internal/ingress/mode.go` reads `core.config_entries` (D10, D13). A thin wrapper, `warnIngressMode` in `cmd/launcher/ingressmode.go`, runs it from `ingress sync` now, and from PR #6's `applyIngress` after the merge-in (D11, D12).

**Tech Stack:** Go 1.23 module (local toolchain go1.27.4), stdlib only (`archive/zip`, `net/http/httptest` and `os/exec` in tests). The install script is bash, run with busybox applets in the HA image and with GNU tools on the host in the script test.

**Spec:** `docs/superpowers/specs/2026-11-12-hass-ingress-pinned-design.md` at f2430c8 (D1–D13, T1–T8, M1–M10, A0–A5). Read it before Task 1.

## Global Constraints

- Branch `feat/hass-ingress-pinned`, built on W-5's `feat/hacs-marketplace` (PR #9). Work in the worktree `.claude/worktrees/hass-ingress-pinned`. Do not push.
- Pin values, verbatim (spec, *Pin constants*): version `1.4.0`; URL `https://github.com/lovelylain/hass_ingress/releases/download/1.4.0/ingress.zip`; size `61359`; sha256 `7b2255fa3f25b97d465c17336cae8188bcc4dfefd52677682f084c1e39a54cac`. Re-checked against the live asset while this plan was written (`HASS_INGRESS_PIN_ONLINE=1`, PASS).
- Nothing under `/config` is written until download, size, sha256 and unzip have all passed (D2). The staging dir is `/config/.snap-hass-ingress.XXXXXX`, never inside `custom_components` (D3).
- The launcher never calls the Marketplace API and never accepts its risk notice (W-12 decision 1). It only reads `.storage/core.config_entries` and never writes it (D10).
- `--force` installs over unknown, older or equal copies and **refuses over a newer one with exit 0** (D5). Plain `install` never changes an existing copy (D8). `hacsGate` ignores `force` (W-5 D5 stays true).
- The mode check never fails the sync (D11). It runs only when at least one panel is configured.
- User-facing text is copied verbatim from the spec's *Messages* and *YAML-mode check* sections. Texts that name the pin are built from `hassIngressPinVersion`.
- `internal/docker`, `snap/snapcraft.yaml` and `README.md` do not change (spec, OUT).
- On this branch, `cmd/launcher/ingress.go` gets only the mode-check block in `sync` (Task 7). Everything else is in new files, so the merge-in after PR #6 is a move into PR #6's shape (D12, Task 9).
- Before every commit, run `gofmt -w` on the Go files the task touched. `internal/ingress/ingress_test.go` is already unformatted on this branch's base. Leave it alone; PR #6 formats it.
- Package tests: `go test ./cmd/launcher/` or `go test ./internal/ingress/`. Run the full suite only under the lock: `flock -w 3600 /tmp/claude-1000/chim-suites.lock go test ./...`.

## Spec gaps (flagged to the orchestrator; the plan's choice is marked so it can be reversed)

- **G1. "One call line" in `sync` is a three-line `if`.** The spec says `ingress.go` gets one call line, but it also says the check runs only with at least one panel, and `warnIngressMode(cli, w)` takes no config. **Plan choice:** `if len(cfg.Ingress) > 0 { warnIngressMode(cli, os.Stderr) }` at the call site (Task 7). It conflicts with PR #6 either way (see G3).
- **G2. T7's fake has `custom_components/ingress` present.** PR #6's `applyIngress` checks the mode only when that dir exists (spec, *Where it runs*). **Plan choice:** T7 sets the dir from the start, so the same test holds before and after the merge-in (Tasks 7 and 9).
- **G3. PR #6 has landed since the spec was approved, so Task 9 can run now.** PR #6 merged into `main` as fca7f12 (2026-11-13 18:17Z), and W-5's pushed head `origin/feat/hacs-marketplace` (6b9c94f) has merged that `main` in. Tasks 1–7 merged against each of `origin/main`, `origin/feat/addons` and `origin/feat/hacs-marketplace` (dry run, `git merge-tree`) conflict in `cmd/launcher/ingress.go` only, as D12 expects. **Plan choice:** Task 9 stays a separate, gated task. Whether it runs straight after Task 8 or waits for PR #9 to land is the orchestrator's call. A second risk remains: if PR #9 is later **squash**-merged, merging `main` in again will conflict in `install.go`, `install_test.go` and `marketplace.go`, because our edits sit on W-5's lines. PR #7 and PR #8 were merge commits.
- **G4. CI must have `wget` and `unzip`.** T4 fails in CI (rather than skipping) if a tool is missing, which is the spec's choice. ubuntu-latest is expected to have both. Nobody verified that in this session; the first CI run of the branch proves it.
- **G5. The gate passes on `ReadFile` errors** the way `hacsGate` does, although `ReadFile` never errors today. The spec says the gate never returns an error, and the behaviour is the same.

**Pre-check (done while writing this plan, 2026-11-13):** every code block below was applied to a throwaway copy of f2430c8 in the scratchpad, outside the repo, in task order. Each task's test failed as its "verify it fails" step says, and passed after the implementation step. `gofmt -l` (apart from the known file), `go vet ./...` and `go build ./...` were clean. The full suite passed under the lock. M1–M10, with the exact edits in Task 8, each turned the listed tests red and reverted clean. Task 9 was dry-run on a shared clone, exactly as written: merging `origin/main` (fca7f12) gave the single `ingress.go` conflict. With `main`'s file taken, T7 and T8 went red. With the Task 9 edit they passed, `gofmt -l` was empty, the full suite passed, and M1–M10 (with M7b) each went red and reverted clean. That pre-check is no substitute for Tasks 8 and 9.

## Review Focus

1. **A config path with a space or a single quote** must not split the script's words. Pinned by s7 in `TestHassIngressScriptInstalls` (Task 2). Production uses `/config`, but the quoting is what keeps a future path safe.
2. **A leftover `.snap-hass-ingress.*` from an interrupted run** (the launcher killed mid-swap) must not block the next install, and is not ours to delete. Pinned by s8 (Task 2).
3. **A manifest version with whitespace, or written as a JSON number** (`" 1.4.0\n"`, `"version": 1.4`). The first is equal to the pin. The second is unknown and proceeds, without panicking. Pinned by a `TestParsePluginVersion` row (Task 1) and a `TestHassIngressGate` row (Task 4).
4. **`core.config_entries` with other domains in UI mode**, or an ingress entry whose `mode` is `null`. Another domain's `ui` must not trigger the warning, and `null` is *unreadable*. Pinned by `TestEntryMode` rows (Task 6).
5. **`--force --no-restart` over an older copy** installs and does not restart. Pinned by case l of `TestRunInstallHassIngressPinned` (Task 5).

---

### Task 1: plugin version parsing (T1)

**Files:**
- Create: `cmd/launcher/hassingress.go`
- Test: `cmd/launcher/hassingress_test.go` (create)

**Interfaces:**
- Consumes: `allDigits(s string) bool` from `cmd/launcher/marketplace.go` (W-5).
- Produces: `func parsePluginVersion(raw string) (v [3]int, ok bool)` and `func comparePluginVersion(a, b [3]int) int` (−1, 0 or 1).

- [ ] **Step 1: Write the failing test.** Create `cmd/launcher/hassingress_test.go`:

```go
package main

import "testing"

func TestParsePluginVersion(t *testing.T) {
	cases := []struct {
		raw string
		v   [3]int
		ok  bool
	}{
		{"1.4.0", [3]int{1, 4, 0}, true},
		{"1.3.2", [3]int{1, 3, 2}, true},
		{"1.10.0", [3]int{1, 10, 0}, true},
		{"1.4", [3]int{1, 4, 0}, true},
		{"1.4.2", [3]int{1, 4, 2}, true},
		{"1.5.0b1", [3]int{1, 5, 0}, true},
		{" 1.4.0\n", [3]int{1, 4, 0}, true}, // Review Focus 3
		{"", [3]int{}, false},
		{"v1.4.0", [3]int{}, false},
		{"1", [3]int{}, false},
		{"1.x.0", [3]int{}, false},
		{"1.4.0.1", [3]int{}, false},
		{".4.0", [3]int{}, false},
		{"1.4.b1", [3]int{}, false}, // the last field must start with a digit
	}
	for _, tc := range cases {
		v, ok := parsePluginVersion(tc.raw)
		if v != tc.v || ok != tc.ok {
			t.Errorf("parsePluginVersion(%q) = (%v, %v), want (%v, %v)", tc.raw, v, ok, tc.v, tc.ok)
		}
	}
}

func TestComparePluginVersion(t *testing.T) {
	pin := [3]int{1, 4, 0}
	cases := []struct {
		raw  string
		want int // vs 1.4.0
	}{
		{"1.4.0", 0},
		{"1.3.2", -1},
		{"1.10.0", 1}, // numeric, not string (M5)
		{"1.4", 0},
		{"1.4.2", 1},
		{"1.5.0b1", 1},
		{"0.9.9", -1},
		{"2.0.0", 1},
	}
	for _, tc := range cases {
		v, ok := parsePluginVersion(tc.raw)
		if !ok {
			t.Fatalf("parsePluginVersion(%q) not ok", tc.raw)
		}
		if got := comparePluginVersion(v, pin); got != tc.want {
			t.Errorf("comparePluginVersion(%v, %v) = %d, want %d", v, pin, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run it and see it fail.**

Run: `go test ./cmd/launcher/ -run 'TestParsePluginVersion|TestComparePluginVersion'`
Expected: build failure, `undefined: parsePluginVersion` and `undefined: comparePluginVersion`.

- [ ] **Step 3: Implement.** Create `cmd/launcher/hassingress.go`:

```go
package main

import (
	"strconv"
	"strings"
)

// parsePluginVersion reads a custom integration's manifest version
// ("1.4.0", "1.4", "1.5.0b1"). There must be 2 or 3 dot-separated fields;
// every field but the last is all ASCII digits, the last starts with at least
// one digit and any non-digit suffix is dropped. A missing third field is 0.
func parsePluginVersion(raw string) (v [3]int, ok bool) {
	fields := strings.Split(strings.TrimSpace(raw), ".")
	if len(fields) < 2 || len(fields) > 3 {
		return [3]int{}, false
	}
	last := len(fields) - 1
	for i, f := range fields {
		if i == last {
			n := 0
			for n < len(f) && f[n] >= '0' && f[n] <= '9' {
				n++
			}
			f = f[:n]
		}
		if !allDigits(f) {
			return [3]int{}, false
		}
		x, err := strconv.Atoi(f)
		if err != nil { // overflow
			return [3]int{}, false
		}
		v[i] = x
	}
	return v, true
}

// comparePluginVersion compares numerically, field by field: -1, 0 or 1.
func comparePluginVersion(a, b [3]int) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}
```

- [ ] **Step 4: Run it and see it pass.**

Run: `go test ./cmd/launcher/ -run 'TestParsePluginVersion|TestComparePluginVersion' -v`
Expected: `--- PASS` for both, then `ok`.

- [ ] **Step 5: Commit.**

```bash
gofmt -w cmd/launcher/hassingress.go cmd/launcher/hassingress_test.go
git add cmd/launcher/hassingress.go cmd/launcher/hassingress_test.go
git commit -m "feat(install): parse and compare hass_ingress manifest versions (W-12)"
```

---

### Task 2: pin constants and the install script, run for real (T4, T5)

**Files:**
- Modify: `cmd/launcher/hassingress.go` (append)
- Test: `cmd/launcher/hassingress_script_test.go` (create)

**Interfaces:**
- Produces: the constants `hassIngressPinVersion`, `hassIngressPinURL`, `hassIngressPinSize`, `hassIngressPinSHA256`; `func shellQuote(s string) string`; `func hassIngressScript(configDir, tmpDir string) string` (production, called with `"/config", "/tmp"`); `func buildHassIngressScript(configDir, tmpDir, url string, size int64, sha256 string) string`. The script is six string constants, `hiHeader + hiDownload + hiSize + hiSHA + hiUnpack + hiSwap`, joined in that order in one `return` expression. Mutations M1–M3 (Task 8) edit that expression.
- Test helpers other tasks do not use: `requireScriptTools`, `makeZip`, `sha256Hex`, `serveZip`, `runScript`, `stagingDirs`, `checkNoResidue`, `readFile`, `writeOld`, `testManifest`.

- [ ] **Step 1: Write the failing tests.** Create `cmd/launcher/hassingress_script_test.go`:

```go
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// requireScriptTools skips when the host lacks a tool the script needs, except
// in CI (CI is set), where a skip would hide the test, so it fails instead.
func requireScriptTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "wget", "unzip", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s not found in CI: %v", tool, err)
			}
			t.Skipf("%s not found: %v", tool, err)
		}
	}
}

// makeZip builds a zip holding files (name → content), in name order.
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, files[n]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// serveZip serves body at /ingress.zip and 404 everywhere else.
func serveZip(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ingress.zip" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runScript runs the real install script with bash on the host and returns its
// stderr and error. no_proxy keeps a host proxy away from the local server.
func runScript(t *testing.T, configDir, tmpDir, url string, size int64, sha string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "-c", buildHassIngressScript(configDir, tmpDir, url, size, sha))
	cmd.Env = append(os.Environ(), "no_proxy=127.0.0.1,localhost", "NO_PROXY=127.0.0.1,localhost")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

func stagingDirs(t *testing.T, configDir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(configDir, ".snap-hass-ingress.*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// checkNoResidue: the run left no staging dir of its own and an empty tmpDir.
func checkNoResidue(t *testing.T, configDir, tmpDir string, stagingBefore []string) {
	t.Helper()
	if got := stagingDirs(t, configDir); strings.Join(got, ",") != strings.Join(stagingBefore, ",") {
		t.Errorf("staging dirs = %q, want %q", got, stagingBefore)
	}
	left, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("tmpDir not empty: %v", left)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

const testManifest = `{"domain": "ingress", "version": "9.9.9"}`

// writeOld creates configDir/custom_components/ingress/old.txt.
func writeOld(t *testing.T, configDir string) {
	t.Helper()
	dir := filepath.Join(configDir, "custom_components", "ingress")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// s1, s2, s7, s8: the script installs, replaces, quotes paths, and ignores a
// leftover staging dir from an interrupted run.
func TestHassIngressScriptInstalls(t *testing.T) {
	requireScriptTools(t)
	body := makeZip(t, map[string]string{"manifest.json": testManifest, "marker.txt": "new"})
	srv := serveZip(t, body)
	cases := []struct {
		name     string
		subdir   string // configDir below t.TempDir()
		old      bool   // an existing custom_components/ingress with old.txt
		leftover bool   // a .snap-hass-ingress.* from an interrupted run
	}{
		{"s1 fresh", "", false, false},
		{"s2 replace", "", true, false},
		{"s7 space and quote in the path", "my config's dir", true, false}, // Review Focus 1
		{"s8 leftover staging dir", "", true, true},                        // Review Focus 2
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configDir := filepath.Join(t.TempDir(), tc.subdir)
			if err := os.MkdirAll(configDir, 0o755); err != nil {
				t.Fatal(err)
			}
			tmpDir := t.TempDir()
			if tc.old {
				writeOld(t, configDir)
			}
			if tc.leftover {
				if err := os.Mkdir(filepath.Join(configDir, ".snap-hass-ingress.ABC123"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			before := stagingDirs(t, configDir)
			stderr, err := runScript(t, configDir, tmpDir, srv.URL+"/ingress.zip", int64(len(body)), sha256Hex(body))
			if err != nil {
				t.Fatalf("script: %v\nstderr: %s", err, stderr)
			}
			dest := filepath.Join(configDir, "custom_components", "ingress")
			if got := readFile(t, filepath.Join(dest, "manifest.json")); got != testManifest {
				t.Errorf("manifest.json = %q, want %q", got, testManifest)
			}
			if got := readFile(t, filepath.Join(dest, "marker.txt")); got != "new" {
				t.Errorf("marker.txt = %q, want new", got)
			}
			if _, err := os.Stat(filepath.Join(dest, "old.txt")); !os.IsNotExist(err) {
				t.Errorf("old.txt still there (err %v)", err)
			}
			checkNoResidue(t, configDir, tmpDir, before)
		})
	}
}

// s3-s6: every failed check exits non-zero and leaves the existing copy as it was.
func TestHassIngressScriptFailsClosed(t *testing.T) {
	requireScriptTools(t)
	body := makeZip(t, map[string]string{"manifest.json": testManifest, "marker.txt": "new"})
	wrapped := makeZip(t, map[string]string{"custom_components/ingress/manifest.json": testManifest})
	srv := serveZip(t, body)
	wsrv := serveZip(t, wrapped)
	cases := []struct {
		name, url string
		size      int64
		sha       string
		wantErr   string
	}{
		{"s3 wrong sha", srv.URL + "/ingress.zip", int64(len(body)), sha256Hex([]byte("other bytes")), "sha256 mismatch"},
		{"s4 wrong size", srv.URL + "/ingress.zip", int64(len(body)) + 1, sha256Hex(body), "size mismatch"},
		{"s5 http 404", srv.URL + "/missing.zip", int64(len(body)), sha256Hex(body), "download failed"},
		{"s6 manifest not at the root", wsrv.URL + "/ingress.zip", int64(len(wrapped)), sha256Hex(wrapped), "manifest.json not at the zip root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configDir, tmpDir := t.TempDir(), t.TempDir()
			writeOld(t, configDir)
			before := stagingDirs(t, configDir)
			stderr, err := runScript(t, configDir, tmpDir, tc.url, tc.size, tc.sha)
			if err == nil {
				t.Fatalf("script succeeded, want failure; stderr: %s", stderr)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.wantErr)
			}
			dest := filepath.Join(configDir, "custom_components", "ingress")
			if got := readFile(t, filepath.Join(dest, "old.txt")); got != "old" {
				t.Errorf("old.txt = %q, want old", got)
			}
			if _, err := os.Stat(filepath.Join(dest, "manifest.json")); !os.IsNotExist(err) {
				t.Errorf("manifest.json appeared in the existing copy (err %v)", err)
			}
			checkNoResidue(t, configDir, tmpDir, before)
		})
	}
}

// T5: run by hand when bumping the pin; CI never hits the network.
func TestHassIngressPinOnline(t *testing.T) {
	if os.Getenv("HASS_INGRESS_PIN_ONLINE") != "1" {
		t.Skip("set HASS_INGRESS_PIN_ONLINE=1 to download the pinned asset")
	}
	resp, err := http.Get(hassIngressPinURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", hassIngressPinURL, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != hassIngressPinSize {
		t.Errorf("size = %d, want %d", len(b), hassIngressPinSize)
	}
	if got := sha256Hex(b); got != hassIngressPinSHA256 {
		t.Errorf("sha256 = %s, want %s", got, hassIngressPinSHA256)
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./cmd/launcher/ -run 'TestHassIngressScript|TestHassIngressPinOnline'`
Expected: build failure, `undefined: buildHassIngressScript`, `undefined: hassIngressPinURL` and the like.

- [ ] **Step 3: Implement.** Append to `cmd/launcher/hassingress.go`. The imports from Task 1 (`strconv`, `strings`) already cover it:

```go
// The pinned hass_ingress release (D1). To bump the pin:
//  1. gh api repos/lovelylain/hass_ingress/releases/tags/<v> --jq '.assets[] | select(.name=="ingress.zip") | {size, digest, browser_download_url}'
//  2. Download the asset yourself; sha256sum and wc -c must match the API's digest and size.
//  3. unzip -l: manifest.json at the root, with "version": "<v>".
//  4. Diff the release's CONFIG_SCHEMA against what ingress.Render emits; check the README's minimum HA version.
//  5. Update these four constants. The texts are built from hassIngressPinVersion, so only test expectations change.
//  6. HASS_INGRESS_PIN_ONLINE=1 go test ./cmd/launcher -run TestHassIngressPinOnline
//  7. Run the device acceptance (A1-A5) on one box.
const (
	hassIngressPinVersion = "1.4.0"
	hassIngressPinURL     = "https://github.com/lovelylain/hass_ingress/releases/download/1.4.0/ingress.zip"
	hassIngressPinSize    = 61359
	hassIngressPinSHA256  = "7b2255fa3f25b97d465c17336cae8188bcc4dfefd52677682f084c1e39a54cac"
)

// The install script, in segments. @NAME@ placeholders are filled, shell-quoted,
// by buildHassIngressScript. Nothing under @CC@ is written before hiSwap (D2).
const (
	hiHeader = `set -eu
work=$(mktemp -d @WORK@)
stage=""
cleanup() { rm -rf "$work"; [ -z "$stage" ] || rm -rf "$stage"; }
trap cleanup EXIT
`
	hiDownload = `wget -qO "$work/ingress.zip" @URL@ || { echo "hass-ingress: download failed: "@URL@ >&2; exit 1; }
`
	hiSize = `size=$(wc -c < "$work/ingress.zip")
[ "$size" -eq @SIZE@ ] || { echo "hass-ingress: size mismatch: got $size bytes, want @SIZE@; nothing changed" >&2; exit 1; }
`
	hiSHA = `echo @SHA@"  $work/ingress.zip" | sha256sum -c - >/dev/null 2>&1 \
  || { echo "hass-ingress: sha256 mismatch for "@URL@"; nothing changed" >&2; exit 1; }
`
	hiUnpack = `mkdir "$work/ingress"
unzip -q "$work/ingress.zip" -d "$work/ingress"
[ -f "$work/ingress/manifest.json" ] || { echo "hass-ingress: manifest.json not at the zip root; nothing changed" >&2; exit 1; }
`
	hiSwap = `mkdir -p @CC@
stage=$(mktemp -d @STAGE@)
cp -r "$work/ingress" "$stage/new"
if [ -e @DEST@ ]; then
  mv @DEST@ "$stage/old"
fi
if ! mv "$stage/new" @DEST@; then
  [ ! -e "$stage/old" ] || mv "$stage/old" @DEST@
  echo "hass-ingress: could not move the new copy into place; previous copy restored" >&2; exit 1
fi
`
)

// shellQuote single-quotes s for bash, so a path with a space or a quote stays one word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// hassIngressScript is the production install script: the pinned release,
// verified in tmpDir, then swapped into configDir. It runs inside the HA
// container as `bash -c <script>`, with ("/config", "/tmp").
func hassIngressScript(configDir, tmpDir string) string {
	return buildHassIngressScript(configDir, tmpDir, hassIngressPinURL, hassIngressPinSize, hassIngressPinSHA256)
}

// buildHassIngressScript takes the source as arguments so the script test can
// run the real script against a local server (D9).
func buildHassIngressScript(configDir, tmpDir, url string, size int64, sha256 string) string {
	r := strings.NewReplacer(
		"@WORK@", shellQuote(tmpDir+"/hass-ingress.XXXXXX"),
		"@URL@", shellQuote(url),
		"@SIZE@", strconv.FormatInt(size, 10),
		"@SHA@", shellQuote(sha256),
		"@CC@", shellQuote(configDir+"/custom_components"),
		"@STAGE@", shellQuote(configDir+"/.snap-hass-ingress.XXXXXX"),
		"@DEST@", shellQuote(configDir+"/custom_components/ingress"),
	)
	return r.Replace(hiHeader + hiDownload + hiSize + hiSHA + hiUnpack + hiSwap)
}
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go test ./cmd/launcher/ -run 'TestHassIngressScript|TestHassIngressPinOnline' -v`
Expected: `--- PASS` for s1, s2, s7, s8, s3, s4, s5 and s6, and `--- SKIP: TestHassIngressPinOnline`. If a subtest is `SKIP`ped for a missing tool, install it (`wget`, `unzip`); this test must not skip in CI.

- [ ] **Step 5: Check the pin against the live asset (once, by hand).**

Run: `HASS_INGRESS_PIN_ONLINE=1 go test ./cmd/launcher/ -run TestHassIngressPinOnline -v`
Expected: `--- PASS: TestHassIngressPinOnline`. A failure means the constants are wrong: stop and report.

- [ ] **Step 6: Commit.**

```bash
gofmt -w cmd/launcher/hassingress.go cmd/launcher/hassingress_script_test.go
git add cmd/launcher/hassingress.go cmd/launcher/hassingress_script_test.go
git commit -m "feat(install): pinned hass_ingress 1.4.0 script, verified before /config is touched (W-12 D1-D4, D9)"
```

---

### Task 3: `Recipe.Gate` gets `--force` (D6)

**Files:**
- Modify: `cmd/launcher/install.go` (the `Gate` field and its comment; the `recipe.Gate(cli)` call in `runInstall`)
- Modify: `cmd/launcher/marketplace.go` (the `hacsGate` signature and comment)
- Test: `cmd/launcher/install_test.go`

**Interfaces:**
- Produces: `Gate func(c *docker.Client, force bool) (proceed bool, notice string, err error)`; `func hacsGate(c *docker.Client, _ bool) (bool, string, error)`. `runInstall` calls `recipe.Gate(cli, force)`.

- [ ] **Step 1: Update the tests.** In `cmd/launcher/install_test.go`:

(a) In `TestHACSGate`, replace the `for _, tc := range cases { … }` loop with:

```go
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
```

(b) In `TestRunInstallGateError`, change the `Gate:` line to:

```go
		Gate: func(*docker.Client, bool) (bool, string, error) { return false, "", errors.New("boom") },
```

(c) Append:

```go
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
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./cmd/launcher/`
Expected: build failure, `too many arguments in call to hacsGate`, and `cannot use func(*docker.Client, bool) … as func(c *docker.Client) …` for both registry literals.

- [ ] **Step 3: Implement.**

In `cmd/launcher/install.go`, replace the `Gate` field and its comment with:

```go
	// Gate, if set, runs after preflight and before the dir probe, and gets
	// --force (D6). proceed=false prints notice and exits 0 with nothing
	// installed; the gate alone decides what --force may override.
	Gate func(c *docker.Client, force bool) (proceed bool, notice string, err error)
```

and in `runInstall` change `proceed, notice, err := recipe.Gate(cli)` to `proceed, notice, err := recipe.Gate(cli, force)`.

In `cmd/launcher/marketplace.go`, replace `hacsGate`'s comment and signature line with:

```go
// hacsGate reads the running HA's version from the config volume (the same
// file get.hacs.xyz checks). 2026.11+ or unknown: guidance, do not proceed
// (D1, D3). Older: proceed with the frozen notice. It ignores force: --force
// never bypasses it (W-5 D5).
func hacsGate(c *docker.Client, _ bool) (bool, string, error) {
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go test ./cmd/launcher/ -run 'TestHACSGate|TestRunInstallGateError|TestRunInstallPassesForceToGate|TestRunInstallHACSMarketplace' -v`
Expected: `--- PASS` for all four. Then run `go test ./cmd/launcher/` and expect `ok`.

- [ ] **Step 5: Commit.**

```bash
gofmt -w cmd/launcher/install.go cmd/launcher/marketplace.go cmd/launcher/install_test.go
git add cmd/launcher/install.go cmd/launcher/marketplace.go cmd/launcher/install_test.go
git commit -m "refactor(install): Recipe.Gate receives --force; hacsGate ignores it (W-12 D6)"
```

---

### Task 4: `hassIngressGate` and its texts (T2)

**Files:**
- Modify: `cmd/launcher/hassingress.go` (import block; append)
- Test: `cmd/launcher/hassingress_test.go` (import block; append)

**Interfaces:**
- Consumes: `parseHAVersion`, `marketplaceBuiltIn`, `haVersionPath` (W-5); `parsePluginVersion`, `comparePluginVersion`, `hassIngressPinVersion` (Tasks 1–2); `haFake`, `fakeRunner` (W-5, `install_test.go`).
- Produces: `func hassIngressGate(c *docker.Client, force bool) (bool, string, error)`; `func joinNotice(status, pointer string) string`; the constants `hassIngressManifestPath`, `hassIngressMarketplacePointer`, `hassIngressOlder`, `hassIngressEqual`, `hassIngressNewer` and `hassIngressRefuse`. Test helpers for Task 5: `keyManifest`, `withManifest(f *fakeRunner, content string) *fakeRunner`, `manifestJSON(version string) string`, and `wantPointer`, `wantOlder132`, `wantEqual`, `wantNewer142`, `wantRefuse142`.

- [ ] **Step 1: Write the failing test.** In `cmd/launcher/hassingress_test.go`, replace `import "testing"` with:

```go
import (
	"errors"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
)
```

and append:

```go
const keyManifest = "exec homeassistant cat /config/custom_components/ingress/manifest.json"

// withManifest makes the fake's cat of the ingress manifest answer content;
// "" leaves the file missing (cat fails, as in production).
func withManifest(f *fakeRunner, content string) *fakeRunner {
	if content == "" {
		f.errs[keyManifest] = errors.New("exit status 1")
	} else {
		f.outputs[keyManifest] = content
	}
	return f
}

func manifestJSON(version string) string {
	return `{"domain": "ingress", "name": "Ingress", "version": "` + version + `"}`
}

const (
	wantPointer   = "To get updates, install Ingress from Home Assistant → Settings → Marketplace (2026.11 and later; accept the one-time risk notice first). It replaces this copy and keeps it up to date. If Home Assistant asks how to configure Ingress, choose YAML."
	wantOlder132  = `hass-ingress 1.3.2 is installed; this snap pins 1.4.0. Run "home-assistant.install hass-ingress --force" to upgrade.`
	wantEqual     = "hass-ingress 1.4.0 (the pinned version) is already installed."
	wantNewer142  = "hass-ingress 1.4.2 is installed, newer than the 1.4.0 this snap pins (probably from the Marketplace); leaving it alone."
	wantRefuse142 = "hass-ingress 1.4.2 is installed, newer than the 1.4.0 this snap pins; not downgrading. To force the pinned version, remove /config/custom_components/ingress first."
)

func TestHassIngressGate(t *testing.T) {
	cases := []struct {
		ha       string // .HA_VERSION; "" = missing
		manifest string // manifest.json; "" = missing
		force    bool
		proceed  bool
		notice   string
	}{
		{"2026.11.1", "", false, true, wantPointer},
		{"2026.11.1", "", true, true, wantPointer},
		{"2026.11.1", manifestJSON("1.3.2"), false, false, wantOlder132 + "\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.3.2"), true, true, wantPointer},
		{"2026.11.1", manifestJSON("1.4.0"), false, false, wantEqual + "\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.4.0"), true, true, wantPointer},
		{"2026.11.1", manifestJSON("1.4"), false, false, wantEqual + "\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.4.2"), false, false, wantNewer142 + "\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.4.2"), true, false, wantRefuse142 + "\n" + wantPointer}, // M4
		{"2026.11.1", manifestJSON("1.10.0"), true, false, "hass-ingress 1.10.0 is installed, newer than the 1.4.0 this snap pins; not downgrading. To force the pinned version, remove /config/custom_components/ingress first.\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.5.0b1"), false, false, "hass-ingress 1.5.0b1 is installed, newer than the 1.4.0 this snap pins (probably from the Marketplace); leaving it alone.\n" + wantPointer},
		{"2026.10.3", manifestJSON("1.3.2"), false, false, wantOlder132}, // no pointer below 2026.11 (M10)
		{"2026.10.3", manifestJSON("1.4.2"), true, false, wantRefuse142},
		{"2026.10.3", "", false, true, ""},
		{"", "", false, true, wantPointer}, // unknown HA version counts as 2026.11+ (D7)
		{"garbage", manifestJSON("1.4.0"), false, false, wantEqual + "\n" + wantPointer},
		{"2026.11.1", "not json", false, true, wantPointer},              // unknown installed version
		{"2026.11.1", `{"domain": "ingress"}`, false, true, wantPointer}, // no version
		{"2026.11.1", manifestJSON("v1.4.0"), false, true, wantPointer},  // unparsable version
		{"2026.11.1", `{"version": 1.4}`, false, true, wantPointer},      // Review Focus 3: a number, not a string
	}
	for _, tc := range cases {
		f := withManifest(haFake(tc.ha), tc.manifest)
		proceed, notice, err := hassIngressGate(docker.NewWithRunner(f), tc.force)
		if err != nil {
			t.Errorf("gate(ha %q, manifest %q, force %v) err = %v", tc.ha, tc.manifest, tc.force, err)
			continue
		}
		if proceed != tc.proceed || notice != tc.notice {
			t.Errorf("gate(ha %q, manifest %q, force %v) = (%v,\n%q)\nwant (%v,\n%q)",
				tc.ha, tc.manifest, tc.force, proceed, notice, tc.proceed, tc.notice)
		}
	}
}
```

- [ ] **Step 2: Run it and see it fail.**

Run: `go test ./cmd/launcher/ -run TestHassIngressGate`
Expected: build failure, `undefined: hassIngressGate`.

- [ ] **Step 3: Implement.** In `cmd/launcher/hassingress.go`, replace the import block with:

```go
import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
)
```

and append:

```go
const hassIngressManifestPath = "/config/custom_components/ingress/manifest.json"

const hassIngressMarketplacePointer = "To get updates, install Ingress from Home Assistant → Settings → Marketplace (2026.11 and later; accept the one-time risk notice first). It replaces this copy and keeps it up to date. If Home Assistant asks how to configure Ingress, choose YAML."

// Status lines; %s is the installed version.
const (
	hassIngressOlder  = `hass-ingress %s is installed; this snap pins ` + hassIngressPinVersion + `. Run "home-assistant.install hass-ingress --force" to upgrade.`
	hassIngressEqual  = "hass-ingress " + hassIngressPinVersion + " (the pinned version) is already installed."
	hassIngressNewer  = "hass-ingress %s is installed, newer than the " + hassIngressPinVersion + " this snap pins (probably from the Marketplace); leaving it alone."
	hassIngressRefuse = "hass-ingress %s is installed, newer than the " + hassIngressPinVersion + " this snap pins; not downgrading. To force the pinned version, remove /config/custom_components/ingress first."
)

// hassIngressGate decides whether `install hass-ingress` runs (D5, D7, D8). It
// puts the Marketplace pointer last on 2026.11+ or an unknown HA version. An
// unknown installed version proceeds, to the generic dir probe without --force.
func hassIngressGate(c *docker.Client, force bool) (bool, string, error) {
	haRaw, err := c.ReadFile(dockerargs.ContainerName, haVersionPath)
	if err != nil {
		return false, "", err
	}
	pointer := ""
	if year, month, ok := parseHAVersion(haRaw); !ok || marketplaceBuiltIn(year, month) {
		pointer = hassIngressMarketplacePointer
	}
	manifest, err := c.ReadFile(dockerargs.ContainerName, hassIngressManifestPath)
	if err != nil {
		return false, "", err
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal([]byte(manifest), &m) != nil {
		return true, pointer, nil
	}
	raw := strings.TrimSpace(m.Version)
	installed, ok := parsePluginVersion(raw)
	if !ok {
		return true, pointer, nil
	}
	pin, _ := parsePluginVersion(hassIngressPinVersion)
	switch cmp := comparePluginVersion(installed, pin); {
	case cmp > 0 && force:
		return false, joinNotice(fmt.Sprintf(hassIngressRefuse, raw), pointer), nil
	case force:
		return true, pointer, nil
	case cmp < 0:
		return false, joinNotice(fmt.Sprintf(hassIngressOlder, raw), pointer), nil
	case cmp == 0:
		return false, joinNotice(hassIngressEqual, pointer), nil
	default:
		return false, joinNotice(fmt.Sprintf(hassIngressNewer, raw), pointer), nil
	}
}

// joinNotice puts the status line first and the pointer, if any, after it.
func joinNotice(status, pointer string) string {
	if pointer == "" {
		return status
	}
	return status + "\n" + pointer
}
```

The `switch` order matters: newer + force must be refused before the generic `force` case (M4).

- [ ] **Step 4: Run it and see it pass.**

Run: `go test ./cmd/launcher/ -run TestHassIngressGate -v`
Expected: `--- PASS: TestHassIngressGate`. Then run `go test ./cmd/launcher/` and expect `ok`.

- [ ] **Step 5: Commit.**

```bash
gofmt -w cmd/launcher/hassingress.go cmd/launcher/hassingress_test.go
git add cmd/launcher/hassingress.go cmd/launcher/hassingress_test.go
git commit -m "feat(install): hass-ingress gate: version-aware, no silent downgrade, Marketplace pointer (W-12 D5, D7, D8)"
```

---

### Task 5: wire the hass-ingress recipe (T3 a–l)

**Files:**
- Modify: `cmd/launcher/install.go` (the `"hass-ingress"` registry entry)
- Test: `cmd/launcher/install_test.go`

**Interfaces:**
- Consumes: `hassIngressGate`, `hassIngressScript`, `hassIngressPinVersion`, `hassIngressPinURL`, `hassIngressPinSHA256` (Tasks 2, 4); `withManifest`, `manifestJSON`, `wantPointer`, `wantOlder132`, `wantEqual`, `wantNewer142`, `wantRefuse142` (Task 4).
- Produces: test constants `wantIngressInstalled`, `ingressRestartLine` and `ingressDir = "/config/custom_components/ingress"`. Task 7 uses `ingressDir`.

- [ ] **Step 1: Update the tests.** In `cmd/launcher/install_test.go`:

(a) Change the constant `prefixIngressInstall = "exec homeassistant bash -c set -e;"` to `prefixIngressInstall = "exec homeassistant bash -c set -eu"`.

(b) In `TestInstalledWording`, change the `"hass-ingress":` entry of `want` to `"hass-ingress": wantIngressInstalled,`.

(c) Delete `TestRunInstallIngressAlreadyInstalledWording` and its two-line comment ("Case g: …"). Its case is now T3 h, and its "never reads .HA_VERSION" assertion no longer holds, because hass-ingress has a gate (spec, *Existing tests that change*). Leave `TestRunInstallIngressFresh`, `TestRunInstallIngressNoRestart` and `TestRunInstallIngressAlreadyInstalled` as they are; they still hold.

(d) Append:

```go
const (
	wantIngressInstalled = `hass-ingress is already installed, but its version could not be read. Run "home-assistant.install hass-ingress --force" to replace it with the pinned 1.4.0.`
	ingressRestartLine   = "Restarting Home Assistant to load hass-ingress …\n"
	ingressDir           = "/config/custom_components/ingress"
)

// T3 a-j, l: what runInstall prints and runs for each installed version.
func TestRunInstallHassIngressPinned(t *testing.T) {
	act := installRegistry["hass-ingress"].Activation
	cases := []struct {
		name                  string
		ha, manifest          string // "" = the file is missing
		dir                   bool   // custom_components/ingress exists
		args                  []string
		out                   string // exact output
		probe, exec, restarts int
	}{
		{"a fresh", "2026.11.1", "", false, []string{"hass-ingress"}, wantPointer + "\n" + ingressRestartLine + act, 1, 1, 1},
		{"b older", "2026.11.1", manifestJSON("1.3.2"), true, []string{"hass-ingress"}, wantOlder132 + "\n" + wantPointer + "\n", 0, 0, 0},
		{"c older --force", "2026.11.1", manifestJSON("1.3.2"), true, []string{"hass-ingress", "--force"}, wantPointer + "\n" + ingressRestartLine + act, 0, 1, 1},
		{"d equal", "2026.11.1", manifestJSON("1.4.0"), true, []string{"hass-ingress"}, wantEqual + "\n" + wantPointer + "\n", 0, 0, 0},
		{"e equal --force", "2026.11.1", manifestJSON("1.4.0"), true, []string{"hass-ingress", "--force"}, wantPointer + "\n" + ingressRestartLine + act, 0, 1, 1},
		{"f newer", "2026.11.1", manifestJSON("1.4.2"), true, []string{"hass-ingress"}, wantNewer142 + "\n" + wantPointer + "\n", 0, 0, 0},
		{"g newer --force", "2026.11.1", manifestJSON("1.4.2"), true, []string{"hass-ingress", "--force"}, wantRefuse142 + "\n" + wantPointer + "\n", 0, 0, 0},
		{"h dir without manifest", "2026.11.1", "", true, []string{"hass-ingress"}, wantPointer + "\n" + wantIngressInstalled + "\n", 1, 0, 0},
		{"i fresh on 2026.10", "2026.10.3", "", false, []string{"hass-ingress"}, ingressRestartLine + act, 1, 1, 1},
		{"j fresh, HA version unknown", "", "", false, []string{"hass-ingress"}, wantPointer + "\n" + ingressRestartLine + act, 1, 1, 1},
		{"l older --force --no-restart", "2026.11.1", manifestJSON("1.3.2"), true, []string{"hass-ingress", "--force", "--no-restart"}, wantPointer + "\n" + act, 0, 1, 0}, // Review Focus 5
	}
	for _, tc := range cases {
		var dirs []string
		if tc.dir {
			dirs = []string{ingressDir}
		}
		f := withManifest(haFake(tc.ha, dirs...), tc.manifest)
		out, err := runInstallFake(t, f, tc.args...)
		if err != nil {
			t.Errorf("%s: runInstall: %v", tc.name, err)
			continue
		}
		if out != tc.out {
			t.Errorf("%s: output =\n%q\nwant\n%q", tc.name, out, tc.out)
		}
		if tc.ha == "2026.10.3" && strings.Contains(out, "Marketplace") {
			t.Errorf("%s: mentions the Marketplace below 2026.11", tc.name)
		}
		if p, e, r := f.count(prefixProbe), f.count(prefixIngressInstall), f.count(keyRestart); p != tc.probe || e != tc.exec || r != tc.restarts {
			t.Errorf("%s: probe/exec/restart = %d/%d/%d, want %d/%d/%d; calls %q", tc.name, p, e, r, tc.probe, tc.exec, tc.restarts, f.calls)
		}
	}
}

// T3 k: the exec'd script is the pinned one, and verifies before it names /config (D2).
func TestRunInstallHassIngressScriptOrder(t *testing.T) {
	f := haFake("2026.11.1")
	if _, err := runInstallFake(t, f, "hass-ingress"); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	const execPrefix = "exec homeassistant bash -c "
	var script string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefixIngressInstall) {
			script = strings.TrimPrefix(c, execPrefix)
		}
	}
	if script != hassIngressScript("/config", "/tmp") {
		t.Fatalf("exec'd script is not hassIngressScript(\"/config\", \"/tmp\"):\n%s", script)
	}
	for _, want := range []string{hassIngressPinURL, hassIngressPinSHA256} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	sha, cfg := strings.Index(script, "sha256sum -c"), strings.Index(script, "/config")
	if sha < 0 || cfg < 0 || sha > cfg {
		t.Errorf("sha256sum -c at %d, first /config at %d: the check must come first", sha, cfg)
	}
}

func TestHassIngressActivationWording(t *testing.T) {
	want := "\nhass_ingress 1.4.0 installed (pinned release, sha256 verified). Next:\n" +
		"  1. Define panels:  snap set home-assistant ingress.<name>.url=http://localhost:<port> ingress.<name>.title=\"<Title>\"\n" +
		"  2. Apply:          home-assistant.ingress sync\n" +
		"Each panel then appears in the Home Assistant sidebar.\n"
	if got := installRegistry["hass-ingress"].Activation; got != want {
		t.Errorf("Activation =\n%q\nwant\n%q", got, want)
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./cmd/launcher/`
Expected: `FAIL`, with `--- FAIL` for `TestRunInstallIngressFresh` and `TestRunInstallIngressNoRestart` (the exec prefix no longer matches), and for `TestInstalledWording`, `TestRunInstallHassIngressPinned`, `TestRunInstallHassIngressScriptOrder` and `TestHassIngressActivationWording`.

- [ ] **Step 3: Implement.** In `cmd/launcher/install.go`, replace the whole `"hass-ingress": { … },` entry with:

```go
	"hass-ingress": {
		Name: "hass-ingress",
		// The integration's dir (and HA domain) is `ingress`, not `hass_ingress`.
		Dest: "/config/custom_components/ingress",
		Gate: hassIngressGate,
		Install: func(c *docker.Client) error {
			// The pinned release, verified in the container's /tmp before
			// anything under /config changes (D1-D4).
			return c.Exec(dockerargs.ContainerName, "bash", "-c", hassIngressScript("/config", "/tmp"))
		},
		Activation: "\nhass_ingress " + hassIngressPinVersion + " installed (pinned release, sha256 verified). Next:\n" +
			"  1. Define panels:  snap set home-assistant ingress.<name>.url=http://localhost:<port> ingress.<name>.title=\"<Title>\"\n" +
			"  2. Apply:          home-assistant.ingress sync\n" +
			"Each panel then appears in the Home Assistant sidebar.\n",
		Installed: `hass-ingress is already installed, but its version could not be read. Run "home-assistant.install hass-ingress --force" to replace it with the pinned ` + hassIngressPinVersion + `.`,
	},
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go test ./cmd/launcher/`
Expected: `ok`.

- [ ] **Step 5: Commit.**

```bash
gofmt -w cmd/launcher/install.go cmd/launcher/install_test.go
git add cmd/launcher/install.go cmd/launcher/install_test.go
git commit -m "feat(install): hass-ingress installs the pinned 1.4.0 release through its gate (W-12)"
```

---

### Task 6: the pure mode parser (T6)

**Files:**
- Create: `internal/ingress/mode.go`
- Test: `internal/ingress/mode_test.go` (create)

**Interfaces:**
- Produces: `const StoragePath = "/config/.storage/core.config_entries"`; `type ModeStatus int` with `ModeNoFile`, `ModeNoEntry`, `ModeYAML`, `ModeOther` and `ModeUnreadable` (in that order, from `iota`); `func EntryMode(storage string) (ModeStatus, string)`; `func ModeWarning(status ModeStatus, mode string) string`.

- [ ] **Step 1: Write the failing test.** Create `internal/ingress/mode_test.go`:

```go
package ingress

import "testing"

// storage wraps entries in core.config_entries' outer shape.
func storage(entries string) string {
	return `{"version": 1, "minor_version": 5, "key": "core.config_entries", "data": {"entries": [` + entries + `]}}`
}

const (
	entryYAML  = `{"domain": "ingress", "data": {"mode": "yaml"}, "version": 2, "source": "import", "disabled_by": null}`
	entryUI    = `{"domain": "ingress", "data": {"mode": "ui"}, "version": 2, "source": "user", "disabled_by": null}`
	entryAgent = `{"domain": "ingress", "data": {"mode": "agent"}, "version": 2}`
	entryOther = `{"domain": "mqtt", "data": {"mode": "ui"}, "version": 1}`
)

func TestEntryMode(t *testing.T) {
	cases := []struct {
		name    string
		storage string
		status  ModeStatus
		mode    string
	}{
		{"absent", "", ModeNoFile, ""},
		{"blank", " \n", ModeNoFile, ""},
		{"not json", "not json", ModeUnreadable, ""},
		{"no entries", `{"data":{"entries":[]}}`, ModeNoEntry, ""},
		{"yaml", storage(entryYAML), ModeYAML, "yaml"},
		{"ui", storage(entryUI), ModeOther, "ui"},
		{"agent", storage(entryAgent), ModeOther, "agent"},
		{"no mode", storage(`{"domain": "ingress", "data": {}}`), ModeUnreadable, ""},
		{"mode null", storage(`{"domain": "ingress", "data": {"mode": null}}`), ModeUnreadable, ""},          // Review Focus 4
		{"another domain in ui mode only", storage(entryOther), ModeNoEntry, ""},                             // M9
		{"another domain in ui mode, ingress yaml", storage(entryOther + "," + entryYAML), ModeYAML, "yaml"}, // Review Focus 4
		{"two ingress entries, the second ui", storage(entryYAML + "," + entryUI), ModeOther, "ui"},
	}
	for _, tc := range cases {
		status, mode := EntryMode(tc.storage)
		if status != tc.status || mode != tc.mode {
			t.Errorf("%s: EntryMode = (%d, %q), want (%d, %q)", tc.name, status, mode, tc.status, tc.mode)
		}
	}
}

func TestModeWarning(t *testing.T) {
	cases := []struct {
		status ModeStatus
		mode   string
		want   string
	}{
		{ModeNoFile, "", ""},
		{ModeNoEntry, "", ""},
		{ModeYAML, "yaml", ""},
		{ModeOther, "ui", "warning: hass_ingress is in \"ui\" mode, so it ignores configuration.yaml and the snap's ingress panels will not appear.\n" +
			"  Switch it to YAML: Home Assistant → Settings → Devices & services → Ingress → ⋮ → Reconfigure → YAML.\n"},
		{ModeOther, "agent", "warning: hass_ingress is in \"agent\" mode, so it ignores configuration.yaml and the snap's ingress panels will not appear.\n" +
			"  Switch it to YAML: Home Assistant → Settings → Devices & services → Ingress → ⋮ → Reconfigure → YAML.\n"},
		{ModeUnreadable, "", "warning: could not read hass_ingress's configuration mode from /config/.storage/core.config_entries.\n" +
			"  If the panels do not appear, check that Ingress is in YAML mode (Settings → Devices & services → Ingress → ⋮ → Reconfigure).\n"},
	}
	for _, tc := range cases {
		if got := ModeWarning(tc.status, tc.mode); got != tc.want {
			t.Errorf("ModeWarning(%d, %q) =\n%q\nwant\n%q", tc.status, tc.mode, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run it and see it fail.**

Run: `go test ./internal/ingress/ -run 'TestEntryMode|TestModeWarning'`
Expected: build failure, `undefined: ModeStatus`, `undefined: EntryMode` and so on ("too many errors").

- [ ] **Step 3: Implement.** Create `internal/ingress/mode.go`:

```go
package ingress

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StoragePath is where Home Assistant keeps its config entries; hass_ingress's
// entry carries the mode (ui, agent or yaml) in data.mode.
const StoragePath = "/config/.storage/core.config_entries"

// ModeStatus is what EntryMode found about hass_ingress's config entry.
type ModeStatus int

const (
	ModeNoFile     ModeStatus = iota // storage absent/empty: fresh HA, nothing to check
	ModeNoEntry                      // no `ingress` entry: the import creates it on the next start
	ModeYAML                         // data.mode == "yaml": fine
	ModeOther                        // data.mode is anything else ("ui", "agent", …)
	ModeUnreadable                   // not JSON, or the entry has no string data.mode
)

// EntryMode reads hass_ingress's mode from core.config_entries. Only entries
// whose domain is "ingress" count; the first one that is not YAML decides.
func EntryMode(storage string) (ModeStatus, string) {
	if strings.TrimSpace(storage) == "" {
		return ModeNoFile, ""
	}
	var doc struct {
		Data struct {
			Entries []struct {
				Domain string         `json:"domain"`
				Data   map[string]any `json:"data"`
			} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(storage), &doc); err != nil {
		return ModeUnreadable, ""
	}
	found := false
	for _, e := range doc.Data.Entries {
		if e.Domain != "ingress" {
			continue
		}
		found = true
		mode, ok := e.Data["mode"].(string)
		if !ok {
			return ModeUnreadable, ""
		}
		if mode != "yaml" {
			return ModeOther, mode
		}
	}
	if !found {
		return ModeNoEntry, ""
	}
	return ModeYAML, "yaml"
}

// ModeWarning is the stderr text for a status, ending with a newline; "" when
// there is nothing to say (D11).
func ModeWarning(status ModeStatus, mode string) string {
	switch status {
	case ModeOther:
		return fmt.Sprintf("warning: hass_ingress is in %q mode, so it ignores configuration.yaml and the snap's ingress panels will not appear.\n", mode) +
			"  Switch it to YAML: Home Assistant → Settings → Devices & services → Ingress → ⋮ → Reconfigure → YAML.\n"
	case ModeUnreadable:
		return "warning: could not read hass_ingress's configuration mode from " + StoragePath + ".\n" +
			"  If the panels do not appear, check that Ingress is in YAML mode (Settings → Devices & services → Ingress → ⋮ → Reconfigure).\n"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run it and see it pass.**

Run: `go test ./internal/ingress/ -run 'TestEntryMode|TestModeWarning' -v`
Expected: `--- PASS` for both.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/ingress/mode.go internal/ingress/mode_test.go
git add internal/ingress/mode.go internal/ingress/mode_test.go
git commit -m "feat(ingress): read hass_ingress's config-entry mode from core.config_entries (W-12 D10, D13)"
```

---

### Task 7: `warnIngressMode` and the check in `ingress sync` (T7)

**Files:**
- Create: `cmd/launcher/ingressmode.go`
- Modify: `cmd/launcher/ingress.go` (the `sync` case: one `if` block after `preflightContainer`)
- Test: `cmd/launcher/ingressmode_test.go` (create)

**Interfaces:**
- Consumes: `ingress.StoragePath`, `ingress.EntryMode`, `ingress.ModeWarning` (Task 6); `haFake`, `keyRestart` (W-5); `ingressDir` (Task 5).
- Produces: `func warnIngressMode(cli *docker.Client, w io.Writer)`. Test constants for Task 9: `keyStorage`, `keyWriteIngress`, `storageYAML`, `storageUI`, and the helper `indexOfCall(calls []string, prefix string) int`.

- [ ] **Step 1: Write the failing tests.** Create `cmd/launcher/ingressmode_test.go`:

```go
package main

import (
	"bytes"
	"errors"
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
		if err := runIngress(docker.NewWithRunner(f), config.Config{Ingress: tc.ingress}, []string{"sync"}); err != nil {
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
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./cmd/launcher/ -run 'TestWarnIngressMode|TestIngressSyncChecksMode'`
Expected: build failure, `undefined: warnIngressMode`.

- [ ] **Step 3: Implement the wrapper.** Create `cmd/launcher/ingressmode.go`:

```go
package main

import (
	"fmt"
	"io"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
	"github.com/giaever-online-iot/home-assistant/internal/ingress"
)

// warnIngressMode warns when hass_ingress is not in YAML mode, where it would
// silently ignore the snap's panels (D10). It only reads, and never fails the
// caller: panels are cosmetic (D11).
func warnIngressMode(cli *docker.Client, w io.Writer) {
	storage, _ := cli.ReadFile(dockerargs.ContainerName, ingress.StoragePath)
	fmt.Fprint(w, ingress.ModeWarning(ingress.EntryMode(storage)))
}
```

- [ ] **Step 4: Run them and see T7 still fail.** This proves T7 tests the call site, not the wrapper.

Run: `go test ./cmd/launcher/ -run 'TestWarnIngressMode|TestIngressSyncChecksMode'`
Expected: `--- FAIL: TestIngressSyncChecksMode` with `one panel: storage read 0 times, want 1`. `TestWarnIngressMode` passes.

- [ ] **Step 5: Call it from `sync`.** In `cmd/launcher/ingress.go`, in `case "sync":`, insert directly after the `preflightContainer` block and before `cli.WriteFile(…ingressFilePath…)`:

```go
		if len(cfg.Ingress) > 0 {
			warnIngressMode(cli, os.Stderr)
		}
```

(`os` is already imported.)

- [ ] **Step 6: Run them and see them pass.**

Run: `go test ./cmd/launcher/ -run 'TestWarnIngressMode|TestIngressSyncChecksMode' -v`
Expected: `--- PASS` for both. Then run `go test ./cmd/launcher/` and expect `ok`.

- [ ] **Step 7: Commit.**

```bash
gofmt -w cmd/launcher/ingressmode.go cmd/launcher/ingressmode_test.go cmd/launcher/ingress.go
git add cmd/launcher/ingressmode.go cmd/launcher/ingressmode_test.go cmd/launcher/ingress.go
git commit -m "feat(ingress): sync warns when hass_ingress is not in YAML mode (W-12 D10-D12)"
```

---

### Task 8: final verification and mutations M1–M10

No new code. Every command's output goes into the task report.

- [ ] **Step 1: Static checks.**

Run: `gofmt -l cmd/ internal/` → expect exactly one line, `internal/ingress/ingress_test.go`. That file is unformatted on this branch's base and is out of scope. Do not reformat it. Any other file listed is a failure.
Run: `go vet ./...` → expect no output.
Run: `go build ./...` → expect no output.

- [ ] **Step 2: Full suite under the lock.**

Run: `flock -w 3600 /tmp/claude-1000/chim-suites.lock go test ./...`
Expected: an `ok` line for every package (`cmd/launcher`, `internal/backup`, `internal/config`, `internal/docker`, `internal/dockerargs`, `internal/ingress`, `internal/reconcile`), and no `FAIL`.

- [ ] **Step 3: Mutations.** For each one: make the single edit, run `go test ./cmd/launcher/ ./internal/ingress/`, confirm `FAIL` and the named tests in the output, then revert with `git checkout -- <file>` and confirm `git diff --exit-code` is clean. If a mutation stays green, stop and report it. Do not weaken a mutation to make it go red.

| # | File | Exact edit | Must go red |
|---|---|---|---|
| M1 | `cmd/launcher/hassingress.go` | in `buildHassIngressScript`'s `return`, `hiSize + hiSHA + hiUnpack` → `hiSize + "true\n" + hiUnpack` | `TestHassIngressScriptFailsClosed/s3_wrong_sha`, `TestRunInstallHassIngressScriptOrder` |
| M2 | `cmd/launcher/hassingress.go` | `hiDownload + hiSize + hiSHA` → `hiDownload + hiSHA` | `TestHassIngressScriptFailsClosed/s4_wrong_size` |
| M3 | `cmd/launcher/hassingress.go` | `hiSHA + hiUnpack + hiSwap` → `hiUnpack + hiSwap + hiSHA` (the swap runs before the sha check) | `TestHassIngressScriptFailsClosed/s3_wrong_sha` (`old.txt` gone), `TestRunInstallHassIngressScriptOrder` |
| M4 | `cmd/launcher/hassingress.go` | in `hassIngressGate`, delete the two lines `case cmp > 0 && force:` and the `return false, joinNotice(fmt.Sprintf(hassIngressRefuse, raw), pointer), nil` under it | `TestHassIngressGate` (1.4.2 and 1.10.0 with force), `TestRunInstallHassIngressPinned` case `g newer --force` |
| M5 | `cmd/launcher/hassingress.go` | insert `return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))` as the first line of `comparePluginVersion` | `TestComparePluginVersion` row 1.10.0 (`= -1, want 1`), `TestHassIngressGate` |
| M6 | `cmd/launcher/install.go` | `if recipe.Gate != nil {` → `if recipe.Gate != nil && !force {` | `TestRunInstallHassIngressPinned` case `g newer --force`, `TestRunInstallPassesForceToGate`, `TestRunInstallHACSMarketplace` (W-5 3b) |
| M7 | `cmd/launcher/ingress.go` | delete the three lines `if len(cfg.Ingress) > 0 { warnIngressMode(cli, os.Stderr) }` | `TestIngressSyncChecksMode` |
| M8 | `internal/ingress/mode.go` | `return ModeOther, mode` → `return ModeYAML, mode` | `TestEntryMode` (ui, agent, two ingress entries), `TestWarnIngressMode` (ui) |
| M9 | `internal/ingress/mode.go` | delete the three lines `if e.Domain != "ingress" { continue }` | `TestEntryMode` ("another domain in ui mode only", "another domain in ui mode, ingress yaml") |
| M10 | `cmd/launcher/hassingress.go` | `!ok \|\| marketplaceBuiltIn(year, month) {` → `!ok \|\| marketplaceBuiltIn(year, month) \|\| true {` | `TestRunInstallHassIngressPinned` case `i fresh on 2026.10` ("mentions the Marketplace below 2026.11"), `TestHassIngressGate` (2026.10.3 rows) |

- [ ] **Step 4: Mergeability with PR #6.**

Run: `git fetch origin && for r in origin/feat/addons origin/main origin/feat/hacs-marketplace; do echo "== $r"; git merge-tree --write-tree --name-only HEAD $r; echo "exit=$?"; done`
Expected, for each of the three: `exit=1`, with exactly one conflicted file listed, `cmd/launcher/ingress.go`, and `CONFLICT (content): Merge conflict in cmd/launcher/ingress.go`. That conflict is D12's, and Task 9 resolves it. Any **other** conflicted file means stop and report. Do not resolve it in this task.

- [ ] **Step 5: The branch diff stays in scope.**

Run: `git diff --stat origin/feat/hacs-marketplace...HEAD`
Expected: only `cmd/launcher/hassingress.go`, `cmd/launcher/hassingress_test.go`, `cmd/launcher/hassingress_script_test.go`, `cmd/launcher/install.go`, `cmd/launcher/install_test.go`, `cmd/launcher/marketplace.go` (comment and signature only), `cmd/launcher/ingressmode.go`, `cmd/launcher/ingressmode_test.go`, `cmd/launcher/ingress.go` (3 lines), `internal/ingress/mode.go`, `internal/ingress/mode_test.go`, and under `docs/superpowers/` the research doc, the spec and this plan. `git diff origin/feat/hacs-marketplace...HEAD -- internal/docker snap README.md` must be empty.

There is no commit in this task unless a step finds something to fix, and any fix goes back through the owning task's test first.

---

### Task 9 — after `main` (with PR #6) is merged in: move the mode check into `applyIngress` (D12, T8)

**Do this task only when the orchestrator says so**, as the "merge main IN" step, before this branch is merged out. Its precondition is that PR #6 is on `main`. That is already true (fca7f12, see G3). Whether to also wait for PR #9 is the orchestrator's call. Merge `origin/main`, never `feat/addons` directly.

**Files:**
- Modify: `cmd/launcher/ingress.go` (PR #6's shape: one `if` block inside `applyIngress`)
- Test: `cmd/launcher/ingressmode_test.go` (append)

**Interfaces:**
- Consumes: PR #6's `applyIngress(cli *docker.Client, cfg config.Config, force bool) error`, `ingress.MergedEntries(c config.Config) map[string]config.IngressSpec` and `ingress.ShouldSync` (on `main` since fca7f12); `warnIngressMode`, `keyStorage`, `keyWriteIngress`, `storageUI` (Task 7); `ingressDir` (Task 5).

- [ ] **Step 1: Merge `main` in and resolve.**

Run: `git fetch origin && git merge origin/main`
Expected: `CONFLICT (content): Merge conflict in cmd/launcher/ingress.go` and no other conflict. If any other file conflicts (see G3), stop and report (`git merge --abort`).
Resolve it by taking `main`'s file whole: `git checkout --theirs cmd/launcher/ingress.go && git add cmd/launcher/ingress.go`. PR #6's `runIngress` sync now calls `applyIngress(cli, cfg, true)`, and our three-line block from Task 7 is gone. We do not restructure PR #6's code (D12).
Run: `go build ./... && go vet ./...` → no output.

- [ ] **Step 2: Write the failing test.** Append to `cmd/launcher/ingressmode_test.go`:

```go
// T8: an unchanged auto-sync (ShouldSync says no) still checks the mode when
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
		if err := applyIngress(docker.NewWithRunner(f), cfg, false); err != nil {
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
```

- [ ] **Step 3: Run them and see them fail.**

Run: `go test ./cmd/launcher/ -run 'TestApplyIngressAutoSyncChecksMode|TestIngressSyncChecksMode'`
Expected: `--- FAIL: TestIngressSyncChecksMode` (`one panel: storage read 0 times, want 1`) and `--- FAIL: TestApplyIngressAutoSyncChecksMode` (`installed: storage read 0 times, want 1`).

- [ ] **Step 4: Implement.** In `applyIngress` in `cmd/launcher/ingress.go`, inside the `if len(entries) > 0 { … }` block, directly after PR #6's `if err == nil && !installed { … "hass_ingress is not installed" … }`, add:

```go
		if err == nil && installed {
			warnIngressMode(cli, os.Stderr)
		}
```

That puts it before the `if !force { … ShouldSync … }` early return, so an unchanged auto-sync still warns (D11). Nothing else in PR #6's function changes. `git diff origin/main -- cmd/launcher/ingress.go` must show exactly these three added lines.

- [ ] **Step 5: Run them and see them pass.**

Run: `go test ./cmd/launcher/ -run 'TestApplyIngressAutoSyncChecksMode|TestIngressSyncChecksMode|TestWarnIngressMode' -v`
Expected: `--- PASS` for all three.

- [ ] **Step 6: Commit, concluding the merge.** The resolution of D12's conflict *is* the move into `applyIngress`, so it lands in the merge commit.

```bash
gofmt -w cmd/launcher/ingress.go cmd/launcher/ingressmode_test.go
git add cmd/launcher/ingress.go cmd/launcher/ingressmode_test.go
git commit -m "Merge origin/main into feat/hass-ingress-pinned; mode check moves into applyIngress (W-12 D12, T8)"
git status --short   # expect no output
```

- [ ] **Step 7: Verify the merged branch.**

Run: `gofmt -l cmd/ internal/` → expect **no** output (PR #6 formats `internal/ingress/ingress_test.go`).
Run: `go vet ./... && go build ./...` → no output.
Run: `flock -w 3600 /tmp/claude-1000/chim-suites.lock go test ./...` → `ok` for every package (7 of them).
Run: `git diff origin/main -- cmd/launcher/ingress.go` → exactly the three lines added in Step 4.
Re-run M1–M10 from Task 8, with M7 now meaning: delete the three lines added in Step 4 → `TestIngressSyncChecksMode` and `TestApplyIngressAutoSyncChecksMode` red (M7b, spec T8). Revert each with `git checkout -- <file>` and confirm `git diff --exit-code` is clean.
No commit unless a step finds something to fix; a fix goes through a failing test first.

---

**Device acceptance (after deploy, with the user; not part of this plan's build):** the spec's A0–A5 on lav918 (HA 2026.11.1, hass_ingress 1.3.2, YAML mode). A6 is not run, by decision.
