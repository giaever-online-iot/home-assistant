# `install hass-ingress`: pinned 1.4.0 release, Marketplace pointer, YAML-mode check: design spec

**Date:** 2026-11-13 · **Wish:** W-12 · **Class:** bounded
**Status:** Proposed (for review).
**Builds on:** W-5, `feat/hacs-marketplace` @ 2b6a7cc (PR #9): `Recipe.Gate`,
`Recipe.Installed`, `parseHAVersion`, `marketplaceBuiltIn`, `fakeRunner`.
**Inputs:** research report `docs/superpowers/research/2026-11-12-hass-ingress-marketplace.md`
(commit f15e032, cherry-picked onto this branch). Joachim's answer to W-12 Q1 is
option 1: a pinned 1.4.0 release with sha256, a Marketplace pointer for updates,
and `ingress sync` warning outside YAML mode.

## Goal

Today `home-assistant.install hass-ingress` copies whatever is on
`lovelylain/hass_ingress`'s `main` branch, with no version and no integrity
check. The copy never updates. That is how lav918 ended up stuck on 1.3.0 in
August.

After this change:

1. `install hass-ingress` installs **one pinned release**, 1.4.0's `ingress.zip`.
   It checks the zip's size and sha256 **before anything under `/config`
   changes**, fails closed on a mismatch, and swaps the new directory in with a
   rename, so a failure leaves the old copy exactly as it was.
2. It knows which version is already installed. It never silently downgrades a
   newer copy, such as a 1.4.x installed from the Marketplace.
3. On Home Assistant 2026.11 or later it points at **Settings → Marketplace** for
   updates. The launcher never calls the Marketplace API and never accepts its
   risk notice (W-12 decision 1).
4. `home-assistant.ingress sync`, and PR #6's auto-sync once it is on `main`,
   warn when hass_ingress's config entry is not in YAML mode. In any other mode,
   1.4.0 silently ignores the snap's `snap-ingress.yaml`.

## Settled facts (with evidence)

**F1. Tools in the HA image.** On 2026.11.1, both arm64 and amd64 ship busybox
`unzip`, `sha256sum`, `wget`, `mktemp` and `mv`, plus real `bash`, `curl` and
`python3` (3.14).
*Evidence:* the laptop has no docker, so I read the published image from ghcr.io
instead. I fetched the index for `ghcr.io/home-assistant/home-assistant:2026.11.1`,
then the arm64 manifest `sha256:9c7c84f9…`, and listed every layer (zstd tar):
- layer 1 is `alpine-minirootfs-3.24.3`, with `usr/bin/unzip -> /bin/busybox`,
  `usr/bin/sha256sum -> /bin/busybox`, `usr/bin/wget -> /bin/busybox`,
  `bin/mktemp -> /bin/busybox` and `bin/mv -> /bin/busybox`;
- layer 2 has `bin/bash` and `usr/bin/curl`;
- layer 3 has `usr/local/bin/python3 -> python3.14`;
- across all 22 layers, the only whiteouts are under `site-packages`, so nothing
  later removes these tools.

The amd64 manifest `sha256:1e25a28c…` has the same symlinks in layer 1 and
`bash`/`curl` in layer 2. Busybox `wget` already fetches HTTPS from GitHub
(including the redirect to the CDN): today's recipe and lav918's
`install hass-ingress --force` on 2026-09-02 used it.

**F2. A fresh 1.4.0 with `ingress: !include snap-ingress.yaml` already in
configuration.yaml gets a YAML-mode entry, headless.** At bootstrap HA loads
every top-level config key. hass_ingress's `async_setup` sees no entry and
`ingress` in config, so it starts an `import` flow (1.4.0 `__init__.py`
L107-L118). `async_step_import` sets `mode = yaml` and creates the entry
(`config_flow.py` L650-L654, L599-L616). Afterwards the user-source form aborts
with `already_configured` (L540-L542), so the UI's default of "Local UI" (L556-L561)
can't create a second entry (`single_config_entry: true`).
*Caveat (from code only, not run on a device):* if someone adds Ingress
through the UI **before** any start that has the `ingress:` key, the form
creates the entry, and its default is `ui`. In the snap's flow the key arrives
with `ingress sync`, so `install` → UI setup → `sync` is the one sequence that
lands in UI mode. The mode check (D10) exists for exactly that sequence.

**F3. Upgrading 1.3.2 → 1.4.0 keeps the entry in YAML mode.** 1.3.2's flow is
`VERSION = 1`, and its `async_step_import` stores `mode = yaml` (1.3.2
`config_flow.py` L17, L86-L87). Its `async_setup` creates that import entry
whenever none exists (1.3.2 `__init__.py` L108-L123). 1.4.0's
`async_migrate_entry` moves v1 to v2 and copies `data` unchanged. It adds only
`ui_configured`, and only if v1 had panels in options or was already `ui`
(1.4.0 `__init__.py` L213-L229). In YAML mode, `async_setup_entry` re-reads the
YAML (L121-L133). lav918 (1.3.2, YAML) therefore stays YAML. The device
acceptance (A4) confirms this on the box.

**F4. Where the mode lives.** The file is `/config/.storage/core.config_entries`
(HA 2026.11.1 `config_entries.py`: `STORAGE_KEY = "core.config_entries"` L144,
`_data_to_save` L3115-L3120, `as_dict` L1351-L1368). Its shape:

```json
{"version": 1, "minor_version": 5, "key": "core.config_entries",
 "data": {"entries": [
   {"domain": "ingress", "data": {"mode": "yaml"}, "version": 2, "source": "import",
    "disabled_by": null, "options": {}, "subentries": [], "...": "..."}]}}
```

The domain is `ingress`, and the mode is `entries[i].data.mode`, one of `"ui"`,
`"agent"` or `"yaml"` (1.4.0 `const.py` L88-L91, `CONF_MODE` = HA's `"mode"`).
HA writes the file atomically with a 1 s save delay (`SAVE_DELAY = 1`, L148), so
a read never sees half a file.

**F5. The 1.4.0 asset.** I downloaded it myself on 2026-11-13: 61,359 bytes,
sha256 `7b2255fa3f25b97d465c17336cae8188bcc4dfefd52677682f084c1e39a54cac`. That
matches the research report and the orchestrator's re-check. The zip's root holds
`manifest.json` (`"domain": "ingress"`, `"version": "1.4.0"`), `__init__.py`,
`client/`, `www/` and `translations/`, with no `custom_components/ingress/`
prefix.

## Scope

**IN**
- `cmd/launcher/hassingress.go` (new): the pin constants, the install-script
  builder, `hassIngressGate`, and manifest-version parsing and comparison.
- `cmd/launcher/install.go`: the hass-ingress recipe uses the pinned script and
  `hassIngressGate`; new Installed and Activation wording; `Gate` gets a
  `force bool` parameter (D6).
- `cmd/launcher/marketplace.go`: `hacsGate` takes the new signature and ignores
  `force` (W-5's D5 is unchanged).
- `internal/ingress/mode.go` (new): a pure parser of `core.config_entries`, and
  the warning text.
- `cmd/launcher/ingressmode.go` (new): `warnIngressMode(cli, w)`, the I/O
  wrapper. `cmd/launcher/ingress.go` gets **one** call line in `sync`.
- After PR #6 is on `main` (D12): the same call in `applyIngress`, which covers
  the reconcile auto-sync.
- Tests: `cmd/launcher/hassingress_test.go`,
  `cmd/launcher/hassingress_script_test.go`, `internal/ingress/mode_test.go`,
  and updates to `install_test.go` (listed under Testing).

**OUT**
- Any Marketplace API call, token handling, or acceptance of the risk notice
  (W-12 decision 1).
- Telling a Marketplace-managed copy from ours by reading Marketplace storage.
  Version comparison is enough (D5).
- Changing an entry's mode, or editing `.storage`. The launcher only reads it.
- Warning about a *disabled* Ingress entry. Disabling is an explicit user act,
  not the silent-default trap that the mode check exists for.
- `snap/snapcraft.yaml` and `README.md`. Neither describes how hass-ingress is
  fetched, so neither changes.
- `internal/docker`. `Exec`, `ExecCheck` and `ReadFile` cover everything.

## Behaviour

### Pin constants (`cmd/launcher/hassingress.go`)

```go
const (
	hassIngressPinVersion = "1.4.0"
	hassIngressPinURL     = "https://github.com/lovelylain/hass_ingress/releases/download/1.4.0/ingress.zip"
	hassIngressPinSize    = 61359
	hassIngressPinSHA256  = "7b2255fa3f25b97d465c17336cae8188bcc4dfefd52677682f084c1e39a54cac"
)
```

### Order of steps for `install hass-ingress`

```
parseInstallArgs → resolveTarget → preflightContainer
  → hassIngressGate(force)        ← NEW for this recipe
  → dir probe (unless --force)    ← unchanged generic step; reached only when the gate proceeds
  → install script → restart (unless --no-restart) → Activation
```

### `hassIngressGate(c, force)`

1. **Pointer.** Read `/config/.HA_VERSION` (`ReadFile`), then call `parseHAVersion`
   and `marketplaceBuiltIn`. On 2026.11 or later, **or when the version is
   unknown** (as in W-5's D3), the notice starts with the Marketplace pointer.
   Below 2026.11 it doesn't.
2. **Installed version.** Read
   `/config/custom_components/ingress/manifest.json` (`ReadFile`; `""` when it
   is absent). Decode it as JSON and take `version`. Then call
   `parsePluginVersion(v)`.
3. Decide using this table. "unknown" means the file is absent or unreadable,
   is not JSON, has no `version`, or has a version that does not parse.

| Installed | no `--force` | `--force` |
|---|---|---|
| unknown | proceed. The dir probe then installs if the dir is absent, or prints `Installed` (unknown-version text) if it is present. | proceed (replace) |
| older than the pin | **no-op**, status *older* | proceed (upgrade) |
| equal to the pin | **no-op**, status *equal* | proceed (reinstall, a repair) |
| newer than the pin | **no-op**, status *newer* | **no-op**, status *refuse-downgrade*, exit 0 |

The gate never returns an error, because both reads go through `ReadFile`,
which never errors. Every no-op leaves no exec, no restart and no write. The
notice is the status line(s) first, then the pointer if there is one.

### Version parsing (pure)

`parsePluginVersion(raw string) (v [3]int, ok bool)`:

1. Trim whitespace, then split on `.`.
2. There must be 2 or 3 fields. Every field except the last must be all ASCII
   digits. The last field must **start** with at least one digit, and any
   non-digit suffix is dropped (`0b1` becomes 0).
3. A missing third field is 0.

`comparePluginVersion(a, b [3]int) int` compares numerically, field by field.

| raw | parsed | vs 1.4.0 |
|---|---|---|
| `1.4.0` | 1,4,0 | equal |
| `1.3.2` | 1,3,2 | older |
| `1.10.0` | 1,10,0 | newer (numeric, not string) |
| `1.4` | 1,4,0 | equal |
| `1.4.2` | 1,4,2 | newer |
| `1.5.0b1` | 1,5,0 | newer |
| `""`, `v1.4.0`, `1`, `1.x.0`, `1.4.0.1`, `.4.0` | — | unknown |

### Install script (`hassIngressScript(configDir, tmpDir string) string`)

In production it is called as `hassIngressScript("/config", "/tmp")` and run as
`docker exec homeassistant bash -c <script>`. The directories are parameters
only so the script test can run the real script on the host (D9). Steps:

```bash
set -eu
work=$(mktemp -d "<tmp>/hass-ingress.XXXXXX")
stage=""
cleanup() { rm -rf "$work"; [ -z "$stage" ] || rm -rf "$stage"; }
trap cleanup EXIT
wget -qO "$work/ingress.zip" "<URL>" || { echo "hass-ingress: download failed: <URL>" >&2; exit 1; }
size=$(wc -c < "$work/ingress.zip")
[ "$size" -eq <SIZE> ] || { echo "hass-ingress: size mismatch: got $size bytes, want <SIZE>; nothing changed" >&2; exit 1; }
echo "<SHA256>  $work/ingress.zip" | sha256sum -c - >/dev/null 2>&1 \
  || { echo "hass-ingress: sha256 mismatch for <URL>; nothing changed" >&2; exit 1; }
mkdir "$work/ingress"
unzip -q "$work/ingress.zip" -d "$work/ingress"
[ -f "$work/ingress/manifest.json" ] || { echo "hass-ingress: manifest.json not at the zip root; nothing changed" >&2; exit 1; }
# --- nothing under <config> has been touched up to here ---
mkdir -p "<config>/custom_components"
stage=$(mktemp -d "<config>/.snap-hass-ingress.XXXXXX")
cp -r "$work/ingress" "$stage/new"
if [ -e "<config>/custom_components/ingress" ]; then
  mv "<config>/custom_components/ingress" "$stage/old"
fi
if ! mv "$stage/new" "<config>/custom_components/ingress"; then
  [ ! -e "$stage/old" ] || mv "$stage/old" "<config>/custom_components/ingress"
  echo "hass-ingress: could not move the new copy into place; previous copy restored" >&2; exit 1
fi
```

The values in `<…>` are formatted from the constants. Each one is quoted for
the shell with a small `shellQuote` helper (single quotes), so a path with a
space can't split. Properties:

- **Verify before touching `/config` (D2).** The download, size, sha256 and
  unzip all happen in the container's `/tmp`. `<config>` is first written only
  after every check has passed.
- **Fail closed.** Any failed check exits non-zero before the staging dir
  exists. The `EXIT` trap removes the work dir, and the existing
  `custom_components/ingress` is untouched. `runInstall` returns
  `installing hass-ingress: exit status 1` and does not restart.
- **Swap with a rename (D3).** The staging dir sits directly under `<config>`,
  the same filesystem as `custom_components`. The swap is two `rename(2)`s
  (old aside, new in), with a rollback if the second fails. Busybox `mv` has no
  `RENAME_EXCHANGE`, so there is a window of microseconds with no
  `custom_components/ingress`. HA is restarted straight afterwards anyway. The
  trap deletes the old copy only after a successful swap.
- The staging dir is `/config/.snap-hass-ingress.*`, not under
  `custom_components`, so HA's custom-component scan never sees a half-made
  second `ingress` dir, even if the launcher is killed mid-way.

### Messages (exact text, pinned by tests)

Marketplace pointer (`hassIngressMarketplacePointer`):

```
To get updates, install Ingress from Home Assistant → Settings → Marketplace (2026.11 and later; accept the one-time risk notice first). It replaces this copy and keeps it up to date. If Home Assistant asks how to configure Ingress, choose YAML.
```

Status lines (`%s` = installed version, the pin is `1.4.0`):

- *older*: `hass-ingress %s is installed; this snap pins 1.4.0. Run "home-assistant.install hass-ingress --force" to upgrade.`
- *equal*: `hass-ingress 1.4.0 (the pinned version) is already installed.`
- *newer*: `hass-ingress %s is installed, newer than the 1.4.0 this snap pins (probably from the Marketplace); leaving it alone.`
- *refuse-downgrade*: `hass-ingress %s is installed, newer than the 1.4.0 this snap pins; not downgrading. To force the pinned version, remove /config/custom_components/ingress first.`

`Installed` (generic probe, unknown version):
`hass-ingress is already installed, but its version could not be read. Run "home-assistant.install hass-ingress --force" to replace it with the pinned 1.4.0.`

`Activation`:

```

hass_ingress 1.4.0 installed (pinned release, sha256 verified). Next:
  1. Define panels:  snap set home-assistant ingress.<name>.url=http://localhost:<port> ingress.<name>.title="<Title>"
  2. Apply:          home-assistant.ingress sync
Each panel then appears in the Home Assistant sidebar.
```

The first and last lines are new. Steps 1 and 2 are unchanged.

### `--force` and `--no-restart`

- `--force` now means "install the pinned version over what is there", **except
  over a newer copy** (D5). It still skips the generic dir probe.
- `--no-restart` is unchanged.

### How to bump the pin (procedure; recorded in a comment above the constants)

1. `gh api repos/lovelylain/hass_ingress/releases/tags/<v> --jq '.assets[] | select(.name=="ingress.zip") | {size, digest, browser_download_url}'`
2. Download the asset yourself, then run `sha256sum` and `wc -c`. They must match
   the API's `digest` and `size`.
3. `unzip -l`: `manifest.json` must be at the root, with `"version": "<v>"`.
4. Diff the release's `CONFIG_SCHEMA` against what `ingress.Render` emits. Check
   the README's minimum HA version.
5. Update the four constants and the `Activation`/status texts' version (they
   are built from `hassIngressPinVersion`, so only the test expectations change).
6. `HASS_INGRESS_PIN_ONLINE=1 go test ./cmd/launcher -run TestHassIngressPinOnline`
   re-downloads the real asset and checks size and sha256 against the constants.
7. Run the device acceptance (A1-A5) on one box.

### YAML-mode check

**Pure part** (`internal/ingress/mode.go`):

```go
type ModeStatus int
const (
	ModeNoFile   ModeStatus = iota // storage absent/empty: fresh HA, nothing to check
	ModeNoEntry                    // no `ingress` entry: the import creates it on the next start
	ModeYAML                       // data.mode == "yaml": fine
	ModeOther                      // data.mode is anything else ("ui", "agent", …)
	ModeUnreadable                 // not JSON, or the entry has no string data.mode
)
func EntryMode(storage string) (status ModeStatus, mode string)
func ModeWarning(status ModeStatus, mode string) string // "" for NoFile, NoEntry, YAML
```

`EntryMode` decodes `{"data":{"entries":[{"domain","data":{"mode"}}]}}`. It
considers **only** entries whose `domain == "ingress"`. If there is more than one
(which `single_config_entry` forbids), the first entry that is not YAML decides.

Warning texts (exact; written to stderr, each ending with a newline):

- *ModeOther*:
  ```
  warning: hass_ingress is in "<mode>" mode, so it ignores configuration.yaml and the snap's ingress panels will not appear.
    Switch it to YAML: Home Assistant → Settings → Devices & services → Ingress → ⋮ → Reconfigure → YAML.
  ```
- *ModeUnreadable*:
  ```
  warning: could not read hass_ingress's configuration mode from /config/.storage/core.config_entries.
    If the panels do not appear, check that Ingress is in YAML mode (Settings → Devices & services → Ingress → ⋮ → Reconfigure).
  ```

**I/O part** (`cmd/launcher/ingressmode.go`): `warnIngressMode(cli, w io.Writer)`
calls `ReadFile(homeassistant, "/config/.storage/core.config_entries")`, then
`EntryMode` and `ModeWarning`, and prints the warning if it is not empty. It
never returns an error and never blocks the sync (D11).

**Where it runs:**
- **`ingress sync`** (this branch): right after `preflightContainer`, and only
  when at least one panel is configured (`len(cfg.Ingress) > 0`). It runs
  before the write and the restart, because a mode outlives a restart, so the
  state before the restart is the state after it.
- **PR #6's `applyIngress`** (both the manual sync and the reconcile
  auto-sync), once PR #6 is on `main` (D12): next to PR #6's "hass_ingress is
  not installed" warning, when `len(entries) > 0` **and** the ingress dir
  exists. It sits **before** the `ShouldSync` early return, so an unchanged
  auto-sync still warns (D11).

## Testing

TDD with `go test ./...`. Every test in `cmd/launcher` uses W-5's `fakeRunner`
and `haFake`.

**T1. `TestParsePluginVersion` and `TestComparePluginVersion`**: table tests
with every row of the version table.

**T2. `TestHassIngressGate`**: a table test against `hassIngressGate` directly.
Its rows cover HA version × manifest × force, checking `proceed` and the exact
notice. New fake keys:
`exec homeassistant cat /config/custom_components/ingress/manifest.json`.

**T3. `runInstall` cases** (fake runner):
- a. Fresh (no manifest, no dir), HA 2026.11.1: the pointer is printed, the
  script is exec'd once, there is one restart, and the output ends with
  `Activation`.
- b. 1.3.2 present, no `--force`: the output is *older* + pointer. No exec, no
  restart.
- c. 1.3.2 present, `--force`: exec'd once, one restart.
- d. 1.4.0 present, no `--force`: *equal*. No exec, no restart.
- e. 1.4.0 present, `--force`: exec'd once (repair).
- f. 1.4.2 present, no `--force`: *newer*. No exec, no restart.
- g. 1.4.2 present, `--force`: *refuse-downgrade*, returns nil. No exec, no
  restart.
- h. Dir present, manifest missing, no `--force`: the output is the pointer,
  then `Installed`. No exec.
- i. HA 2026.10.3, fresh: the output does **not** contain `Marketplace`, and
  the script is exec'd.
- j. `.HA_VERSION` missing, fresh: the pointer is printed.
- k. The exec'd script contains `hassIngressPinURL` and
  `hassIngressPinSHA256`, and the first occurrence of `sha256sum -c` comes
  before the first occurrence of `/config`.

**T4. `TestHassIngressScript*`** runs the **real** script with `bash` on the
host (D9). `httptest.NewServer` serves a zip built in the test with
`archive/zip`: `manifest.json` with `"version":"9.9.9"`, plus `marker.txt`.
`configDir = t.TempDir()`, `tmpDir = t.TempDir()`, and the URL, size and sha
are passed through a test-only builder that takes them as arguments. The
production builder is a thin wrapper over the same function. The cases:
- s1. No existing dir: exit 0, and `custom_components/ingress/manifest.json`
  holds 9.9.9.
- s2. Existing dir with `old.txt`: exit 0, `old.txt` is gone, and the manifest
  is 9.9.9.
- s3. **Wrong sha** (the pin's sha is of other bytes): exit ≠ 0, stderr has
  `sha256 mismatch`, and the existing `old.txt` is still there.
- s4. Wrong size: exit ≠ 0, `size mismatch`, and the dir is intact.
- s5. HTTP 404: exit ≠ 0, `download failed`, and the dir is intact.
- s6. A zip wrapped in `custom_components/ingress/` (manifest not at the root):
  exit ≠ 0, `manifest.json not at the zip root`, and the dir is intact.

After **every** case, no `.snap-hass-ingress.*` is left in `configDir` and
`tmpDir` is empty.

If `bash`, `wget`, `unzip` or `sha256sum` is missing, the test `t.Skip`s, unless
`CI` is set, in which case it `t.Fatal`s. ubuntu-latest has all four, so the
test can't quietly skip in CI.

**T5. `TestHassIngressPinOnline`**: skipped unless `HASS_INGRESS_PIN_ONLINE=1`.
It downloads `hassIngressPinURL` and checks `len == hassIngressPinSize` and the
sha256 against `hassIngressPinSHA256`. It's used when bumping the pin, and is
not run in CI because CI should not hit the network.

**T6. `TestEntryMode` / `TestModeWarning`** (`internal/ingress`): `""` → NoFile;
`"not json"` → Unreadable; `{"data":{"entries":[]}}` → NoEntry; ingress yaml →
YAML; ingress ui → Other("ui"); ingress agent → Other("agent"); ingress entry
without `mode` → Unreadable; **another domain with `mode: ui` and no ingress
entry → NoEntry**; plus exact texts for every status (`""` for NoFile, NoEntry
and YAML).

**T7. `TestIngressSyncChecksMode`** (`cmd/launcher`): `runIngress` sync with a
fake runner and one panel configured. The calls include
`exec homeassistant cat /config/.storage/core.config_entries`. With zero
panels, that read is absent.

**T8 (merge-in step, after PR #6 is on main)**:
`TestApplyIngressAutoSyncChecksMode`. `applyIngress(cli, cfg, false)` with an
**unchanged** `snap-ingress.yaml` (so `ShouldSync` says no) still records the
storage read, and still records no restart.

**Existing tests that change:**
- `TestInstalledWording`: hass-ingress expects the new unknown-version text.
- `TestRunInstallIngressAlreadyInstalledWording`: rewritten as T3h. The "never
  reads .HA_VERSION" assertion is dropped, because hass-ingress now has a gate.
- `prefixIngressInstall` becomes `"exec homeassistant bash -c set -eu"`.
- `TestRunInstallGateError` and `TestHACSGate`: updated for the new `Gate`
  signature. `TestRunInstallHACSMarketplace` 3b (`--force` doesn't bypass) stays
  as it is, and still guards W-5's D5.

**Must be mutation-checked** (each must turn at least one test red; revert
after):
- M1. **Wrong sha accepted:** replace the `sha256sum -c` line with `true` →
  s3 red.
- M2. Drop the size check → s4 red.
- M3. Move the swap (`mv` into `custom_components`) above the sha check → s3 red
  (`old.txt` gone).
- M4. **No-downgrade guard removed** (newer + force → proceed) → T2 and T3g red.
- M5. Compare versions as strings → T1 `1.10.0` row red.
- M6. Make `--force` skip the gate entirely → T3g red.
- M7. **Mode check removed** from `runIngress` sync → T7 red. After the merge-in,
  removing it from `applyIngress` → T8 red.
- M8. Treat every mode as YAML (`ModeOther` → no warning) → T6 red.
- M9. Drop the domain filter in `EntryMode` → T6 "another domain" row red.
- M10. Pointer shown below 2026.11 → T3i red.

## Device acceptance (lav918: HA 2026.11.1, hass_ingress 1.3.2, panels configured)

Run after deploy with the user. Record command output for each step.

- **A0 (before).** `docker exec homeassistant cat /config/custom_components/ingress/manifest.json | grep version`
  shows `1.3.2`. `docker exec homeassistant python3 -c 'import json;print([e["data"] for e in json.load(open("/config/.storage/core.config_entries"))["data"]["entries"] if e["domain"]=="ingress"])'`
  shows `[{'mode': 'yaml'}]`. Note the sidebar panels that load.
- **A1.** `home-assistant.install hass-ingress; echo $?` prints the *older* line +
  the pointer, then `0`. `docker inspect -f '{{.State.StartedAt}}' homeassistant`
  is unchanged.
- **A2.** `home-assistant.install hass-ingress --force; echo $?` restarts HA and
  prints the Activation text, then `0`. The manifest now shows `1.4.0`.
  `docker exec homeassistant ls -a /config | grep snap-hass-ingress` is empty.
- **A3.** After HA is up: the entry query from A0 shows `'mode': 'yaml'`, and the
  entry's `version` is 2. The HA log has no `ingress` errors
  (`docker logs homeassistant 2>&1 | grep -i 'custom_components.ingress\|\[ingress\]' | tail`).
- **A4.** Every panel from A0 opens from the sidebar (user's eyes).
- **A5.** `home-assistant.ingress sync` prints **no** mode warning (YAML).
  `home-assistant.install hass-ingress` now prints the *equal* line + the pointer.
- **A6 (optional, needs the user and is reversible).** Reconfigure Ingress → Local
  UI in the HA UI (do **not** import panels), then run `home-assistant.ingress sync`.
  The *ModeOther* warning names `"ui"`. Reconfigure back to YAML, then sync again:
  no warning, and the panels are back.

Not covered on a device: the fail-closed path (s3-s6 cover it with the real
script on GNU tools, not busybox) and a fresh box (F2 is settled from code).

## Decisions record

- **D1. Pin one release asset by URL, size and sha256 in Go constants.**
  *Reason:* `main` is unreleased and moves; the asset is immutable and published
  with a `digest`. Constants make the pin reviewable in a diff and testable (T5).
  The Marketplace verifies no checksum at all, so this path is stricter than
  the Marketplace's.
- **D2. Download, verify and unzip in the container's `/tmp`. Write to
  `/config` only after every check passes.** *Reason:* "fail closed" has to mean
  the existing copy is byte-for-byte untouched. If nothing under `/config` is
  written before verification, that holds by construction, and s3-s6 prove it.
- **D3. Staging dir under `/config` (top level), then a two-rename swap with
  rollback.** *Reason:* `rename(2)` is atomic only within one filesystem, and
  `/tmp` (overlay) and `/config` (volume or bind mount) differ. Staging at the
  top level of `/config`, not inside `custom_components`, keeps a half-made copy
  away from HA's integration scan. A true exchange (`RENAME_EXCHANGE`) is not
  available in busybox. The microsecond gap is harmless, because HA restarts
  straight after.
- **D4. Use busybox `wget`, `sha256sum` and `unzip` in the HA image, not
  python3.** *Reason:* all three are present on both arches (F1), and today's
  recipe already relies on `wget`. A shell pipeline reads in one place and the
  script test runs it as-is. python3 `zipfile`/`hashlib` stays the fallback
  only if a future image drops an applet.
- **D5. Version-aware, with no silent downgrade:** no `--force` → never change
  an existing copy; just say how it relates to the pin. `--force` → install the
  pin over unknown, older or equal copies, and **refuse over a newer one** with
  exit 0. *Reason:* a newer copy almost certainly came from the Marketplace,
  which then owns updates, and replacing it would undo the user's choice. Exit 0
  is used because the state `--force` exists to reach ("at least the pinned
  version") already holds, and scripts that run `--force` to refresh must not
  start failing once the user has moved to the Marketplace. Unknown copies
  (old `main` installs, or a missing manifest) are what `--force` is for. A
  Marketplace copy at exactly the pin is replaced with identical bytes, so the
  Marketplace's own state (keyed by repository, not by files) is unaffected.
- **D6. `Recipe.Gate` gains `force bool`. `hacsGate` ignores it.** *Reason:*
  the downgrade guard needs `--force`, and a second hook (a per-recipe probe)
  would duplicate the gate's job. W-5's D5 ("--force does not bypass the hacs
  gate") stays true, because `hacsGate` never reads the flag, and W-5's test 3b
  keeps guarding it. W-5 (PR #9) is this branch's base and is not yet on `main`,
  so this changes no shipped API.
- **D7. The Marketplace pointer goes in the gate's notice, on 2026.11 or later
  or when the version is unknown.** *Reason:* this reuses
  `parseHAVersion`/`marketplaceBuiltIn` and W-5's notice plumbing with no new
  hook. Unknown counts as 2026.11 or later, as in W-5's D3: the default image
  is, and the pointer is only advice. Below 2026.11 the Marketplace doesn't
  exist, so pointing at it would mislead. The "choose YAML" sentence covers the
  one sequence (F2 caveat) that would create a UI-mode entry.
- **D8. Plain `install` (no `--force`) does not auto-upgrade an older copy.**
  *Reason:* `install` without flags has always been an idempotent no-op on an
  existing install, and it is safe in scripts. Upgrading restarts HA, so it
  should be asked for. The *older* line gives the exact command.
- **D9. The install script is built by a function with injectable dirs and
  source, and the tests run it for real.** *Reason:* a fake-runner test can only
  show that a string was exec'd, not that a bad sha stops the swap. Running the
  real script against a local HTTP server proves fail-closed and leaves no
  residue (M1-M3). Production calls the same builder with `/config` and `/tmp`
  and the pinned constants. The busybox-vs-GNU difference is covered by the
  device acceptance (A2).
- **D10. The mode check reads `core.config_entries` read-only and warns.**
  *Reason:* in any mode other than YAML, 1.4.0 ignores `ingress:` silently, so
  `sync` would "succeed" with no panels. Reading is safe, because HA writes that
  file atomically (F4). Writing it, to switch the mode back, would be editing HA
  internals under a running process (rejected in research A3/A4).
- **D11. Quiet when there is nothing to judge, loud when there is. The check
  never fails the sync.** File absent or empty → silent, because HA hasn't
  started yet. No ingress entry → silent, because the import creates a YAML
  entry on the restart that sync triggers (F2). Not JSON, or no mode → a soft
  "could not read" warning. Other mode → the warning. It runs on every
  auto-sync, even an unchanged one, and only with at least one panel. *Reason:*
  the failure is persistent and silent, and the reconcile log is where an
  operator looks. Reconcile runs on configure and daemon start, not in a loop,
  so this is not spam. Panels are cosmetic (PR #6's rule), so a check must never
  turn into an error.
- **D12. On this branch the mode check goes in `runIngress sync`. It moves into
  `applyIngress` when `main` is merged in after PR #6 lands.** *Reason:* PR #6
  (open, e78fa74) replaces `runIngress`'s body with `applyIngress`. Our only
  edit to `ingress.go` is one call line, and all the logic is in new files, so
  the merge-in is a one-line move into PR #6's shape. We do not restructure
  PR #6's code. T8 is written then.
- **D13. The pure parser lives in `internal/ingress/mode.go` (a new file).**
  *Reason:* it is about hass_ingress's config, next to `Render` and
  `EnsureInclude`. It is pure and table-testable. As a new file it can't
  conflict with PR #6's edits to `ingress.go` and `derive.go`.

## Open questions (for the orchestrator)

1. **Merge order with PR #6.** D12 assumes PR #6 lands on `main` before this
   branch's merge-in, so the auto-sync wiring (T8) is part of this branch. If
   W-12 ships first, the auto-sync check becomes a follow-up commit on top of
   PR #6. Which order is planned?
2. **A6 on lav918.** Flipping the live cabin entry to Local UI and back is
   reversible, but it touches the user's running config. Should the battery
   include it, or should the plan rely on T6/T7 plus A5?
