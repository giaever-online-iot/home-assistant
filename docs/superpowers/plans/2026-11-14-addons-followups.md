# Phase 3 follow-ups (W-13): volumes, one HA start, model B localhost, empty maps, log replay, ufw — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the product problems found on lav918 in phase 3. Removing or recreating an add-on stops leaking anonymous volumes (with a warning first). A reconcile starts HA once. HA restarts only when the panels really changed. Model B panels that point at host services keep working. `snap get -d` keeps no empty maps. The journal shows each HA log line once. The ufw interaction is documented.

**Architecture:** Each fix lands in the package that already owns the behavior. `internal/docker` gains `rm -v`, `ImageVolumes`, `ReadFileIfExists`, `StartedAt` and a `--since` follow. `internal/reconcile` gains one optional `BeforeSwap` hook in the recreate branch. `internal/ingress` gets the pure `Decide` and the model B render rewrite. `internal/config` gets the loopback rewrite, the D12 warning, and empty-object pruning plus `EmptyPaths`. `cmd/launcher` splits `applyIngress` into a restart-free `syncIngressFiles`, a hardened `ensureInclude` and the pre-swap hook. It also gains the VOLUME warning, the log cursor, and a docker-free `prune-config` that the configure hook runs.

**Tech Stack:** Go 1.23 module (local toolchain go1.27.4), stdlib only. The configure hook is POSIX sh.

**Spec:** `docs/superpowers/specs/2026-11-14-addons-followups-design.md` at 89c9017 (D1–D19 with D4a and D8a, T1–T18 with T13a, M1–M15 with M12a and M12b, A1–A10). Read it before Task 1.

## Global Constraints

- Branch `fix/addons-followups`, based on `main` 088f717. Work only in the worktree `.claude/worktrees/addons-followups`. Do not push.
- **Model A must stay byte-identical.** Task 1's golden hashes and args never change after Task 1 (anti-goal: recreating a model A user's HA on upgrade). If a later task turns `TestGoldenModelA` red, the code is wrong, not the golden.
- Never lose a named volume (`ha-addon-<name>-data`, `homeassistant-config`) or a bind mount. Only `docker rm -v` deletes volumes, and it deletes anonymous ones only (D2).
- Cosmetic paths never fail anything. Panels, VOLUME warnings, pruning and the log cursor never fail a reconcile and never reject a `snap set`. `prune-config` runs as `|| true` (D15).
- `configuration.yaml` is never created and never written from a failed read (D8a). This holds from Task 4 onwards, and Tasks 6 and 8 widen that path only after it.
- `docker logs --since` never lies in the future (D16, the outer `min`).
- User-facing texts are copied verbatim from the spec: D3, D7, D8a (absent), D12 and the README content (D2, D3, D9, D11–D13, D17).
- Before every commit, run `gofmt -w` on the Go files the task touched. `gofmt -l .` is empty on the base.
- Package tests: `go test ./<pkg>/ -run '<names>'`. Run the full suite only under the lock: `flock -w 3600 /tmp/claude-1000/chim-suites.lock go test ./...`.
- Never touch lav918. Device acceptance (A1–A10) is the orchestrator's, after deploy.

## Spec gaps (flagged to the orchestrator; the plan's choice is marked so it can be reversed)

- **G1. D14 changes what `ingress: {}` means, and more existing tests than D19 names depend on it.** Today `"ingress":{}` means "panel wanted with defaults". Three test inputs in `derive_test.go` and two rows of `TestValidateAddonsRejectsBadInputs` use it that way. A third row uses `{"addons":{"x":{}}}` as "missing image", which is now no add-on at all. **Plan choice:** Task 11 changes those inputs to carry a real key (`"title"`, or a `ports` entry for the missing-image row) *before* the implementation. They pass on the old code and on the new, so the change does not weaken them. The README says a panel needs at least one `ingress.*` key. A user who set `addons.x.ingress='{}'` on purpose loses that panel. Nothing can set that from `snap set k.v=…` syntax, so the risk is small.
- **G2. The D12 per-panel line prints the whole rewritten URL.** The spec's template shows `http://host.docker.internal:<port>`. **Plan choice:** print `LoopbackToHostGateway`'s full output, so a URL with a path shows the path too (`…:3000/x`). For the plain `scheme://host:port` URLs the spec's example uses, it is identical.
- **G3. A stopped container reads as "absent" in D8a.** `docker exec` into a stopped container exits non-zero, just as `test -e` on a missing file does. `ReadFileIfExists` can't tell them apart without another docker call. **Plan choice:** accept it. Absent means a warning and no write, which is the safe side, and `applyIngress` preflights for a running HA anyway.
- **G4. `applyIngress`'s own messages go to one writer (stderr), not stdout.** That covers "Added `ingress: !include …`" and "Wrote N ingress panel(s); restarting Home Assistant…". The writer is needed so tests can see the D8/D8a warnings. **Plan choice:** all of them go to `w` (`os.Stderr` from `reconcileSet` and from `ingress sync`). The D7 "panels unchanged" line goes to stdout.
- **G5. The include line is now checked only when there is at least one panel (D8).** Before, it was checked after *any* write, including the write of `{}` when the last panel goes away, and `sync --force` with no panels. **Plan choice:** follow D8. A missing include next to an empty `{}` file changes nothing in HA.
- **G6. The D4 hook prints its `note:` line when HA is not running too, not only when a write fails.** That is the D4a/D5 case, and the note helps A3 tell a lost stop race apart from a D4 bug. The spec leaves the wording to the builder.
- **G7. T8's "whitespace-only differences" is read as whitespace at the ends of the file.** That matches today's trimmed comparison. A change of indentation inside the file changes YAML meaning, so it counts as a change.
- **G8. T7 (the create fallback) passes before Task 8.** It pins behavior that exists today. The behavioral red for Task 8 is T6, and M5 proves it.
- **G9. `Parse` now decodes with a `json.Decoder` (for `UseNumber`), which on its own would accept trailing data after the document.** **Plan addition:** `withoutEmptyObjects` rejects trailing data, `TestParseRejectsTrailingData` pins it, and an extra mutation, **M16** in Task 15, proves the test bites.
- **G10. Coverage compares `path.Clean`ed paths (T2).** So `data-dir=/mosquitto/data/` covers the VOLUME `/mosquitto/data`. Docker cleans mount destinations the same way. A prefix still covers nothing, so M2 holds.

**Pre-check (done while writing this plan, 2026-11-14):** every code block below was applied in task order to a throwaway worktree of 89c9017 in the scratchpad. Each task's test failed as its "see it fail" step says, then passed after the implementation step. `gofmt -l .` was empty, and `go vet ./...` and `go build ./...` were clean. Each of M1–M15 (with M12a, M12b and M16) turned its named tests red, and each reverted clean. The full-suite result under the lock is recorded in the report to the orchestrator. This pre-check is no substitute for Task 15.

## Review Focus

1. **A Pi whose clock comes up behind, so the cursor or HA's StartedAt lies in the future.** `--since` must still be ≤ the daemon's start, or the journal goes silent. Pinned by the "cursor in the future" and "HA started in the future" rows of `TestLogsSince` (Task 13).
2. **A mistyped `ingress sync` option** (`--frce`) must fail with a usage error before any docker call, never fall through to a restart. Pinned by `TestIngressSyncRejectsUnknownOption` (Task 6).
3. **HA stopped when the D4 hook runs** (the W-10 stop race won). The hook must write nothing and print exactly one `note:` line. Pinned by `TestPreSwapIngressStoppedHA` (Task 8).
4. **Model B panel URLs with a path or a query** (`https://localhost/panel?a=1`) keep both; a URL without a host is left alone. Pinned by rows of `TestLoopbackToHostGateway` (Task 10).
5. **Numbers, `false` and `""` survive the empty-object pass** (snapctl emits ports unquoted). Pinned by `TestParseKeepsNumbers` and the last row of `TestParseEmptyObjectsAreAbsent` (Task 11).

## Spec tests → tasks

| Spec test | Task | Test(s) |
|---|---|---|
| T1 | 2 | `TestRemoveDeletesAnonymousVolumes` |
| T2 | 3 | `TestUncoveredVolumes` |
| T3 | 3 | `TestWarnUncoveredVolumes` (+ `TestImageVolumes`) |
| T4 | 7 | `TestBeforeSwapRunsBetweenPullAndRemove` |
| T5 | 7 | `TestBeforeSwapOnlyOnRecreate` |
| T6 | 8 | `TestReconcileSetWritesIngressBeforeSwap` |
| T7 | 8 | `TestReconcileSetCreateFallsBackToRestart` |
| T8 | 5 | `TestDecide` |
| T9 | 6 | `TestIngressSyncCompareFirst` (+ the D19 `TestIngressSyncChecksMode`) |
| T10 | 1 | `TestGoldenModelA` |
| T11 | 9 | `TestModelBAddsHostGateway` |
| T12 | 10 | `TestLoopbackToHostGateway`, `TestMergedEntriesModelBLoopback` |
| T13 | 11 | `TestParseEmptyObjectsAreAbsent` |
| T13a | 4 | `TestEnsureIncludeHardenedRead` (+ `TestReadFileIfExists`) |
| T14 | 12 | `TestEmptyPaths` |
| T15 | 12 | `TestConfigureHookPrunesAfterValidate` |
| T16 | 13 | `TestLogsSince` |
| T17 | 13 | `TestFollowLogsSince`, `TestFollowHALogs` (+ `TestStartedAt`, `TestCursorFile`) |
| T18 | 10 | `TestValidateModelBWarning` |

**D19: existing tests that change on purpose.**
- `TestShouldSync` → `TestDecide` (Task 5). `ShouldSync` is removed.
- `TestIngressSyncChecksMode`: the restart is asserted on `sync --force`, and a no-force case asserts that an unchanged sync does not restart (Task 6).
- `TestApplyIngressAutoSyncChecksMode`: only the call changes (`_, err := applyIngress(…, false, io.Discard)`, Task 6), plus one comment word (Task 5).
- From G1 (Task 11): rows 1, 8 and 9 of `TestValidateAddonsRejectsBadInputs`; the two inputs of `TestDeriveEntriesModelAExplicitBindIP`; and the add-on input of `TestMergedEntries`.
- The shared `fakeRunner` in `install_test.go` learns that a write lands the file (Task 4). Existing tests are unaffected: none of them reads a path it wrote.

---

### Task 1: pin the model A golden (T10)

**Files:**
- Create: `internal/dockerargs/golden_test.go`

**Interfaces:**
- Produces: the test constants `goldenHashDefault`, `goldenHashRich`, `preD10HashModelB` and `richModelA`, and the helpers `cfg`/`join` from `dockerargs_test.go`, which Task 9 reuses.

The values below were computed on 088f717 (89c9017 adds only the spec) with the unchanged code. Run this task before any other change.

- [ ] **Step 1: Write the test.** Create `internal/dockerargs/golden_test.go`:

```go
package dockerargs

import (
	"reflect"
	"testing"
)

// Golden values computed on main 088f717, before any W-13 change (T10). A
// changed model A hash would recreate every model A user's Home Assistant on
// upgrade (W-13 anti-goal), so these must never be edited to make a test pass.
const (
	goldenHashDefault = "e64d29989689f9d174f148ce7e5cf8de2d0691be12160419c484ab4208847b3a"
	goldenHashRich    = "b6f4bd2862cab556c07f45dbe6bebbd6e7160833cf351aeda7c6ec09441f6496"
	// Model B's hash before D10; T11 checks that D10 moves it.
	preD10HashModelB = "2345565b3c8bd2eaf7132456b7a9bcf068659b37ce680a224d029c481bc8bbe8"

	richModelA = `{"image":{"digest":"sha256:abc"},"docker":{"bluetooth":false,"config-dir":"/srv/ha","timezone":"Europe/Oslo","devices":{"z":"/dev/ttyUSB0"},"volumes":{"m":"/mnt/media:/media"},"environment":{"FOO":"bar"},"extra-args":"--cap-add=NET_ADMIN"}}`
)

func TestGoldenModelA(t *testing.T) {
	cases := []struct {
		name, json, hash string
		args             []string
	}{
		{"default", `{}`, goldenHashDefault, []string{
			"run", "-d", "--name", "homeassistant", "--restart=unless-stopped",
			"--label", "io.giaever.home-assistant.spec-hash=" + goldenHashDefault,
			"--network=host", "--privileged",
			"-v", "homeassistant-config:/config", "-v", "/run/dbus:/run/dbus:ro",
			"ghcr.io/home-assistant/home-assistant:stable",
		}},
		{"rich", richModelA, goldenHashRich, []string{
			"run", "-d", "--name", "homeassistant", "--restart=unless-stopped",
			"--label", "io.giaever.home-assistant.spec-hash=" + goldenHashRich,
			"--network=host", "--privileged",
			"-v", "/srv/ha:/config", "-e", "TZ=Europe/Oslo", "--device=/dev/ttyUSB0",
			"-v", "/mnt/media:/media", "-e", "FOO=bar", "--cap-add=NET_ADMIN",
			"ghcr.io/home-assistant/home-assistant@sha256:abc",
		}},
	}
	for _, tc := range cases {
		if got := SpecHash(cfg(tc.json)); got != tc.hash {
			t.Errorf("%s: SpecHash = %s, want the golden %s", tc.name, got, tc.hash)
		}
		if got := BuildRunArgs(cfg(tc.json)); !reflect.DeepEqual(got, tc.args) {
			t.Errorf("%s: BuildRunArgs =\n %q\nwant the golden\n %q", tc.name, got, tc.args)
		}
	}
}
```

- [ ] **Step 2: Run it and see it pass on the unchanged code** (it is a characterization test).

Run: `go test ./internal/dockerargs/ -run TestGoldenModelA`
Expected: `ok`.

- [ ] **Step 3: Prove it bites.** Add one hash line temporarily, run, then revert:

```bash
sed -i 's|fmt.Fprintf(h, "ref=%s\\n", ImageRef(c))|fmt.Fprintf(h, "ref=%s\\n", ImageRef(c)); fmt.Fprintf(h, "x\\n")|' internal/dockerargs/dockerargs.go
go test ./internal/dockerargs/ -run TestGoldenModelA   # FAIL: default: SpecHash = 64a00ed7…, want the golden e64d2998…
git checkout internal/dockerargs/dockerargs.go
go test ./internal/dockerargs/ -run TestGoldenModelA   # ok
```

- [ ] **Step 4: Commit.**

```bash
git add internal/dockerargs/golden_test.go
git commit -m "test(dockerargs): pin model A SpecHash and run args as golden values (W-13 T10)"
```

---

### Task 2: `docker rm -f -v` (D2, T1)

**Files:**
- Modify: `internal/docker/docker.go:110`
- Test: `internal/docker/docker_test.go` (append)

**Interfaces:**
- Produces: `(*Client).Remove(name string) error`, which now runs `rm -f -v <name>`. It is the same signature, so all three callers (reconcile recreate, orphan removal, rollback) get `-v`.

- [ ] **Step 1: Write the failing test.** Append to `internal/docker/docker_test.go`:

```go
// T1: rm -v drops the container's anonymous volumes (D2); named volumes and
// bind mounts are never touched by -v.
func TestRemoveDeletesAnonymousVolumes(t *testing.T) {
	f := &fakeRunner{}
	if err := NewWithRunner(f).Remove("ha-addon-mqtt"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || strings.Join(f.calls[0], " ") != "rm -f -v ha-addon-mqtt" {
		t.Errorf("Remove called %q, want exactly [rm -f -v ha-addon-mqtt]", f.calls)
	}
}
```

- [ ] **Step 2: Run it and see it fail.**

Run: `go test ./internal/docker/ -run TestRemoveDeletesAnonymousVolumes`
Expected: `FAIL … Remove called [["rm" "-f" "ha-addon-mqtt"]], want exactly [rm -f -v ha-addon-mqtt]`

- [ ] **Step 3: Implement.** In `internal/docker/docker.go`, replace the `Remove` line with:

```go
func (c *Client) Remove(name string) error { return c.r.Stream("rm", "-f", "-v", name) }
```

- [ ] **Step 4: Run it and see it pass.**

Run: `go test ./internal/docker/ ./internal/reconcile/ ./cmd/launcher/`
Expected: `ok` ×3.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/docker/docker.go
git add internal/docker/
git commit -m "fix(docker): remove containers with -v so anonymous volumes do not leak (W-13 D2)"
```

---

### Task 3: warn about image VOLUMEs that config does not cover (D3, T2, T3)

**Files:**
- Modify: `internal/docker/docker.go` (imports; new method before `EnsureNetwork`)
- Create: `cmd/launcher/volumes.go`
- Modify: `cmd/launcher/main.go` (`reconcileSet`, after the result loop)
- Test: `internal/docker/docker_test.go` (append), `cmd/launcher/volumes_test.go` (create)

**Interfaces:**
- Consumes: `reconcile.Result`, `reconcile.ActionCreated`/`ActionRecreated`, `dockerargs.AddonContainerName`, and `haFake`/`fakeRunner.count` from `install_test.go`.
- Produces: `func (c *Client) ImageVolumes(image string) ([]string, error)` (sorted); `func uncoveredVolumes(imageVolumes []string, spec config.AddonSpec) []string` (sorted); `func warnUncoveredVolumes(cli *docker.Client, cfg config.Config, results []reconcile.Result, w io.Writer)`.

- [ ] **Step 1: Write the failing tests.** Append to `internal/docker/docker_test.go`:

```go
func TestImageVolumes(t *testing.T) {
	const key = "image inspect -f {{json .Config.Volumes}} "
	f := &fakeRunner{
		outputs: map[string]string{
			key + "eclipse-mosquitto:2": `{"/mosquitto/log":{},"/mosquitto/data":{}}`,
			key + "plain:1":             "null",
		},
		errs: map[string]error{key + "gone:1": errors.New("No such image")},
	}
	c := NewWithRunner(f)
	if got, err := c.ImageVolumes("eclipse-mosquitto:2"); err != nil || strings.Join(got, ",") != "/mosquitto/data,/mosquitto/log" {
		t.Errorf("mosquitto: %q, %v; want sorted [/mosquitto/data /mosquitto/log]", got, err)
	}
	if got, err := c.ImageVolumes("plain:1"); err != nil || len(got) != 0 {
		t.Errorf("no VOLUMEs (null): %q, %v; want none", got, err)
	}
	if _, err := c.ImageVolumes("gone:1"); err == nil {
		t.Error("an inspect failure must be returned")
	}
}
```

Create `cmd/launcher/volumes_test.go`:

```go
package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/reconcile"
)

