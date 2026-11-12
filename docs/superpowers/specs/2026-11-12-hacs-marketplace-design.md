# `install hacs` on Home Assistant 2026.11+ (the Marketplace): design spec

**Date:** 2026-11-12 · **Wish:** W-5 · **Class:** bounded
**Status:** Proposed (for review).
**Inputs:** research report `docs/superpowers/research/2026-11-12-hacs-in-core.md`
(commit 9c2f312); Joachim's answer to W-5 Q2: option 1, a version-gated no-op.

## Goal

Home Assistant 2026.11.0 ships HACS in core as the **Marketplace** integration
(domain `marketplace`). On 2026.11 and later, `get.hacs.xyz` refuses to run
(exit 1) and the core loader blocks the `hacs` custom domain. The snap's default
image channel (`stable`) is already 2026.11.x, so today
`home-assistant.install hacs` fails with `installing hacs: exit status 1`.

After this change, `install hacs` does the right thing on every HA version:

- **HA 2026.11 or later:** it prints guidance pointing at Settings → Marketplace
  and exits 0. It downloads nothing, runs nothing and does not restart HA.
- **HA older than 2026.11:** it runs the legacy `get.hacs.xyz` install as it does
  today, and adds a one-line notice that HACS is frozen.

The change also fixes the "already installed (it self-updates …)" message,
which has never been true for `hass-ingress`.

## Scope

**IN**
- A per-recipe version gate in `cmd/launcher/install.go`, with the `hacs` recipe
  as its only user.
- A pure, unit-tested HA version parser and a pure threshold check.
- A per-recipe "already installed" message, replacing the shared one.
- New wording for the hacs legacy Activation text.
- The `snap/snapcraft.yaml` description, lines 21–23.
- A superseded note at the top of
  `docs/superpowers/specs/2026-06-28-install-subcommand-design.md`. Nothing else
  in that file changes.
- Tests: the parser table, the threshold, and `runInstall` with a fake docker
  runner on both branches.

**OUT** (not decided here; see *Open questions*)
- Any change to how `install hass-ingress` installs (Marketplace guidance,
  pinning a release asset, and so on). Only its "already installed" wording
  changes.
- hass_ingress 1.4.0's «native HA row» config (lovelylain/hass_ingress#115).
- Removing the `hacs` target (rejected by Q2).
- `README.md`: it has no HACS mention on `main` (checked with
  `grep -i hacs README.md`, which finds nothing), so it does not change.

## Behaviour

### Order of steps in `runInstall`

```
parseInstallArgs → resolveTarget → preflightContainer
  → recipe.Gate (if set)       ← NEW
  → dir probe (unless --force)
  → recipe.Install → restart (unless --no-restart) → Activation
```

`Gate` has the signature
`func(c *docker.Client) (proceed bool, notice string, err error)`. A nil
`Gate` means "proceed with no notice", so `hass-ingress` behaves exactly as
before.

- `err != nil`: `runInstall` returns it.
- `proceed == false`: print `notice` and return nil (exit 0). There is no dir
  probe, no Install and no Restart, **whatever `--force` and `--no-restart`
  say**.
- `proceed == true`: print `notice` if it is not empty, then carry on with the
  existing flow (dir probe, Install, restart, Activation), unchanged.

### The hacs gate

The gate reads `/config/.HA_VERSION` with the existing
`docker.Client.ReadFile(dockerargs.ContainerName, "/config/.HA_VERSION")`.
That is the same file `get.hacs.xyz` reads. It then calls
`parseHAVersion(raw)` and `marketplaceBuiltIn(year, month)`.

| Version read | proceed | notice (exact text pinned by tests) |
|---|---|---|
| ≥ 2026.11 | false | Marketplace guidance (below), with the version |
| < 2026.11 | true | `Note: HACS is frozen: it moved into Home Assistant core (the Marketplace) in 2026.11 and gets no further releases. Upgrading Home Assistant migrates it automatically.` |
| missing / unparsable | false | `Could not read Home Assistant's version from /config/.HA_VERSION (got "<raw>"); assuming 2026.11 or later.` + Marketplace guidance + `On an older Home Assistant, run the legacy installer by hand: docker exec homeassistant bash -c "wget -O - https://get.hacs.xyz \| bash"` |

