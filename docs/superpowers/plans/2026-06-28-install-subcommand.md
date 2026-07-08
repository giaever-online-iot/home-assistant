# `install` Subcommand Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `home-assistant.install <target>` — an extensible, idempotent installer for custom integrations into `/config`, starting with HACS.

**Architecture:** A target→recipe registry in `cmd/launcher`. The subcommand resolves the target, probes idempotency via `docker exec … test -d`, runs the recipe (which execs the install inside the running container, writing to the `/config` volume), optionally restarts the container, and prints activation guidance. No new privileges — reuses the existing `docker.Client`.

**Tech Stack:** Go (stdlib + lipgloss already present), the existing `internal/docker` client, snapcraft (`core26`, strict).

## Global Constraints

- Module `github.com/giaever-online-iot/home-assistant`; `go 1.23`; strict confinement; reuse `dockerargs.ContainerName` ("homeassistant").
- The launcher only execs `snapctl` + the docker CLI (over the existing `docker` plug). No new plugs.
- `install` requires a running container; it goes through the existing `requireDocker()` + `preflightContainer()` gates.
- Recipes are data + a function; the docker interactions are behind `docker.Client` so the pure logic is unit-testable with a fake.
- v1 ships exactly one target: `hacs`. Unknown target → list targets, exit non-zero.

---

### Task 1: docker client — `Restart` + `ExecCheck`

**Files:**
- Modify: `internal/docker/docker.go`
- Test: `internal/docker/docker_test.go`

**Interfaces:**
- Produces: `func (c *Client) Restart(name string) error` (`docker restart <name>`, streamed); `func (c *Client) ExecCheck(name string, cmd ...string) (bool, error)` — runs `docker exec <name> <cmd…>`, returns `(true,nil)` on exit 0, `(false,nil)` on non-zero exit, error only on a docker-invocation failure.

- [ ] **Step 1: Write failing tests** for `Restart` (asserts args `["restart","homeassistant"]` via a fake Runner) and `ExecCheck` (exit-0 → true; non-zero → false,nil).
- [ ] **Step 2: Run them — expect FAIL** (methods undefined).
- [ ] **Step 3: Implement** `Restart` (`c.r.Stream("restart", name)`) and `ExecCheck` (use `Capture`/`Run`-style that distinguishes exit code from invocation error).
- [ ] **Step 4: Run tests — expect PASS.**
- [ ] **Step 5: Commit.**

### Task 2: install registry + HACS recipe

**Files:**
- Create: `cmd/launcher/install.go`
- Test: `cmd/launcher/install_test.go`

**Interfaces:**
- Produces: `type Recipe struct { Name, Dest, Activation string; Install func(*docker.Client) error }`; `var installRegistry map[string]Recipe`; `func resolveTarget(name string) (Recipe, bool)`; `func installTargets() []string` (sorted names, for the "unknown target" message).
- The `hacs` recipe: `Dest = "/config/custom_components/hacs"`, `Install` = `c.Exec(ContainerName, "bash", "-c", "wget -O - https://get.hacs.xyz | bash")`, `Activation` = the Settings→Add-Integration→HACS + GitHub-device-auth guidance.

- [ ] **Step 1: Write failing tests** — `resolveTarget("hacs")` returns the recipe; `resolveTarget("nope")` returns `false`; `installTargets()` includes `"hacs"`; the hacs recipe's `Dest` is the expected path.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** the type + registry + resolvers + the hacs recipe.
- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Commit.**

> Note (security): v1 uses the documented `get.hacs.xyz` script. A hardening follow-up (Task 6) swaps `Install` to fetch the pinned HACS release zip + verify checksum, then `docker cp` it in — the `Install func` boundary makes this a drop-in replacement.

### Task 3: `install` subcommand wiring

**Files:**
- Modify: `cmd/launcher/main.go` (the `run()` switch + a `runInstall` helper)
- Test: `cmd/launcher/install_test.go` (extend — flag parsing is pure)

**Interfaces:**
- Consumes: Task 1 (`Restart`, `ExecCheck`), Task 2 (registry).
- Produces: `case "install":` → `runInstall(cli, args)`; `func parseInstallArgs([]string) (target string, noRestart, force bool, err error)`.

- [ ] **Step 1: Write failing test** for `parseInstallArgs` (e.g. `["hacs","--no-restart"]` → `("hacs",true,false,nil)`; `["--force","hacs"]` → `("hacs",false,true,nil)`; `[]` → error).
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** `parseInstallArgs` and `runInstall`: resolve target (unknown → list `installTargets()`, error); if `ExecCheck(test -d Dest)` and not `--force` → print "already installed", return; else run `recipe.Install`; unless `--no-restart` → `Restart(ContainerName)`; print `recipe.Activation`. Wire `case "install"` into `run()` (after `requireDocker()` + a `preflightContainer()` call). Update the usage string.
- [ ] **Step 4: Run — expect PASS** (+ `go build ./...`).
- [ ] **Step 5: Commit.**

### Task 4: snap surface

**Files:**
- Modify: `snap/snapcraft.yaml`

- [ ] **Step 1:** Add an `install` app: `command: bin/launcher install`, `plugs: [docker]`. Update the snap `description`'s usage block to mention `snap … install hacs`.
- [ ] **Step 2:** `yq '.apps.install' snap/snapcraft.yaml` shows the app; `go build ./...` still green.
- [ ] **Step 3: Commit.**

### Task 5: build + on-device smoke

- [ ] **Step 1:** Push (matrix build → revision). On the Pi: `sudo home-assistant.install hacs` → expect the streamed HACS download, a restart, and the activation guidance; confirm `/config/custom_components/hacs/` exists (`docker exec homeassistant ls /config/custom_components/`).
- [ ] **Step 2:** Re-run → expect "already installed". `--force` re-runs.
- [ ] **Step 3:** In HA UI: add the HACS integration + GitHub device-auth → confirm HACS loads.

### Task 6 (follow-up, optional): harden HACS fetch

- [ ] Replace the `hacs` recipe's `Install` with: GitHub API → latest release → download `hacs.zip` asset → verify checksum → `docker cp` extracted tree into the volume. Keep the same `Recipe` shape so nothing else changes. Test the (pure) release-asset selection + the docker-cp arg building.