// T2: only an exact data-dir or volumes.* destination covers an image VOLUME.
func TestUncoveredVolumes(t *testing.T) {
	mosq := []string{"/mosquitto/log", "/mosquitto/data"}
	cases := []struct {
		name string
		spec config.AddonSpec
		want string
	}{
		{"data-dir elsewhere", config.AddonSpec{DataDir: "/mosquitto/config"}, "/mosquitto/data,/mosquitto/log"},
		{"data-dir on one", config.AddonSpec{DataDir: "/mosquitto/data"}, "/mosquitto/log"},
		{"volumes entry with opts", config.AddonSpec{DataDir: "/mosquitto/data", Volumes: map[string]string{"x": "x:/mosquitto/log:ro"}}, ""},
		{"prefix covers nothing", config.AddonSpec{DataDir: "/mosquitto"}, "/mosquitto/data,/mosquitto/log"},
		{"trailing slash is the same path", config.AddonSpec{DataDir: "/mosquitto/data/"}, "/mosquitto/log"},
		{"nothing set", config.AddonSpec{}, "/mosquitto/data,/mosquitto/log"},
	}
	for _, tc := range cases {
		if got := strings.Join(uncoveredVolumes(mosq, tc.spec), ","); got != tc.want {
			t.Errorf("%s: uncovered = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := uncoveredVolumes(nil, config.AddonSpec{}); len(got) != 0 {
		t.Errorf("an image without VOLUMEs: %q, want none", got)
	}
}

// T3: the warning is checked only for add-ons that were just created or
// recreated; inspect errors are silent.
func TestWarnUncoveredVolumes(t *testing.T) {
	const key = "image inspect -f {{json .Config.Volumes}} "
	f := haFake("2026.11.1")
	for _, img := range []string{"a:1", "b:1", "c:1", "d:1"} {
		f.outputs[key+img] = `{"/data":{}}`
	}
	f.errs[key+"e:1"] = errors.New("No such image")
	cfg := config.Config{Addons: map[string]config.AddonSpec{
		"a": {Image: "a:1"}, "b": {Image: "b:1"}, "c": {Image: "c:1"}, "d": {Image: "d:1"}, "e": {Image: "e:1"},
	}}
	results := []reconcile.Result{
		{Name: "homeassistant", Action: reconcile.ActionCreated},
		{Name: "ha-addon-a", Action: reconcile.ActionCreated},
		{Name: "ha-addon-b", Action: reconcile.ActionRecreated},
		{Name: "ha-addon-c", Action: reconcile.ActionStarted},
		{Name: "ha-addon-d", Action: reconcile.ActionNone},
		{Name: "ha-addon-e", Action: reconcile.ActionCreated},
		{Name: "ha-addon-old", Action: reconcile.ActionRemoved},
	}
	var buf bytes.Buffer
	warnUncoveredVolumes(docker.NewWithRunner(f), cfg, results, &buf)
	want := "warning: addons.a: image a:1 declares VOLUME /data not covered by data-dir or volumes.*; their content is discarded when the add-on is recreated or removed\n" +
		"warning: addons.b: image b:1 declares VOLUME /data not covered by data-dir or volumes.*; their content is discarded when the add-on is recreated or removed\n"
	if buf.String() != want {
		t.Errorf("warnings =\n%s\nwant\n%s", buf.String(), want)
	}
	if n := f.count("image inspect"); n != 3 {
		t.Errorf("image inspect ran %d times, want 3 (a, b, e); calls %q", n, f.calls)
	}
	for _, img := range []string{"c:1", "d:1", "ghcr.io"} {
		if n := f.count(key + img); n != 0 {
			t.Errorf("%s was inspected; only created/recreated add-ons are", img)
		}
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./internal/docker/ -run TestImageVolumes; go test ./cmd/launcher/ -run 'TestUncoveredVolumes|TestWarnUncoveredVolumes'`
Expected: build failures, `c.ImageVolumes undefined (type *Client has no field or method ImageVolumes)`, then `undefined: uncoveredVolumes` and `undefined: warnUncoveredVolumes`.

- [ ] **Step 3: Implement.**

In `internal/docker/docker.go`, add `"encoding/json"` and `"sort"` to the imports. Insert this immediately before `// EnsureNetwork creates`:

```go
// ImageVolumes returns the VOLUME paths the image declares, sorted ("null",
// i.e. no VOLUMEs, gives none). Used to warn about anonymous volumes whose
// content `rm -v` discards (D3).
func (c *Client) ImageVolumes(image string) ([]string, error) {
	out, err := c.r.Output("image", "inspect", "-f", "{{json .Config.Volumes}}", image)
	if err != nil {
		return nil, err
	}
	var vols map[string]struct{}
	if err := json.Unmarshal([]byte(out), &vols); err != nil {
		return nil, fmt.Errorf("parsing the VOLUMEs of %s: %w", image, err)
	}
	paths := make([]string, 0, len(vols))
	for p := range vols {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}
```

Create `cmd/launcher/volumes.go`:

```go
package main

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
	"github.com/giaever-online-iot/home-assistant/internal/reconcile"
)

// uncoveredVolumes returns, sorted, the image VOLUME paths that neither
// data-dir nor the destination of a volumes.* entry covers (D3). Only the
// exact path counts: docker replaces a VOLUME's anonymous volume only with a
// mount at that same path, so a parent directory covers nothing.
func uncoveredVolumes(imageVolumes []string, spec config.AddonSpec) []string {
	covered := map[string]bool{}
	if spec.DataDir != "" {
		covered[path.Clean(spec.DataDir)] = true
	}
	for _, v := range spec.Volumes {
		// host:container[:opts] — the destination is the second field.
		if parts := strings.Split(v, ":"); len(parts) >= 2 {
			covered[path.Clean(parts[1])] = true
		}
	}
	var out []string
	for _, p := range imageVolumes {
		if !covered[path.Clean(p)] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// warnUncoveredVolumes warns, for each add-on this reconcile created or
// recreated, about image VOLUMEs outside data-dir and volumes.*: `rm -v`
// discards their content on the next recreate or removal (D2, D3). Inspect
// errors are silent, and the warning never fails the reconcile.
func warnUncoveredVolumes(cli *docker.Client, cfg config.Config, results []reconcile.Result, w io.Writer) {
	byContainer := make(map[string]string, len(cfg.Addons))
	for n := range cfg.Addons {
		byContainer[dockerargs.AddonContainerName(n)] = n
	}
	for _, r := range results {
		if r.Err != nil || (r.Action != reconcile.ActionCreated && r.Action != reconcile.ActionRecreated) {
			continue
		}
		name, ok := byContainer[r.Name]
		if !ok {
			continue // Home Assistant itself
		}
		a := cfg.Addons[name]
		vols, err := cli.ImageVolumes(a.Image)
		if err != nil {
			continue
		}
		if un := uncoveredVolumes(vols, a); len(un) > 0 {
			fmt.Fprintf(w, "warning: addons.%s: image %s declares VOLUME %s not covered by data-dir or volumes.*; their content is discarded when the add-on is recreated or removed\n",
				name, a.Image, strings.Join(un, ", "))
		}
	}
}
```

In `cmd/launcher/main.go` (`reconcileSet`), directly after the loop that prints `reconcile %s: %s`, add:

```go
	warnUncoveredVolumes(cli, cfg, results, os.Stderr)
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go vet ./... && go test ./internal/docker/ ./cmd/launcher/`
Expected: `ok` ×2.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/docker/ cmd/launcher/
git add internal/docker/ cmd/launcher/volumes.go cmd/launcher/volumes_test.go cmd/launcher/main.go
git commit -m "feat(addons): warn when an image VOLUME is not covered by data-dir or volumes (W-13 D3)"
```

---

### Task 4: never create or blind-write `configuration.yaml` (D8a, T13a)

**Files:**
- Modify: `internal/docker/docker.go` (new method after `ReadFile`)
- Modify: `cmd/launcher/ingress.go` (`applyIngress`'s include block → `ensureInclude`)
- Modify: `cmd/launcher/install_test.go` (`fakeRunner`: writes land)
- Test: `internal/docker/docker_test.go` (append), `cmd/launcher/ingress_test.go` (create)

**Interfaces:**
- Produces: `func (c *Client) ReadFileIfExists(name, path string) (content string, present bool, err error)`; `func ensureInclude(cli *docker.Client, w io.Writer) (changed bool, err error)`, used by Tasks 6 and 8; `fakeRunner.stdin map[string]string` (the last stdin per `StreamIn` key); and the test constants `keyTestConfigYAML`, `keyCatConfigYAML`, `keyWriteConfigYAML` and `includeYAMLLine`.

- [ ] **Step 1: Make the fake land writes.** In `cmd/launcher/install_test.go`, add the `stdin` field to `fakeRunner`:

```go
type fakeRunner struct {
	outputs map[string]string
	errs    map[string]error
	checks  map[string]bool
	calls   []string
	stdin   map[string]string // the last stdin piped to each StreamIn key
}
```

Then replace the one-line `StreamIn` method with:

```go

// StreamIn records the call and its stdin. A `docker exec -i <c> sh -c 'cat >
// <path>'` write lands the file: later `cat` reads return it trimmed (as
// docker.Client.ReadFile does), and `test -e` finds it.
func (f *fakeRunner) StreamIn(stdin string, args ...string) error {
	k := f.record(args)
	if f.stdin == nil {
		f.stdin = map[string]string{}
	}
	f.stdin[k] = stdin
	if len(args) == 6 && args[0] == "exec" && args[1] == "-i" && args[3] == "sh" && args[4] == "-c" && strings.HasPrefix(args[5], "cat > ") {
		container, path := args[2], strings.TrimPrefix(args[5], "cat > ")
		if f.outputs == nil {
			f.outputs = map[string]string{}
		}
		if f.checks == nil {
			f.checks = map[string]bool{}
		}
		f.outputs["exec "+container+" cat "+path] = strings.TrimSpace(stdin)
		delete(f.errs, "exec "+container+" cat "+path)
		f.checks["exec "+container+" test -e "+path] = true
	}
	return nil
}
```

(After `gofmt -w`, the `Stream` one-liner above it loses its padding. That is the only formatting change.) Run `go test ./cmd/launcher/` and expect `ok`, which shows no existing test depends on the old fake.

- [ ] **Step 2: Write the failing tests.** Append to `internal/docker/docker_test.go`:

```go
// D8a: present, absent and error are three different outcomes.
func TestReadFileIfExists(t *testing.T) {
	const (
		testKey = "exec homeassistant test -e /config/configuration.yaml"
		catKey  = "exec homeassistant cat /config/configuration.yaml"
	)
	cases := []struct {
		name             string
		exists, catFails bool
		checkErr         error
		content          string
		present, isErr   bool
	}{
		{"present", true, false, nil, "default_config:", true, false},
		{"absent", false, false, nil, "", false, false},
		{"cat fails on a file that exists", true, true, nil, "", true, true},
		{"docker cannot run", false, false, errors.New("exec: docker not found"), "", false, true},
	}
	for _, tc := range cases {
		f := &fakeRunner{
			outputs: map[string]string{catKey: "default_config:"},
			checks:  map[string]bool{testKey: tc.exists},
			errs:    map[string]error{testKey: tc.checkErr},
		}
		if tc.catFails {
			f.outputs[catKey] = ""
			f.errs[catKey] = errors.New("exit status 1")
		}
		content, present, err := NewWithRunner(f).ReadFileIfExists("homeassistant", "/config/configuration.yaml")
		if content != tc.content || present != tc.present || (err != nil) != tc.isErr {
			t.Errorf("%s: got (%q, %v, %v), want (%q, %v, error=%v)", tc.name, content, present, err, tc.content, tc.present, tc.isErr)
		}
	}
}
```

Create `cmd/launcher/ingress_test.go`:

```go
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
```

- [ ] **Step 3: Run them and see them fail.**

Run: `go test ./internal/docker/ -run TestReadFileIfExists; go test ./cmd/launcher/ -run TestEnsureIncludeHardenedRead`
Expected: build failures, `NewWithRunner(f).ReadFileIfExists undefined`, then `undefined: ensureInclude`.

- [ ] **Step 4: Implement.**

In `internal/docker/docker.go`, insert after `ReadFile` (before the `const (` with `pullAttempts`):

```go
// ReadFileIfExists tells the three outcomes of reading path inside the
// container apart (D8a): present (content, true, nil), absent (`test -e`
// exits non-zero: "", false, nil) and error (docker could not run, or `cat`
// failed on a file that exists). A stopped container also reads as absent,
// since `docker exec` then exits non-zero too; callers only ever skip a write
// on absent, so that is the safe side. Content is trimmed, as ReadFile's is.
func (c *Client) ReadFileIfExists(name, path string) (content string, present bool, err error) {
	ok, err := c.r.Check("exec", name, "test", "-e", path)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	out, err := c.r.Output("exec", name, "cat", path)
	if err != nil {
		return "", true, err
	}
	return out, true, nil
}
```

In `cmd/launcher/ingress.go`, add `"io"` to the imports. In `applyIngress`, replace everything from `current, err := cli.ReadFile(dockerargs.ContainerName, configYAMLPath)` up to the closing `}` of the `switch` (the conflict/changed block) with:

```go
	if _, err := ensureInclude(cli, os.Stderr); err != nil {
		return err
	}
```

Append to `cmd/launcher/ingress.go`:

```go
// ensureInclude adds `ingress: !include snap-ingress.yaml` to
// configuration.yaml and reports whether it wrote the file. It never creates
// the file and never writes from a failed read (D8a): absent or unreadable
// gives a warning and no write. HA writes its default configuration.yaml at
// first boot only when the file is missing, so a snap-made one-liner would
// suppress those defaults. Only a failed write is an error.
func ensureInclude(cli *docker.Client, w io.Writer) (changed bool, err error) {
	current, present, err := cli.ReadFileIfExists(dockerargs.ContainerName, configYAMLPath)
	if err != nil {
		fmt.Fprintf(w, "warning: could not read %s (%v); the ingress include was not checked\n", configYAMLPath, err)
		return false, nil
	}
	if !present {
		fmt.Fprintln(w, "warning: configuration.yaml not found yet; the ingress include is added on the next reconcile")
		return false, nil
	}
	out, changed, conflict := ingress.EnsureInclude(current)
	switch {
	case conflict:
		fmt.Fprintf(w,
			"warning: %s already defines its own `ingress:` key — leaving it untouched.\n"+
				"  Remove it (or fold in `!include %s`) for snap-managed ingress to apply.\n",
			configYAMLPath, ingress.IncludeFile)
		return false, nil
	case changed:
		if err := cli.WriteFile(dockerargs.ContainerName, configYAMLPath, out); err != nil {
			return false, fmt.Errorf("updating %s: %w", configYAMLPath, err)
		}
		fmt.Fprintf(w, "Added `ingress: !include %s` to configuration.yaml.\n", ingress.IncludeFile)
		return true, nil
	}
	return false, nil
}
```

- [ ] **Step 5: Run them and see them pass.**

Run: `go vet ./... && go test ./internal/docker/ ./cmd/launcher/`
Expected: `ok` ×2.

- [ ] **Step 6: Commit.**

```bash
gofmt -w internal/docker/ cmd/launcher/
git add internal/docker/ cmd/launcher/ingress.go cmd/launcher/ingress_test.go cmd/launcher/install_test.go
git commit -m "fix(ingress): never create configuration.yaml or write it from a failed read (W-13 D8a)"
```

---

### Task 5: `ingress.Decide` replaces `ShouldSync` (D6, T8)

**Files:**
- Modify: `internal/ingress/derive.go` (replace `ShouldSync`)
- Modify: `cmd/launcher/ingress.go` (the one call site)
- Modify: `cmd/launcher/ingressmode_test.go` (one comment)
- Test: `internal/ingress/derive_test.go` (`TestShouldSync` → `TestDecide`, D19)

**Interfaces:**
- Produces: `func Decide(current, rendered string, panels int) (write, restart bool)`. `ShouldSync` is removed.

- [ ] **Step 1: Write the failing test.** In `internal/ingress/derive_test.go`, add `"strings"` to the imports and replace the whole `TestShouldSync` function with:

```go
// T8 (D6, replaces TestShouldSync per D19): write and restart only when the
// YAML changes meaning; a comment-only change is written without a restart.
func TestDecide(t *testing.T) {
	const (
		oldHdr = "# Managed by the home-assistant snap (old wording).\n"
		newHdr = "# Managed by the home-assistant snap — new wording.\n# Second header line.\n"
		body   = "nodered:\n  url: \"http://127.0.0.1:1880\"\n"
	)
	cases := []struct {
		name              string
		current, rendered string
		panels            int
		write, restart    bool
	}{
		{"fresh install: no panels, no file", "", newHdr + "{}\n", 0, false, false},
		{"first panel", "", newHdr + body, 1, true, true},
		{"trimmed-equal (ReadFile trims)", strings.TrimSpace(newHdr + body), newHdr + body, 1, false, false},
		{"whitespace at the ends only", "\n\n" + newHdr + body + "\n  \n", newHdr + body, 1, false, false},
		{"header-only change", oldHdr + body, newHdr + body, 1, true, false},
		{"header added to a headerless file", body, newHdr + body, 1, true, false},
		{"panel changed", newHdr + "old:\n  url: \"http://x\"\n", newHdr + body, 1, true, true},
		{"last panel removed", newHdr + body, newHdr + "{}\n", 0, true, true},
		{"header change on the empty map", oldHdr + "{}", newHdr + "{}\n", 0, true, false},
	}
	for _, tc := range cases {
		write, restart := Decide(tc.current, tc.rendered, tc.panels)
		if write != tc.write || restart != tc.restart {
			t.Errorf("%s: Decide = (write %v, restart %v), want (%v, %v)", tc.name, write, restart, tc.write, tc.restart)
		}
	}
}
```

- [ ] **Step 2: Run it and see it fail.**

Run: `go test ./internal/ingress/`
Expected: build failure, `undefined: Decide`.

- [ ] **Step 3: Implement.** In `internal/ingress/derive.go`, replace the `ShouldSync` comment and function with:

```go
// Decide says whether reconcile must (re)write the ingress file and whether
// HA must restart for it (D6). current is the file's present content ("" when
// absent; docker's ReadFile trims it, hence the trimmed comparison).
//   - no panels and no file (fresh install): neither, so no include line and
//     no restart either;
//   - trimmed content equal: neither;
//   - equal once the `#` comment lines are dropped (a header-only change, as
//     between snap revisions): write, no restart;
//   - anything else: both.
//
// Render quotes every value, so a `#` only ever starts a comment line.
func Decide(current, rendered string, panels int) (write, restart bool) {
	cur, ren := strings.TrimSpace(current), strings.TrimSpace(rendered)
	if panels == 0 && cur == "" {
		return false, false
	}
	if cur == ren {
		return false, false
	}
	if withoutComments(cur) == withoutComments(ren) {
		return true, false
	}
	return true, true
}

// withoutComments drops the lines whose first non-blank character is `#`.
func withoutComments(s string) string {
	var keep []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		keep = append(keep, ln)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}
```

In `cmd/launcher/ingress.go`, replace `if !ingress.ShouldSync(current, body, len(entries)) {` with:

```go
		if write, _ := ingress.Decide(current, body, len(entries)); !write {
```

Task 6 uses `restart` for real. Until then, a write still restarts, as it does today. In `cmd/launcher/ingressmode_test.go`, change the comment `(ShouldSync says no)` to `(Decide says no write)`.

- [ ] **Step 4: Run it and see it pass.**

Run: `go vet ./... && go test ./internal/ingress/ ./cmd/launcher/`
Expected: `ok` ×2.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/ingress/ cmd/launcher/
git add internal/ingress/ cmd/launcher/ingress.go cmd/launcher/ingressmode_test.go
git commit -m "feat(ingress): Decide replaces ShouldSync; a comment-only change is written without a restart (W-13 D6)"
```

---

### Task 6: compare-first `ingress sync`, `--force`, and the include check on every reconcile with panels (D7, D8, T9)

**Files:**
- Modify: `cmd/launcher/ingress.go` (replace `runIngress` and `applyIngress`; add `syncIngressFiles`)
- Modify: `cmd/launcher/main.go` (two call sites)
- Modify: `cmd/launcher/ingressmode_test.go` (D19: `TestIngressSyncChecksMode`, the `TestApplyIngressAutoSyncChecksMode` call)
- Test: `cmd/launcher/ingress_test.go` (append)

**Interfaces:**
- Consumes: `ingress.Decide` (Task 5), `ensureInclude` (Task 4), and the stateful `fakeRunner` (Task 4).
- Produces: `func runIngress(cli *docker.Client, cfg config.Config, args []string, stdout, stderr io.Writer) error`; `func applyIngress(cli *docker.Client, cfg config.Config, force bool, w io.Writer) (restarted bool, err error)`; `func syncIngressFiles(cli *docker.Client, cfg config.Config, force bool, w io.Writer) (restart bool, err error)`, which never restarts and is used by Task 8. Also the test helpers `syncFake(file, configYAML string) *fakeRunner`, `zwaveCfg` and `unchangedMsg`.

- [ ] **Step 1: Write the failing tests.** In `cmd/launcher/ingress_test.go`, extend the imports to `"bytes"`, `"errors"`, `"io"`, `"strings"`, `"testing"`, plus `internal/config`, `internal/docker` and `internal/ingress`. Then append:

```go
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
		{"include missing, file unchanged", []string{"sync"}, rendered, "default_config:", 0, 1, 1, false, ""},
		{"foreign ingress key, file unchanged", []string{"sync"}, rendered, "ingress:\n  foo:\n    url: http://x", 0, 0, 0, true, "already defines its own `ingress:` key"},
		{"panel changed", []string{"sync"}, "old:\n  url: \"http://x\"", withInclude, 1, 0, 1, false, ""},
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
```

D19 edits in `cmd/launcher/ingressmode_test.go`. First add `"io"` to its imports. Then extend the comment above `TestIngressSyncChecksMode` with:

```go
// W-13 D19: the restart is asserted on `sync --force`; a plain sync of an
// unchanged file checks the mode the same way and does not restart.
```

Change its `runIngress` call to:

```go
		if err := runIngress(docker.NewWithRunner(f), config.Config{Ingress: tc.ingress}, []string{"sync", "--force"}, io.Discard, io.Discard); err != nil {
```

Then add this after its `for` loop, before the function's closing `}`:

```go

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
```

In `TestApplyIngressAutoSyncChecksMode`, change the call to:

```go
		if _, err := applyIngress(docker.NewWithRunner(f), cfg, false, io.Discard); err != nil {
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./cmd/launcher/`
Expected: build failure, `too many arguments in call to runIngress` (and to `applyIngress`).

- [ ] **Step 3: Implement.** In `cmd/launcher/ingress.go`, remove `"os"` from the imports. Replace everything from `// runIngress renders` down to (not including) `// ensureInclude adds` with:

```go
// runIngress renders the merged panel map (explicit ingress.* + add-on
// panels) from snap config.
//   - `show`: print the rendered file (dry run, no side effects).
//   - `sync` (default): compare-first, like reconcile's auto-sync (D7): land
//     the file and the include line, and restart HA only when the panels
//     changed. `sync --force` always writes and restarts (the repair hatch).
func runIngress(cli *docker.Client, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	sub := "sync"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "show":
		fmt.Fprint(stdout, ingress.Render(ingress.MergedEntries(cfg)))
		return nil
	case "sync":
		force := false
		var opts []string
		if len(args) > 1 {
			opts = args[1:]
		}
		for _, a := range opts {
			if a != "--force" {
				return fmt.Errorf("unknown ingress sync option %q (use: --force)", a)
			}
			force = true
		}
		restarted, err := applyIngress(cli, cfg, force, stderr)
		if err == nil && !restarted {
			fmt.Fprintln(stdout, "ingress: panels unchanged; Home Assistant not restarted (use --force to restart anyway)")
		}
		return err
	default:
		return fmt.Errorf("unknown ingress subcommand %q (use: sync, show)", sub)
	}
}

// applyIngress lands the merged panel map in /config and restarts HA when
// that is needed for it to take effect; restarted says whether it did. It
// checks the hass_ingress install and mode first (W-12 D11, D12). force
// (`ingress sync --force`) writes and restarts unconditionally; otherwise it
// is change-driven (D6–D8), so a boot-time reconcile is a true no-op. Its
// messages go to w. Callers treat auto-sync errors as warnings: panels are
// cosmetic, containers are the substance.
func applyIngress(cli *docker.Client, cfg config.Config, force bool, w io.Writer) (restarted bool, err error) {
	entries := ingress.MergedEntries(cfg)
	if err := preflightContainer(cli, cfg); err != nil {
		return false, err
	}
	if len(entries) > 0 {
		installed, err := cli.ExecCheck(dockerargs.ContainerName, "test", "-d", "/config/custom_components/ingress")
		if err == nil && !installed {
			fmt.Fprintln(w, "warning: ingress panels are configured but hass_ingress is not installed — run `home-assistant.install hass-ingress`")
		}
		if err == nil && installed {
			warnIngressMode(cli, w)
		}
	}
	restart, err := syncIngressFiles(cli, cfg, force, w)
	if err != nil {
		return false, err
	}
	if !restart {
		return false, nil
	}
	fmt.Fprintf(w, "Wrote %d ingress panel(s); restarting Home Assistant…\n", len(entries))
	return true, cli.Restart(dockerargs.ContainerName)
}

// syncIngressFiles brings snap-ingress.yaml and configuration.yaml's include
// line up to date and reports whether HA must restart to pick them up. It
// never restarts HA itself, so it can also run in the old container just
// before a recreate (D4). force writes the file and asks for a restart even
// when nothing changed. The include line is checked whenever there is at
// least one panel, even when the file is unchanged (D8). Warnings go to w.
func syncIngressFiles(cli *docker.Client, cfg config.Config, force bool, w io.Writer) (restart bool, err error) {
	entries := ingress.MergedEntries(cfg)
	body := ingress.Render(entries)
	write := true
	restart = true
	if !force {
		current, _ := cli.ReadFile(dockerargs.ContainerName, ingressFilePath)
		write, restart = ingress.Decide(current, body, len(entries))
	}
	if write {
		if err := cli.WriteFile(dockerargs.ContainerName, ingressFilePath, body); err != nil {
			return false, fmt.Errorf("writing %s: %w", ingressFilePath, err)
		}
	}
	if len(entries) > 0 {
		added, err := ensureInclude(cli, w)
		if err != nil {
			return false, err
		}
		restart = restart || added
	}
	return restart, nil
}

```

In `cmd/launcher/main.go`, change the `ingress` case to `return runIngress(cli, cfg, os.Args[2:], os.Stdout, os.Stderr)`, and change `reconcileSet`'s call to:

```go
	if _, err := applyIngress(cli, cfg, false, os.Stderr); err != nil {
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go vet ./... && go test ./cmd/launcher/`
Expected: `ok`.

- [ ] **Step 5: Commit.**

```bash
gofmt -w cmd/launcher/
git add cmd/launcher/
git commit -m "feat(ingress): compare-first sync with --force, include line checked on every reconcile with panels (W-13 D7, D8)"
```

---

### Task 7: `BeforeSwap` in the reconcile ladder (D4, T4, T5)

**Files:**
- Modify: `internal/reconcile/reconcile.go` (`ContainerSpec`, the recreate branch)
- Test: `internal/reconcile/reconcile_test.go` (fake records order and can fail the pull; append tests)

**Interfaces:**
- Produces: `ContainerSpec.BeforeSwap func()`, called once, only in the recreate branch, after `Pull` succeeds and before `Remove`.

- [ ] **Step 1: Write the failing tests.** In `internal/reconcile/reconcile_test.go`, add two fields to `fakeDocker`:

```go
	pullErr                    error
	order                      []string // "pull", "remove", "run" and the test's "hook", in call order
```

Replace the `Pull` one-liner with:

```go
func (f *fakeDocker) Pull(string) error {
	f.pulled = true
	f.order = append(f.order, "pull")
	return f.pullErr
}
```

Add `f.order = append(f.order, "remove")` to `Remove`, after its `removedNames` append. Add `f.order = append(f.order, "run")` to `Run`, after `f.ran = true`. Then append:

```go
// hookedSpec is spec() with a BeforeSwap that counts its calls and records
// itself in f.order.
func hookedSpec(f *fakeDocker, calls *int) ContainerSpec {
	s := spec()
	s.BeforeSwap = func() {
		*calls++
		f.order = append(f.order, "hook")
	}
	return s
}

// T4 (D4): on recreate the hook runs once, after the pull and before the rm.
func TestBeforeSwapRunsBetweenPullAndRemove(t *testing.T) {
	f := &fakeDocker{exists: true, hash: "stale"}
	calls := 0
	act, err := Reconcile(f, hookedSpec(f, &calls), false)
	if err != nil || act != ActionRecreated {
		t.Fatalf("act=%v err=%v", act, err)
	}
	if calls != 1 {
		t.Errorf("hook called %d times, want 1", calls)
	}
	if got := strings.Join(f.order, ","); got != "pull,hook,remove,run" {
		t.Errorf("order = %s, want pull,hook,remove,run", got)
	}
}

// T5 (D4): no hook on create, start, no-op or a failed pull; nil is fine.
func TestBeforeSwapOnlyOnRecreate(t *testing.T) {
	cases := []struct {
		name string
		f    *fakeDocker
	}{
		{"create", &fakeDocker{exists: false}},
		{"start", &fakeDocker{exists: true, running: false, hash: "want"}},
		{"no-op", &fakeDocker{exists: true, running: true, hash: "want"}},
		{"pull fails", &fakeDocker{exists: true, hash: "stale", pullErr: errors.New("registry down")}},
	}
	for _, tc := range cases {
		calls := 0
		_, _ = Reconcile(tc.f, hookedSpec(tc.f, &calls), false)
		if calls != 0 {
			t.Errorf("%s: hook called %d times, want 0; order %v", tc.name, calls, tc.f.order)
		}
	}
	f := &fakeDocker{exists: true, hash: "stale"}
	if act, err := Reconcile(f, spec(), false); err != nil || act != ActionRecreated {
		t.Errorf("nil hook: act=%v err=%v, want a plain recreate", act, err)
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./internal/reconcile/`
Expected: build failure, `s.BeforeSwap undefined (type ContainerSpec has no field or method BeforeSwap)`.

- [ ] **Step 3: Implement.** In `internal/reconcile/reconcile.go`, add after `RunArgs  []string` in `ContainerSpec`:

```go
	// BeforeSwap, when set, runs once on recreate only: after the pull has
	// succeeded and before the old container is removed, while it is still
	// running. It returns nothing; the ladder ignores what it does (D4).
	BeforeSwap func()
```

In the recreate branch (`if force || have != s.WantHash {`), between the `Pull` error check and `d.Remove`, add:

```go
		if s.BeforeSwap != nil {
			s.BeforeSwap()
		}
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go vet ./... && go test ./internal/reconcile/ ./cmd/launcher/`
Expected: `ok` ×2.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/reconcile/
git add internal/reconcile/
git commit -m "feat(reconcile): optional BeforeSwap hook between pull and rm on recreate (W-13 D4)"
```

---

### Task 8: HA's hook writes the ingress files before the swap (D4, D5, T6, T7)

**Files:**
- Modify: `cmd/launcher/ingress.go` (append `preSwapIngress`)
- Modify: `cmd/launcher/main.go` (`reconcileSet`)
- Test: `cmd/launcher/reconcileset_test.go` (create)

**Interfaces:**
- Consumes: `syncIngressFiles` (Task 6), `ContainerSpec.BeforeSwap` (Task 7), and `syncFake`, `includeYAMLLine`, `keyWriteConfigYAML`, `indexOfCall`, `keyRunning`, `keyRestart` and `keyWriteIngress` from the other test files.
- Produces: `func preSwapIngress(cli *docker.Client, cfg config.Config, w io.Writer)`.

- [ ] **Step 1: Write the failing tests.** Create `cmd/launcher/reconcileset_test.go`:

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
	keyExistsHA  = "ps -a --filter name=^/homeassistant$ --format {{.Names}}"
	keyHashHA    = `inspect -f {{ index .Config.Labels "io.giaever.home-assistant.spec-hash" }} homeassistant`
	keyRemoveHA  = "rm -f -v homeassistant"
	prefixRunHA  = "run -d --name homeassistant "
	keyStartHA   = "start homeassistant"
	keyCatIngres = "exec homeassistant cat " + ingressFilePath
)

// modelBNodeRED moves HA onto the bridge with one add-on panel, so both HA's
// hash and the panel URL change against the model A state the fake holds.
const modelBNodeRED = `{"docker":{"network":"ha-addons"},"addons":{"nodered":{"image":"nodered/node-red:4","ports":{"ui":1880},"ingress":{"title":"Node-RED"}}}}`

func mustParse(t *testing.T, in string) config.Config {
	t.Helper()
	c, err := config.Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// T6 (D4): a network change recreates HA with the ingress files already
// written through the old container, so HA starts once and is never
// restarted.
func TestReconcileSetWritesIngressBeforeSwap(t *testing.T) {
	modelA := mustParse(t, `{"addons":{"nodered":{"image":"nodered/node-red:4","ports":{"ui":1880},"ingress":{"title":"Node-RED"}}}}`)
	f := syncFake(ingress.Render(ingress.MergedEntries(modelA)), "default_config:\n"+includeYAMLLine)
	f.outputs[keyExistsHA] = "homeassistant"
	f.outputs[keyHashHA] = "stale"
	if _, err := reconcileSet(docker.NewWithRunner(f), mustParse(t, modelBNodeRED), false); err != nil {
		t.Fatalf("reconcileSet: %v", err)
	}
	w, rm := indexOfCall(f.calls, keyWriteIngress), indexOfCall(f.calls, keyRemoveHA)
	if w < 0 || rm < 0 || w > rm {
		t.Errorf("snap-ingress.yaml write at %d, rm at %d: the write must come first; calls %q", w, rm, f.calls)
	}
	if n := f.count(keyWriteIngress); n != 1 {
		t.Errorf("snap-ingress.yaml written %d times, want 1 (the hook's)", n)
	}
	if !strings.Contains(f.stdin[keyWriteIngress], `"http://ha-addon-nodered:1880"`) {
		t.Errorf("the hook wrote %q, want the model B panel URL", f.stdin[keyWriteIngress])
	}
	if n := f.count(keyRestart); n != 0 {
		t.Errorf("restart called %d times, want 0", n)
	}
	if n, s := f.count(prefixRunHA), f.count(keyStartHA); n != 1 || s != 0 {
		t.Errorf("HA run %d times and started %d times, want one run and no start", n, s)
	}
}

// T7 (D5): when HA does not exist yet, the files are written after Set and
// HA restarts once, as before.
func TestReconcileSetCreateFallsBackToRestart(t *testing.T) {
	f := syncFake("", "default_config:\n"+includeYAMLLine)
	f.errs[keyCatIngres] = errors.New("exit status 1")
	if _, err := reconcileSet(docker.NewWithRunner(f), mustParse(t, modelBNodeRED), false); err != nil {
		t.Fatalf("reconcileSet: %v", err)
	}
	run, w := indexOfCall(f.calls, prefixRunHA), indexOfCall(f.calls, keyWriteIngress)
	if run < 0 || w < run {
		t.Errorf("HA run at %d, snap-ingress.yaml write at %d: the write must follow the run; calls %q", run, w, f.calls)
	}
	if n := f.count(keyWriteIngress); n != 1 {
		t.Errorf("snap-ingress.yaml written %d times, want 1", n)
	}
	if n := f.count(keyRestart); n != 1 {
		t.Errorf("restart called %d times, want 1 (the D5 fallback)", n)
	}
}

// D4/D5: a stopped HA cannot be written through; the hook says so in one
// note line and leaves the files to the post-Set sync.
func TestPreSwapIngressStoppedHA(t *testing.T) {
	f := syncFake("", "default_config:")
	f.outputs[keyRunning] = "false"
	var buf bytes.Buffer
	preSwapIngress(docker.NewWithRunner(f), mustParse(t, modelBNodeRED), &buf)
	if n := f.count(keyWriteIngress) + f.count(keyWriteConfigYAML); n != 0 {
		t.Errorf("%d writes to a stopped HA, want 0", n)
	}
	if lines := strings.Count(buf.String(), "\n"); lines != 1 || !strings.HasPrefix(buf.String(), "note: ") {
		t.Errorf("output %q, want exactly one note: line", buf.String())
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./cmd/launcher/ -run 'TestReconcileSet|TestPreSwap'`
Expected: build failure, `undefined: preSwapIngress`. T7 pins today's fallback and passes once the file builds (G8). The behavioral red for this task is T6: before Step 3's wiring, it fails with `snap-ingress.yaml write at 15, rm at 4: the write must come first` and `restart called 1 times, want 0` (Task 15, M5).

- [ ] **Step 3: Implement.** Append to `cmd/launcher/ingress.go`:

```go

// preSwapIngress is HA's BeforeSwap hook (D4). It lands the ingress files in
// /config through the old, still-running container, so the replacement reads
// them at its first boot and the post-Set sync finds nothing to restart for.
// It is quiet apart from one note: line when it cannot write; the post-Set
// sync then writes and restarts as before (D5).
func preSwapIngress(cli *docker.Client, cfg config.Config, w io.Writer) {
	if running, _ := cli.Running(dockerargs.ContainerName); !running {
		fmt.Fprintln(w, "note: Home Assistant is not running, so the ingress files are written after it is recreated (one extra restart)")
		return
	}
	if _, err := syncIngressFiles(cli, cfg, false, io.Discard); err != nil {
		fmt.Fprintf(w, "note: could not write the ingress files before recreating Home Assistant (%v); they are written after the swap instead\n", err)
	}
}
```

In `cmd/launcher/main.go` (`reconcileSet`), replace `results := reconcile.Set(cli, buildContainerSpecs(cfg), force)` with:

```go
	specs := buildContainerSpecs(cfg)
	// D4: on a recreate, HA's replacement boots with the final ingress files,
	// so a config change costs one HA start instead of a start and a restart.
	specs[0].BeforeSwap = func() { preSwapIngress(cli, cfg, os.Stderr) }
	results := reconcile.Set(cli, specs, force)
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go vet ./... && go test ./cmd/launcher/`
Expected: `ok`.

- [ ] **Step 5: Commit.**

```bash
gofmt -w cmd/launcher/
git add cmd/launcher/
git commit -m "fix(ingress): write the ingress files in the old HA before the swap, so a recreate starts HA once (W-13 D4, D5)"
```

---

### Task 9: `--add-host=host.docker.internal:host-gateway` under model B (D10, T11)

**Files:**
- Create: `internal/config/modelb.go` (the constant; Task 10 extends the file)
- Modify: `internal/dockerargs/dockerargs.go`
- Test: `internal/dockerargs/golden_test.go` (append)

**Interfaces:**
- Produces: `config.HostGatewayName = "host.docker.internal"`, and the unexported `dockerargs.hostGatewayFlag`.

- [ ] **Step 1: Write the failing test.** In `internal/dockerargs/golden_test.go`, add `"strings"` to the imports and append:

```go
// T11 (D10): under model B, HA gets a stable name for the host; nothing else
// gets --add-host.
func TestModelBAddsHostGateway(t *testing.T) {
	b := cfg(`{"docker":{"network":"ha-addons"}}`)
	if got := join(BuildRunArgs(b)); !strings.Contains(got, "--add-host=host.docker.internal:host-gateway") {
		t.Errorf("model B args lack --add-host=host.docker.internal:host-gateway\n got: %s", got)
	}
	if SpecHash(b) == preD10HashModelB {
		t.Error("model B's hash must change with --add-host, or an existing model B HA is never recreated to get it")
	}
	for _, j := range []string{`{}`, richModelA, `{"docker":{"network":"bridge"}}`} {
		if got := join(BuildRunArgs(cfg(j))); strings.Contains(got, "--add-host") {
			t.Errorf("%s: unexpected --add-host\n got: %s", j, got)
		}
	}
}
```

- [ ] **Step 2: Run it and see it fail.**

Run: `go test ./internal/dockerargs/`
Expected: `FAIL: TestModelBAddsHostGateway … model B args lack --add-host=host.docker.internal:host-gateway` and `model B's hash must change with --add-host`. `TestGoldenModelA` still passes.

- [ ] **Step 3: Implement.** Create `internal/config/modelb.go`:

```go
package config

// HostGatewayName is the name Home Assistant uses for the host under model B
// (docker.network=ha-addons): docker maps it to the bridge gateway through
// `--add-host=host.docker.internal:host-gateway` (D10, Docker ≥ 20.10).
const HostGatewayName = "host.docker.internal"
```

In `internal/dockerargs/dockerargs.go`, add above `// ConfigSource returns`:

```go
// hostGatewayFlag gives HA a stable name for the host under model B, where
// its "localhost" is its own container (D10).
const hostGatewayFlag = "--add-host=" + config.HostGatewayName + ":host-gateway"
```

In `BuildRunArgs`, change the model B line to `args = append(args, "-p", "8123:8123", hostGatewayFlag)`. In `SpecHash`, insert before the `sortedKeys(c.Devices)` loop:

```go
	// Model B only, so a model A hash stays byte-identical to the one before
	// D10 and model A users are not recreated on upgrade (golden_test.go).
	if c.Network == AddonNetwork {
		fmt.Fprintf(h, "addhost=%s\n", hostGatewayFlag)
	}
```

- [ ] **Step 4: Run it and see it pass.**

Run: `go vet ./... && go test ./internal/... ./cmd/...`
Expected: all `ok`. `TestGoldenModelA` is unchanged and still green.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/config/ internal/dockerargs/
git add internal/config/modelb.go internal/dockerargs/
git commit -m "feat(dockerargs): model B runs HA with --add-host=host.docker.internal:host-gateway (W-13 D10)"
```

---

### Task 10: rewrite loopback panel URLs under model B, and say so (D11, D12, T12, T18)

**Files:**
- Modify: `internal/config/modelb.go` (rewrite + warnings)
- Modify: `internal/config/config.go` (`Validate`'s network warning)
- Modify: `internal/ingress/derive.go` (`MergedEntries`)
- Test: `internal/config/modelb_test.go` (create), `internal/ingress/derive_test.go` (append)

**Interfaces:**
- Consumes: `config.HostGatewayName` (Task 9), `addonsNetworkName` (in `config/addons.go`).
- Produces: `func LoopbackToHostGateway(raw string) (string, bool)`; `func (c Config) modelBWarnings() []string`.

- [ ] **Step 1: Write the failing tests.** Create `internal/config/modelb_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

// T12 (D11): only a host of exactly localhost, 127.0.0.1 or [::1] is rewritten;
// scheme, port and path are kept.
func TestLoopbackToHostGateway(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"http://localhost:8091", "http://host.docker.internal:8091", true},
		{"http://127.0.0.1:3000/x", "http://host.docker.internal:3000/x", true},
		{"http://[::1]:80", "http://host.docker.internal:80", true},
		{"https://localhost/panel?a=1", "https://host.docker.internal/panel?a=1", true},
		{"http://localhost.example:80", "http://localhost.example:80", false},
		{"http://192.168.1.5:80", "http://192.168.1.5:80", false},
		{"http://ha-addon-nodered:1880", "http://ha-addon-nodered:1880", false},
		{"not a url", "not a url", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := LoopbackToHostGateway(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("LoopbackToHostGateway(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// T18 (D12): under model B the warning says what breaks and names each
// rewritten panel; model A has no such warning.
func TestValidateModelBWarning(t *testing.T) {
	const zwave = `"ingress":{"zwave":{"url":"http://localhost:8091"},"lan":{"url":"http://192.168.1.5:80"}}`
	c, _ := Parse([]byte(`{"docker":{"network":"ha-addons"},` + zwave + `}`))
	warnings, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(warnings, "\n")
	for _, want := range []string{
		`docker.network="ha-addons": discovery and Bluetooth need host networking, and Home Assistant's "localhost" becomes its own container — integrations set up with localhost URLs (e.g. zwave_js ws://localhost:3000) lose their connection; point them at host.docker.internal; the host service must listen beyond 127.0.0.1`,
		"ingress.zwave: url rewritten to http://host.docker.internal:8091 under docker.network=ha-addons",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings lack %q\n got:\n%s", want, all)
		}
	}
	if strings.Contains(all, "ingress.lan:") {
		t.Errorf("a non-loopback panel was reported as rewritten:\n%s", all)
	}
	c, _ = Parse([]byte(`{` + zwave + `}`))
	warnings, _ = c.Validate()
	if all := strings.Join(warnings, "\n"); strings.Contains(all, HostGatewayName) {
		t.Errorf("model A must not warn about %s:\n%s", HostGatewayName, all)
	}
}
```

Append to `internal/ingress/derive_test.go`:

```go
// T12 (D11): under model B, explicit loopback panels render with the host's
// name; model A and derived add-on panels are untouched.
func TestMergedEntriesModelBLoopback(t *testing.T) {
	const body = `"ingress":{
		"zwave":{"url":"http://localhost:8091"},
		"lan":{"url":"http://localhost.example:80"}
	},"addons":{"nodered":{"image":"i","ports":{"ui":"8080:1880"},"ingress":{"title":"Node-RED"}}}`
	b := MergedEntries(parse(t, `{"docker":{"network":"ha-addons"},`+body+`}`))
	if got := b["zwave"].URL; got != "http://host.docker.internal:8091" {
		t.Errorf("model B zwave URL = %q, want http://host.docker.internal:8091", got)
	}
	if got := b["lan"].URL; got != "http://localhost.example:80" {
		t.Errorf("model B lan URL = %q, want it unchanged", got)
	}
	if got := b["nodered"].URL; got != "http://ha-addon-nodered:1880" {
		t.Errorf("model B derived URL = %q, want the container-name form", got)
	}
	a := MergedEntries(parse(t, `{`+body+`}`))
	if got := a["zwave"].URL; got != "http://localhost:8091" {
		t.Errorf("model A zwave URL = %q, want it unchanged", got)
	}
	if got := a["nodered"].URL; got != "http://127.0.0.1:8080" {
		t.Errorf("model A derived URL = %q", got)
	}
}
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./internal/config/ ./internal/ingress/`
Expected: config: build failure, `undefined: LoopbackToHostGateway`. ingress: `FAIL: TestMergedEntriesModelBLoopback … model B zwave URL = "http://localhost:8091", want http://host.docker.internal:8091`.

- [ ] **Step 3: Implement.** Replace `internal/config/modelb.go` with:

```go
package config

import (
	"net/url"
	"sort"
)

// HostGatewayName is the name Home Assistant uses for the host under model B
// (docker.network=ha-addons): docker maps it to the bridge gateway through
// `--add-host=host.docker.internal:host-gateway` (D10, Docker ≥ 20.10).
const HostGatewayName = "host.docker.internal"

// modelBWarning is D12's text: what model B breaks, and the way out.
const modelBWarning = `docker.network="ha-addons": discovery and Bluetooth need host networking, and Home Assistant's "localhost" becomes its own container — integrations set up with localhost URLs (e.g. zwave_js ws://localhost:3000) lose their connection; point them at host.docker.internal; the host service must listen beyond 127.0.0.1`

// LoopbackToHostGateway rewrites a URL whose host is exactly localhost,
// 127.0.0.1 or ::1 to HostGatewayName, keeping scheme, port, path and query
// (D11). ok is false, and raw comes back unchanged, for any other host or a
// URL without a host.
func LoopbackToHostGateway(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw, false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return raw, false
	}
	host := HostGatewayName
	if p := u.Port(); p != "" {
		host += ":" + p
	}
	u.Host = host
	return u.String(), true
}

// modelBWarnings is D12: the model B warning, then one line per explicit
// panel that render rewrites (sorted by name).
func (c Config) modelBWarnings() []string {
	warnings := []string{modelBWarning}
	names := make([]string, 0, len(c.Ingress))
	for n := range c.Ingress {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if u, ok := LoopbackToHostGateway(c.Ingress[n].URL); ok {
			warnings = append(warnings, "ingress."+n+": url rewritten to "+u+" under docker.network="+addonsNetworkName)
		}
	}
	return warnings
}
```

In `internal/config/config.go` (`Validate`), replace the `if c.Network != "host" { … }` block with:

```go
	switch {
	case c.Network == addonsNetworkName:
		warnings = append(warnings, c.modelBWarnings()...)
	case c.Network != "host":
		warnings = append(warnings, fmt.Sprintf(
			"docker.network=%q: device discovery and Bluetooth need host networking; prefer docker.network=host", c.Network))
	}
```

In `internal/ingress/derive.go`, replace `MergedEntries` and its comment with:

```go
// MergedEntries is the full panel map: derived add-on panels plus explicit
// ingress.* entries. Collisions are rejected at validate; if one slips
// through anyway the explicit entry wins (deterministic). Under model B an
// explicit loopback URL is rendered with the host's name, since HA's own
// localhost is its container there (D11); snap config is left as it is, so
// switching back to model A restores the original URL.
func MergedEntries(c config.Config) map[string]config.IngressSpec {
	m := DeriveEntries(c.Addons, c.Network)
	for k, v := range c.Ingress {
		if c.Network == dockerargs.AddonNetwork {
			if u, ok := config.LoopbackToHostGateway(v.URL); ok {
				v.URL = u
			}
		}
		m[k] = v
	}
	return m
}
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go vet ./... && go test ./internal/... ./cmd/...`
Expected: all `ok`. `TestValidateWarnsOnNonHostNetwork` (`bridge`) keeps the old text.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/config/ internal/ingress/
git add internal/config/ internal/ingress/
git commit -m "feat(ingress): model B renders loopback panel URLs as host.docker.internal and warns what breaks (W-13 D11, D12)"
```

---

### Task 11: an empty object is absent, at every level (D14, T13)

**Files:**
- Create: `internal/config/empty.go`
- Modify: `internal/config/config.go` (`Parse`)
- Test: `internal/config/addons_test.go` (D19/G1 rows, append), `internal/ingress/derive_test.go` (D19/G1 inputs)

**Interfaces:**
- Produces: `func isEmptyObject(v any) bool`, `func pruneEmpty(m map[string]any)` and `func withoutEmptyObjects(data []byte) ([]byte, error)`, all unexported. Task 12 builds `EmptyPaths` on `isEmptyObject`.

- [ ] **Step 1: Change the tests that relied on `{}` (G1), and see them pass on the old code.** In `internal/config/addons_test.go`, `TestValidateAddonsRejectsBadInputs`:

```go
		`{"addons":{"x":{"ports":{"ui":80}}}}`, // missing image (an empty addons.x is no add-on, D14)
```

replaces the row `{"addons":{"x":{}}}`. The `"ingress":{}` of the "ambiguous panel port" and "name collision" rows becomes `"ingress":{"title":"X"}`. In `internal/ingress/derive_test.go`, the two `"ingress":{}` of `TestDeriveEntriesModelAExplicitBindIP` become `"ingress":{"title":"Node-RED"}` and `"ingress":{"title":"MQTT"}`. The one in `TestMergedEntries` becomes `"ingress":{"title":"Node-RED"}`.

Run: `gofmt -w internal/config/addons_test.go && go test ./internal/config/ ./internal/ingress/`
Expected: `ok` ×2, on the unchanged parser.

- [ ] **Step 2: Write the failing tests.** Append to `internal/config/addons_test.go`:

```go
// T13 (D14): an empty object is absent, at every level and recursively.
func TestParseEmptyObjectsAreAbsent(t *testing.T) {
	cases := []struct {
		name, in string
		check    func(Config) bool
	}{
		{"empty add-on", `{"addons":{"x":{}}}`, func(c Config) bool { return c.Addons == nil }},
		{"empty add-on panel", `{"addons":{"x":{"image":"i","ports":{"ui":1},"ingress":{}}}}`,
			func(c Config) bool { return c.Addons["x"].Image == "i" && c.Addons["x"].Ingress == nil }},
		{"empty explicit panel", `{"ingress":{"z":{}}}`, func(c Config) bool { return c.Ingress == nil }},
		{"recursively empty add-on", `{"addons":{"x":{"ports":{},"ingress":{}}}}`, func(c Config) bool { return c.Addons == nil }},
		{"empty docker maps", `{"docker":{"devices":{},"volumes":{},"environment":{}},"addons":{"x":{"image":"i","volumes":{},"environment":{},"devices":{}}}}`,
			func(c Config) bool {
				a := c.Addons["x"]
				return c.Devices == nil && c.Volumes == nil && c.Environment == nil && a.Volumes == nil && a.Environment == nil && a.Devices == nil
			}},
		{"false and empty strings are values", `{"docker":{"privileged":false,"timezone":""},"addons":{"x":{"image":"i","ports":{"ui":1},"ingress":{"require-admin":false}}}}`,
			func(c Config) bool { return !c.Privileged && c.Addons["x"].Ingress != nil }},
	}
	for _, tc := range cases {
		c, err := Parse([]byte(tc.in))
		if err != nil {
			t.Errorf("%s: Parse: %v", tc.name, err)
			continue
		}
		if !tc.check(c) {
			t.Errorf("%s: parsed %+v", tc.name, c)
		}
		if _, err := c.Validate(); err != nil {
			t.Errorf("%s: Validate: %v", tc.name, err)
		}
	}
}

// Numbers survive the empty-object pass unchanged (snapctl emits ports
// unquoted).
func TestParseKeepsNumbers(t *testing.T) {
	c, err := Parse([]byte(`{"addons":{"x":{"image":"i","ports":{"ui":1880,"big":65535}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p := c.Addons["x"].Ports; p["ui"] != "1880" || p["big"] != "65535" {
		t.Errorf("ports = %v", p)
	}
}

// The empty-object pass must not loosen parsing: trailing data stays an error.
func TestParseRejectsTrailingData(t *testing.T) {
	if _, err := Parse([]byte(`{"docker":{}} x`)); err == nil {
		t.Error("trailing data after the document must be an error")
	}
}
```

- [ ] **Step 3: Run them and see them fail.**

Run: `go test ./internal/config/`
Expected: `FAIL: TestParseEmptyObjectsAreAbsent`, with lines such as `empty add-on: Validate: addons.x.image is required`, `empty explicit panel: Validate: ingress.z.url is required` and `recursively empty add-on: Validate: addons.x.image is required`. `TestParseKeepsNumbers` and `TestParseRejectsTrailingData` pass on the old parser. They pin behavior the new decoder must keep (G9, M16).

- [ ] **Step 4: Implement.** Create `internal/config/empty.go`:

```go
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// isEmptyObject reports whether v is a JSON object whose every value is
// itself an empty object; {} is the base case. Scalars (false and "" too)
// and arrays never are (D14, D15).
func isEmptyObject(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for _, child := range m {
		if !isEmptyObject(child) {
			return false
		}
	}
	return true
}

// pruneEmpty deletes, at every level of m, each key whose value is an empty
// object. snapd leaves `{}` behind when the last child key is unset, and an
// empty map must mean "absent": no add-on, no panel (D14).
func pruneEmpty(m map[string]any) {
	for k, v := range m {
		if isEmptyObject(v) {
			delete(m, k)
			continue
		}
		if child, ok := v.(map[string]any); ok {
			pruneEmpty(child)
		}
	}
}

// withoutEmptyObjects is data with every empty object removed (pruneEmpty).
// UseNumber keeps numbers exactly as snapctl wrote them.
func withoutEmptyObjects(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after the config document")
	}
	pruneEmpty(doc)
	return json.Marshal(doc)
}
```

In `internal/config/config.go` (`Parse`), replace the `if len(data) > 0 { … }` block with:

```go
	if len(data) > 0 {
		pruned, err := withoutEmptyObjects(data)
		if err != nil {
			return Config{}, fmt.Errorf("parsing snap config: %w", err)
		}
		if err := json.Unmarshal(pruned, &raw); err != nil {
			return Config{}, fmt.Errorf("parsing snap config: %w", err)
		}
	}
```

- [ ] **Step 5: Run them and see them pass.**

Run: `go vet ./... && go test ./internal/... ./cmd/...`
Expected: all `ok`.

- [ ] **Step 6: Commit.**

```bash
gofmt -w internal/config/ internal/ingress/
git add internal/config/ internal/ingress/derive_test.go
git commit -m "fix(config): an empty object is absent at every level, so unsetting the last key never means 'panel wanted' (W-13 D14)"
```

---

### Task 12: `launcher prune-config`, run by the configure hook (D15, T14, T15)

**Files:**
- Modify: `internal/config/empty.go` (append `EmptyPaths`)
- Modify: `cmd/launcher/main.go` (`loadConfig` split; `runPruneConfig`; `run`; usage)
- Modify: `snap/hooks/configure`
- Test: `internal/config/empty_test.go` (create), `cmd/launcher/snapcraft_test.go` (append)

**Interfaces:**
- Consumes: `isEmptyObject` (Task 11).
- Produces: `func EmptyPaths(doc map[string]any) []string` (sorted, maximal); `func snapConfigJSON() ([]byte, error)`; `func runPruneConfig() error`; and the subcommand `launcher prune-config`.

- [ ] **Step 1: Write the failing tests.** Create `internal/config/empty_test.go`:

```go
package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// T14 (D15): the maximal empty-object paths, sorted; values are never listed.
func TestEmptyPaths(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"addons":{}}`, "addons"},
		{`{"docker":{},"ingress":{"a":{"url":"u"}}}`, "docker"},
		{`{"addons":{"x":{"ingress":{}},"y":{"image":"i"}}}`, "addons.x"},
		{`{"addons":{"x":{"ingress":{}}}}`, "addons"},
		{`{"addons":{"y":{"image":"i","ports":{},"ingress":{"title":"T"}}},"docker":{"devices":{}}}`, "addons.y.ports,docker"},
		{`{"docker":{"privileged":false,"timezone":"","extra-args":""},"image":{"channel":"stable"}}`, ""},
		{`{}`, ""},
	}
	for _, tc := range cases {
		var doc map[string]any
		if err := json.Unmarshal([]byte(tc.in), &doc); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(EmptyPaths(doc), ","); got != tc.want {
			t.Errorf("EmptyPaths(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

Append to `cmd/launcher/snapcraft_test.go`:

```go
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
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./internal/config/; go test ./cmd/launcher/ -run TestConfigureHookPrunesAfterValidate`
Expected: config: build failure, `undefined: EmptyPaths`. launcher: `FAIL … hook lacks a validate line, a guarded prune-config line or the restart (285, -1, 624)`.

- [ ] **Step 3: Implement.** In `internal/config/empty.go`, add `"sort"` to the imports and append:

```go

// EmptyPaths lists, sorted, the maximal dotted paths in doc whose value is an
// empty object (isEmptyObject): when every child of a map is empty, the map's
// own path is listed and its children are not (D15).
func EmptyPaths(doc map[string]any) []string {
	var out []string
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			p := prefix + k
			if isEmptyObject(v) {
				out = append(out, p)
				continue
			}
			if child, ok := v.(map[string]any); ok {
				walk(p+".", child)
			}
		}
	}
	walk("", doc)
	sort.Strings(out)
	return out
}
```

In `cmd/launcher/main.go`, split `loadConfig`. The new `loadConfig` is:

```go
func loadConfig() (config.Config, error) {
	data, err := snapConfigJSON()
	if err != nil {
		return config.Config{}, err
	}
	return config.Parse(data)
}

// snapConfigJSON is the snap's config as one JSON document, merged from the
// launcher's namespaces.
func snapConfigJSON() ([]byte, error) {
```

The rest of the old body follows it unchanged, except that its two error returns become `return nil, fmt.Errorf(…)` and its tail becomes `return data, nil`. Then add after it:

```go

// runPruneConfig unsets the empty maps snapd leaves behind when the last
// child key is unset, so `snap get -d` stays tidy (D15). Docker-free; the
// configure hook runs it after validate and ignores its exit status.
func runPruneConfig() error {
	data, err := snapConfigJSON()
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing snap config: %w", err)
	}
	paths := config.EmptyPaths(doc)
	if len(paths) == 0 {
		return nil
	}
	if err := exec.Command("snapctl", append([]string{"unset"}, paths...)...).Run(); err != nil {
		return fmt.Errorf("snapctl unset %s: %w", strings.Join(paths, " "), err)
	}
	fmt.Fprintln(os.Stderr, "pruned empty snap config:", strings.Join(paths, " "))
	return nil
}
```

At the top of `run`, before `cfg, err := loadConfig()`:

```go
	// "prune-config" works on the raw config document, not the parsed one.
	if cmd == "prune-config" {
		return runPruneConfig()
	}
```

In `main`'s usage line, append `|prune-config` after `validate`.

In `snap/hooks/configure`, insert after the `"$SNAP/bin/launcher" validate` line:

```sh

# Unset the empty maps snapd leaves behind when the last child key is unset
# (e.g. "addons": {}), so `snap get -d` stays tidy.  Cosmetic only: the parser
# already treats an empty map as absent, so pruning can never reject the set.
"$SNAP/bin/launcher" prune-config || true
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go vet ./... && go test ./internal/config/ ./cmd/launcher/ && sh -n snap/hooks/configure`
Expected: `ok` ×2, and `sh -n` silent.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/config/ cmd/launcher/
git add internal/config/ cmd/launcher/main.go cmd/launcher/snapcraft_test.go snap/hooks/configure
git commit -m "feat(config): prune-config unsets empty maps from the configure hook (W-13 D15)"
```

---

### Task 13: follow HA's log from a cursor, not `--tail 100` (D16, T16, T17)

**Files:**
- Modify: `internal/docker/docker.go` (`FollowLogs`; add `StartedAt`)
- Create: `cmd/launcher/logs.go`
- Modify: `cmd/launcher/main.go` (`runDaemon`)
- Test: `internal/docker/docker_test.go` (append; import `time`), `cmd/launcher/logs_test.go` (create)

**Interfaces:**
- Produces: `func (c *Client) FollowLogs(name string, since time.Time) error` (the signature changes; its only caller is `runDaemon`); `func (c *Client) StartedAt(name string) (time.Time, error)`; `func logsSince(cursor, startedAt *time.Time, daemonStart time.Time) time.Time`; `func logsCursorPath() string`; `func readCursor(path string) *time.Time`; `func writeCursor(path string, t time.Time)`; `func followHALogs(cli *docker.Client, cursorPath string, daemonStart time.Time, now func() time.Time) error`.

- [ ] **Step 1: Write the failing tests.** In `internal/docker/docker_test.go`, add `"time"` to the imports and append:

```go
// T17 (D16): follow from a point in time, never a fixed --tail.
func TestFollowLogsSince(t *testing.T) {
	f := &fakeRunner{}
	since := time.Date(2026, 11, 14, 10, 0, 0, 500_000_000, time.FixedZone("CET", 3600))
	if err := NewWithRunner(f).FollowLogs("homeassistant", since); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.calls[0], " "); len(f.calls) != 1 || got != "logs -f --since 2026-11-14T09:00:00.5Z homeassistant" {
		t.Errorf("FollowLogs called %q, want [logs -f --since 2026-11-14T09:00:00.5Z homeassistant]", f.calls)
	}
}

func TestStartedAt(t *testing.T) {
	const key = "inspect -f {{.State.StartedAt}} homeassistant"
	f := &fakeRunner{outputs: map[string]string{key: "2026-11-13T17:06:12.123456789Z"}}
	got, err := NewWithRunner(f).StartedAt("homeassistant")
	if want := time.Date(2026, 11, 13, 17, 6, 12, 123456789, time.UTC); err != nil || !got.Equal(want) {
		t.Errorf("StartedAt = %v, %v; want %v", got, err, want)
	}
	f = &fakeRunner{errs: map[string]error{key: errors.New("No such container")}}
	if _, err := NewWithRunner(f).StartedAt("homeassistant"); err == nil {
		t.Error("an inspect failure must be returned")
	}
}
```

Create `cmd/launcher/logs_test.go`:

```go
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
```

- [ ] **Step 2: Run them and see them fail.**

Run: `go test ./internal/docker/; go test ./cmd/launcher/ -run 'TestLogsSince|TestCursorFile|TestFollowHALogs'`
Expected: docker: build failure, `too many arguments in call to NewWithRunner(f).FollowLogs`. launcher: `undefined: logsSince`, `undefined: readCursor`, …

- [ ] **Step 3: Implement.** In `internal/docker/docker.go`, replace the three-line `FollowLogs` function (after `Stop`) with:

```go

// FollowLogs streams the container's log from since onwards (D16). No
// --tail: a fixed line count replays up to an hour of old lines into the
// journal on a quiet HA.
func (c *Client) FollowLogs(name string, since time.Time) error {
	return c.r.Stream("logs", "-f", "--since", since.UTC().Format(time.RFC3339Nano), name)
}

// StartedAt is when the container last started (docker's State.StartedAt).
func (c *Client) StartedAt(name string) (time.Time, error) {
	out, err := c.r.Output("inspect", "-f", "{{.State.StartedAt}}", name)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, out)
}

```

Create `cmd/launcher/logs.go`:

```go
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

// writeCursor records t (RFC3339Nano, UTC). Best-effort: a failed write only
// costs a capped replay on the next start.
func writeCursor(path string, t time.Time) {
	if path == "" {
		return
	}
	_ = os.WriteFile(path, []byte(t.UTC().Format(time.RFC3339Nano)+"\n"), 0o644)
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
```

In `cmd/launcher/main.go` (`runDaemon`), make `daemonStart := time.Now()` the first line. Then replace everything from `sigs := make(chan os.Signal, 1)` to the end of the function with:

```go
	cursor := logsCursorPath()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		// First, before anything that can be slow or lose the exit race
		// (D16; W-10 owns the rest of this handler and keeps this line first).
		writeCursor(cursor, time.Now())
		_ = cli.Stop(dockerargs.ContainerName)
		os.Exit(0)
	}()
	return followHALogs(cli, cursor, daemonStart, time.Now)
}
```

- [ ] **Step 4: Run them and see them pass.**

Run: `go vet ./... && go test ./internal/docker/ ./cmd/launcher/`
Expected: `ok` ×2.

- [ ] **Step 5: Commit.**

```bash
gofmt -w internal/docker/ cmd/launcher/
git add internal/docker/ cmd/launcher/logs.go cmd/launcher/logs_test.go cmd/launcher/main.go
git commit -m "fix(daemon): follow HA's log from a cursor so a restart does not replay old lines (W-13 D16)"
```

---

### Task 14: README (D2, D3, D7, D9, D11–D13, D14, D17)

**Files:**
- Modify: `README.md` (the `### Notes` list under `## Add-ons`; two new sections)

- [ ] **Step 1: Write the content.** In `README.md`, replace the `### Notes` list (and its existing **Bridge mode** bullet) with:

```markdown
### Notes

- **Environment variables**: Uppercase names require the JSON-document form: `sudo snap set home-assistant addons.x.environment='{"TZ":"Europe/Oslo"}'`
- **Panels**: An add-on gets a sidebar panel when it has at least one `ingress.*` key, e.g. `addons.nodered.ingress.title="Node-RED"`. An empty map counts as unset, at every level.
- **Restarts**: hass_ingress reads its YAML only at startup, so Home Assistant restarts (30–60 s) whenever the panels change. Several add-ons set in **one** `snap set` cost one restart; separate `snap set` calls cost one each. `home-assistant.ingress sync` restarts only when the panels changed; `home-assistant.ingress sync --force` always writes the panel file and restarts.
- **Anonymous volumes**: Recreating or removing an add-on deletes its anonymous volumes (`docker rm -v`). Named volumes (`ha-addon-<name>-data`, `homeassistant-config`) and bind mounts are never deleted. An image `VOLUME` that neither `data-dir` nor a `volumes.*` entry covers at exactly that path is anonymous: the reconcile warns about it, and its content is discarded on the next recreate or removal. The same goes for an anonymous `-v /path` in `docker.extra-args`. Volumes orphaned by earlier releases are not cleaned up: list them with `docker volume ls -qf dangling=true`, check each with `docker volume inspect`, and remove only the ones you recognise.
- **Devices**: The `devices.*` namespace is reserved for a later release.
- **Backups**: Add-on volumes are not included in `backup` or `rollback` snapshots.

### Bridge mode (model B)

`docker.network=ha-addons` moves Home Assistant onto the ha-addons bridge. Panels switch to container-name URLs and `:8123` is auto-published, but discovery, mDNS and Bluetooth degrade. Home Assistant's `localhost` is now its own container:

- Explicit panels whose URL host is `localhost`, `127.0.0.1` or `[::1]` are rendered with `host.docker.internal` instead. Home Assistant runs with `--add-host=host.docker.internal:host-gateway` (Docker 20.10 or later). Snap config is not changed, so switching back to `docker.network=host` restores the original URL. The host service must listen beyond `127.0.0.1` for the panel to work.
- Integrations set up with localhost URLs (e.g. zwave_js `ws://localhost:3000`) lose their connection. The snap cannot change them; point them at `host.docker.internal` in Home Assistant.
- A reverse proxy on the host (e.g. cloudflared) may reach Home Assistant from the bridge gateway (e.g. 172.18.0.1) instead of 127.0.0.1. If Home Assistant answers 400, add the ha-addons subnet to `http.trusted_proxies`. Find the subnet with `docker network inspect ha-addons -f '{{(index .IPAM.Config 0).Subnet}}'`.

### Firewall (ufw) on Ubuntu Core

Reported behaviour, not reproduced in this repository: with the `ufw` snap enabled, ufw uses legacy iptables with a FORWARD policy of DROP, while docker's rules live in nf_tables. Packets have to pass both, so bridge add-ons lose outbound traffic and their published ports. Allow routed traffic with `sudo ufw default allow routed`, or more narrowly with `sudo ufw route allow in on br-<id>` and `sudo ufw route allow out on br-<id>`, where `<id>` is the first 12 characters of `docker network inspect ha-addons -f '{{.Id}}'`.
```

- [ ] **Step 2: Verify that every required item is there.**

Run:
```bash
for s in 'docker rm -v' 'dangling=true' 'one** `snap set`' 'sync --force' 'host.docker.internal:host-gateway' 'ws://localhost:3000' 'trusted_proxies' 'ufw default allow routed' 'at least one `ingress.*` key'; do printf '%s: ' "$s"; grep -c -F -- "$s" README.md; done
```
Expected: every line ends in `1`.

- [ ] **Step 3: Commit.**

```bash
git add README.md
git commit -m "docs(readme): anonymous volumes, restarts, model B localhost and proxies, ufw (W-13 D2, D3, D9, D11–D13, D17)"
```

---

### Task 15: final verification and mutations M1–M16

- [ ] **Step 1: Static checks.**

Run: `test -z "$(gofmt -l .)" && go vet ./... && go build ./... && echo OK`
Expected: `OK`.

- [ ] **Step 2: Full suite, under the lock.**

Run: `flock -w 3600 /tmp/claude-1000/chim-suites.lock go test ./...`
Expected: every package `ok`. If the lock wait times out, say so; do not run the suite without the lock.

- [ ] **Step 3: Mutations.** Apply each edit, run the named tests, see **each named test** reported as `--- FAIL`, then revert with `git checkout -- <file>`. Run with `go test -count=1 <pkg> -run '^(<names>)$'`.

| M | File | Edit | Must go red |
|---|---|---|---|
| M1 | `internal/docker/docker.go` | `"rm", "-f", "-v", name` → `"rm", "-f", name` | `TestRemoveDeletesAnonymousVolumes` |
| M2 | `cmd/launcher/volumes.go` | `if !covered[path.Clean(p)] {` → a prefix match: `hit := false; for c := range covered { if strings.HasPrefix(p, c) { hit = true } }; if !hit {` | `TestUncoveredVolumes` |
| M3 | `internal/reconcile/reconcile.go` | move the `if s.BeforeSwap != nil { s.BeforeSwap() }` block from after the pull to the first line inside `if force \|\| have != s.WantHash {` | `TestBeforeSwapRunsBetweenPullAndRemove`, `TestBeforeSwapOnlyOnRecreate` |
| M4 | `internal/reconcile/reconcile.go` | add the same block as the first line inside `if !exists {` | `TestBeforeSwapOnlyOnRecreate` |
| M5 | `cmd/launcher/main.go` | delete `specs[0].BeforeSwap = func() { preSwapIngress(cli, cfg, os.Stderr) }` | `TestReconcileSetWritesIngressBeforeSwap` |
| M6 | `internal/ingress/derive.go` | in `Decide`, `return true, false` → `return true, true` | `TestDecide` |
| M7 | `cmd/launcher/ingress.go` | `force = true` → `force = false` | `TestIngressSyncCompareFirst`, `TestIngressSyncChecksMode` |
| M8 | `cmd/launcher/ingress.go` | in `syncIngressFiles`, `if len(entries) > 0 {` → `if write && len(entries) > 0 {` | `TestIngressSyncCompareFirst` |
| M9 | `internal/dockerargs/dockerargs.go` | in `SpecHash`, `if c.Network == AddonNetwork {` (the `addhost=` block) → `if true {` | `TestGoldenModelA` |
| M10 | `internal/ingress/derive.go` | in `MergedEntries`, `if c.Network == dockerargs.AddonNetwork {` → `if true {` | `TestMergedEntriesModelBLoopback` |
| M11 | `internal/config/modelb.go` | replace the `switch u.Hostname()` with `if h := u.Hostname(); !strings.Contains(h, "localhost") && h != "127.0.0.1" && h != "::1" { return raw, false }` (import `strings`) | `TestLoopbackToHostGateway` |
| M12 | `internal/config/config.go` | `pruned, err := withoutEmptyObjects(data)` → `pruned, err := data, error(nil)` | `TestParseEmptyObjectsAreAbsent` |
| M12a | `internal/config/empty.go` | `isEmptyObject`'s `for` loop and `return true` → `return len(m) == 0` | `TestParseEmptyObjectsAreAbsent` |
| M12b | `cmd/launcher/ingress.go` | in `ensureInclude`'s read-error branch, replace the warning and `return false, nil` with `current, present = "", true` | `TestEnsureIncludeHardenedRead` |
| M13 | `internal/config/empty.go` | in `EmptyPaths`, `if isEmptyObject(v) {` → `if e, ok := v.(map[string]any); ok && len(e) == 0 {` | `TestEmptyPaths` |
| M14 | `cmd/launcher/logs.go` | delete `if since.After(daemonStart) { since = daemonStart }` | `TestLogsSince` |
| M15 | `cmd/launcher/logs.go` | `since := daemonStart.Add(-logsReplayFloor)` → `var since time.Time` | `TestLogsSince` |
| M16 | `internal/config/empty.go` | delete the `dec.Token()` trailing-data check (G9) | `TestParseRejectsTrailingData` |

Expected: all 18 red, and `git status --porcelain` empty afterwards.

- [ ] **Step 4: Report.** Give the orchestrator the SHAs, the suite output, and the mutation table with results. Nothing to commit unless a fix was needed. A fix gets its own commit, and Steps 1–3 run again.

---

## Device acceptance (the orchestrator's, after deploy; not run by this plan)

Spec §"Device acceptance on lav918". Before starting, record the baseline HA `StartedAt`, `docker volume ls -qf dangling=true | wc -l`, `snap get -d home-assistant`, and the ingress routes. Restore everything at the end.

- **A1 (F-a):** `addons.mqtest` (`eclipse-mosquitto:2`, `data-dir=/mosquitto/config`). The journal shows the D3 warning naming `/mosquitto/data, /mosquitto/log`. After unset, the dangling count equals the baseline, and `ha-addon-mqtest-data` and `homeassistant-config` are still present.
- **A2 (F-a recreate):** change one env var. The add-on is recreated and the dangling count is unchanged.
- **A3 (F-b):** `snap set docker.network=ha-addons`. StartedAt changes exactly once; the journal has `reconcile homeassistant: recreated` and no `restarting Home Assistant…`. Model B URLs are in `snap-ingress.yaml`, and `/api/ingress/<panel>/` returns 302. Revert with the same check, and record `State.FinishedAt` before and after (D4a). A `note: Home Assistant is not running…` line marks the D5 fallback.
- **A4 (F-c):** `ingress sync` on unchanged panels leaves StartedAt unchanged and prints the "panels unchanged" line. `ingress sync --force` restarts HA once.
- **A5 (F-c header):** change only the header in the container, then `home-assistant.reconcile`. The file is rewritten and StartedAt is unchanged.
- **A6 (F-d):** under model B, the zwave panel URL is `http://host.docker.internal:8091`. Record the curl code from HA and the `ExtraHosts` entry. Also record where the D12 warning appeared (the terminal, or only the journal).
- **A7 (F-e, optional, read-only):** the source address HA logs for a request through cloudflared, and cloudflared's origin URL.
- **A8 (F-f):** after the last unsets, `snap get -d` has no `addons` and no `docker` key (D15; record whether `snapctl unset` in the hook persisted). Then `snap unset addons.x.ingress.title`: the panel is gone and the set succeeded.
- **A9 (F-h):** `snap restart home-assistant.daemon` shows no HA line older than the previous stop. After `docker restart homeassistant` and then a daemon restart, HA's startup lines appear once.
- **A10 (regression):** refresh under model A. `reconcile homeassistant: none`, StartedAt unchanged, the header rewritten without a restart, and zwave_js connected.