Marketplace guidance:

```
HACS is built into Home Assistant 2026.11 and later as the Marketplace (found <version>); nothing to install.
  1. Home Assistant → Settings → Marketplace (administrator accounts only).
  2. Accept the one-time risk notice. Until you do, nothing can be installed or updated.
An existing HACS install is migrated to the Marketplace automatically on the first start of 2026.11 or later.
```

### Version parsing (pure function)

`parseHAVersion(raw string) (year, month int, ok bool)`:

1. Trim surrounding whitespace, including the trailing newline.
2. Split on `.`. There must be at least 2 fields, and fields 0 and 1 must be
   non-empty and made only of ASCII digits. Anything after field 1 is ignored.
3. Otherwise return `ok = false`.

`marketplaceBuiltIn(year, month int) bool` returns
`year > 2026 || (year == 2026 && month >= 11)`. The threshold lives in named
constants (`marketplaceYear = 2026`, `marketplaceMonth = 11`).

| `.HA_VERSION` | parsed | Marketplace? |
|---|---|---|
| `2026.11.0` | 2026, 11 | yes |
| `2026.11.1\n` | 2026, 11 | yes |
| `2026.12.0b1` | 2026, 12 | yes |
| `2026.12.0.dev20261110` | 2026, 12 | yes |
| `2026.11.0.dev20260915` | 2026, 11 | yes (see D4) |
| `2026.10.3` | 2026, 10 | no |
| `2026.1.0` | 2026, 1 | no (numeric, not string, comparison) |
| `2027.1.0` | 2027, 1 | yes |
| `0.118.5` | 0, 118 | no |
| `2026.11` | 2026, 11 | yes |
| `""`, `garbage`, `v2026.11.0`, `2026`, `2026.x.0`, `.11.0` | — | unparsable → D3 |

### Flags on HA 2026.11 or later
- `--force` does **not** bypass the gate (D5).
- `--no-restart` has no effect, because nothing restarts anyway.
- Both are still accepted, and `parseInstallArgs` does not change, so existing
  scripts keep parsing.

### "Already installed" wording (per recipe)

`Recipe` gets a new `Installed string` field, which is printed when the dir
probe finds `Dest`. The shared `"(it self-updates from the Home Assistant UI)"`
line is removed.

- **hacs** (reached only on HA older than 2026.11):
  `hacs is already installed. It updates from its own page in the Home Assistant UI, but gets no new releases. Use --force to reinstall.`
- **hass-ingress**:
  `hass-ingress is already installed. This copy does not update itself: run "home-assistant.install hass-ingress --force" to refresh it from upstream.`

### hacs legacy Activation text

Keep steps 1 and 2 as they are. Replace the line `HACS updates itself from the
UI thereafter.` with `HACS gets no further releases; upgrading Home Assistant to
2026.11 or later moves it to the built-in Marketplace.`

### snapcraft.yaml description (lines 21–23)

```
  HACS (the Home Assistant Community Store) is built into Home Assistant
  2026.11 and later as Settings → Marketplace. On older Home Assistant
  versions, `home-assistant.install hacs` still installs it.
```

### Superseded note (2026-06-28 install spec)

A blockquote is added directly under the title, and nothing else in the file
changes:

> **Superseded in part (2026-11-12, W-5):** HACS moved into Home Assistant core
> as the Marketplace in 2026.11. `install hacs` is now a version-gated no-op on
> 2026.11 and later. See `2026-11-12-hacs-marketplace-design.md`.

## Testing

All tests live in `cmd/launcher/install_test.go`. TDD, run with `go test ./cmd/launcher/...`.

1. **`TestParseHAVersion`**: a table test with every row of the parsing table
   above, checking year, month and ok.
2. **`TestMarketplaceBuiltIn`**: a table test covering (2026,10)=false,
   (2026,11)=true, (2026,12)=true, (2025,12)=false, (2027,1)=true and
   (0,118)=false.
3. **`runInstall` with a fake runner.** Add a `fakeRunner` in `cmd/launcher`
   that implements `docker.Runner` and records calls, and use
   `docker.NewWithRunner`. `inspect -f {{.State.Running}} homeassistant`
   returns `"true"`, so preflight passes. `runInstall` gets an `io.Writer` for
   its output (D7). The cases:
   - **a. ≥2026.11** (`exec homeassistant cat /config/.HA_VERSION` → `2026.11.1`),
     args `hacs`: returns nil; the output has the guidance and `found 2026.11.1`.
     The recorded calls contain **no** `exec … bash -c …get.hacs.xyz…`, **no**
     `restart`, and **no** `test -d` probe.
   - **b. ≥2026.11 with `--force`**, and **c. with the hacs dir present**
     (`exec homeassistant test -d /config/custom_components/hacs` → true):
     same assertions as (a), and the output does not say "already installed".
   - **d. <2026.11** (`2026.10.3`), args `hacs`, dir absent: the frozen notice
     is printed, `get.hacs.xyz` is exec'd once, `restart homeassistant` is
     called once, and the Activation text is printed.
   - **e. <2026.11 with `--no-restart`**: exec'd, no restart.
   - **f. missing file** (the Output error on `cat`): returns nil, the
     "Could not read … assuming 2026.11" line is printed, and there is no exec
     and no restart.
   - **g. hass-ingress already installed**: it prints the hass-ingress
     `Installed` text, which contains `--force` and does not contain
     `self-update`. `.HA_VERSION` is never read (nil Gate).
4. Existing tests (`TestResolveTarget`, `TestInstallTargets`,
   `TestParseInstallArgs`) stay. `hacs` is still a target.

**Must be mutation-checked** (each mutation must turn at least one test red;
revert after):
- M1: in `marketplaceBuiltIn`, change `>= 11` to `> 11`, or the constant 11 to
  12. Expected red: test 2 and 3a.
- M2: remove the `Gate` call from `runInstall`. Expected red: 3a, 3b and 3f.
- M3: move the Gate after the dir probe. Expected red: 3c.
- M4: let `--force` bypass the gate. Expected red: 3b.
- M5: change the unknown-version default to proceed. Expected red: 3f.
- M6: compare the version as a string, not numerically. Expected red: the
  `2026.1.0` row.

**Device check (acceptance, after deploy):** on a box running HA 2026.11.x:
1. Run `home-assistant.install hacs; echo $?`. The guidance is printed, then `0`.
2. `docker inspect -f '{{.State.StartedAt}}' homeassistant` shows the same value
   before and after (no restart).
3. `home-assistant.install hass-ingress` on a box that already has it prints the
   new wording.

The legacy (<2026.11) branch is covered by unit tests only, unless the
controller asks for a pinned-image device run.

## Decisions record

- **D1. Version-gated no-op** (Joachim, W-5 Q2.a1). On 2026.11 or later, print
  the Marketplace guidance and exit 0 with no download and no restart. Below
  2026.11, keep the legacy `get.hacs.xyz` path plus a one-line frozen notice.
  *Reason:* scripts and muscle memory that call `install hacs` keep working and
  are told where HACS went. Users who pin an older image are not stranded.
  Nothing is wrong on new HA, so failing would be dishonest.
- **D2. Read `/config/.HA_VERSION` with the existing `ReadFile`.** *Reason:*
  `get.hacs.xyz` gates on the same file, so the two can never disagree. It
  describes the HA that is actually running in the config volume, not the
  configured image tag, which could be `stable` or a digest. `ReadFile` already
  exists, so `internal/docker` does not change.
- **D3. A missing or unparsable version counts as 2026.11 or later** (guidance,
  exit 0), with an explicit "could not read" line and the manual legacy command.
  *Reason:* this is the safe side, because it downloads nothing and restarts
  nothing. The default image is 2026.11 or later. On new HA the legacy path
  could only exit 1 anyway. HA writes `.HA_VERSION` on every start, so a
  running container almost always has it. `ReadFile` reports docker errors as
  `""`, and those land here too, which is harmless. The rare user on an old HA
  without the file gets the exact command to run instead.
- **D4. The comparison uses year and month only, and treats beta and dev builds
  of 2026.11 or later as having the Marketplace.** *Reason:* `get.hacs.xyz`
  refuses on any 2026.11 or later version, so for a `2026.11.0.devX` build from
  before the Marketplace merge, the legacy path would still fail. Matching the
  script's threshold is the only consistent choice. Pre-2026.11 dev builds are
  not a supported target.
- **D5. `--force` does not bypass the gate. `--no-restart` is accepted and has
  no effect.** *Reason:* on 2026.11 or later the forced path can only reproduce
  the exit 1 (the script refuses, and the loader blocks `hacs`). Keeping both
  flags accepted means `parseInstallArgs` and existing invocations do not
  change.
- **D6. The gate runs after preflight and before the dir probe.** *Reason:*
  reading the file needs a running container, which preflight guarantees and
  explains when it is not there. Running the gate before the probe stops a
  leftover `custom_components/hacs` from producing a false "already installed".
  The migration leaves symlinks and git checkouts in place, and an install that
  was never set up may survive too (research §7).
- **D7. `Recipe` gets an optional `Gate` hook and a required `Installed`
  message, and `runInstall` writes to an `io.Writer`** (main passes
  `os.Stdout`). *Reason:* per-recipe behaviour stays in the registry, where
  recipes already live, rather than as `if target == "hacs"` in the flow.
  `hass-ingress` is untouched with a nil Gate. The writer lets tests assert the
  exact messages without capturing the process's stdout.
- **D8. A per-recipe "already installed" message.** *Reason:* the shared
  "self-updates from the Home Assistant UI" claim was never true for
  hass-ingress (the launcher copy never updates, which caused the lav918 1.3.0
  skew). Each recipe now tells its own truth, and hass-ingress points at
  `--force`.
- **D9. The hacs target stays registered, and the parse tests keep using it.**
  *Reason:* Q2 rejected removal. Leaving the tests unchanged also shows that the
  CLI surface did not change.
- **D10. Docs: change the snapcraft description, add a superseded note to the
  old spec, and leave README alone.** *Reason:* the store description is
  user-facing and now wrong. The old spec is a design record, so it is annotated
  rather than rewritten. README has no HACS text on `main`. The snapcraft hunk
  (lines 21–23) is separate from PR #6's hunk (after line 28), so a textual
  conflict is unlikely, and if one happens it is trivial.

## Open questions (for the orchestrator; not decided here)

1. **Should `install hass-ingress` point at the Marketplace on HA 2026.11 or
   later?** hass_ingress is in the catalog (`lovelylain/hass_ingress`, domain
   `ingress`, 1.4.0), and a Marketplace install gets an update entity, which
   would fix the "launcher installs never update" skew (lav918 is stuck on
   1.3.0). A Marketplace install writes to the same `custom_components/ingress`
   that PR #6's auto-sync probes, so the sync keeps working whoever installed
   it. The catch: each admin must accept the Marketplace risk notice in the UI,
   so a headless or offline box still needs the launcher copy (ideally pinned to
   the `ingress.zip` release asset rather than `main`). The options are
   guidance-only, guidance plus copy as a fallback, or no change.
2. **hass_ingress 1.4.0 «Configure each ingress from a native Home Assistant
   row» (#115):** does it change or deprecate the YAML `ingress:` schema that
   `home-assistant.ingress sync` and PR #6 render into `snap-ingress.yaml`?
   This needs checking before lav918, or any Marketplace install, moves to
   1.4.0.
