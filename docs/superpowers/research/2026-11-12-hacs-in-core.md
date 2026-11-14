# Research: HACS is now part of Home Assistant core (the Marketplace)

Date: 2026-11-12 · Wish: W-5 · Kind: research only (no code changed)

> Scope note. The first brief for W-5 asked whether *ingress* had moved into core.
> Joachim corrected it (W-5 Q1: «Not the ingress, but HACS»). I checked the
> ingress claim before the correction, and the finding is recorded in §6 so it
> does not get re-asked.

## 1. Verdict

**Confirmed.** Home Assistant **2026.11.0** (stable, released 2026-11-04;
2026.11.1 followed on 2026-11-09) brings HACS into core as the **Marketplace**
integration (domain `marketplace`). It upstreams HACS itself rather than
reimplementing it: frenck's core PR describes it as bringing HACS in "as much as
possible as it is". On 2026.11 and later:

- the HACS **custom integration is blocked by the core loader** (`hacs` is in
  `BLOCKED_CUSTOM_INTEGRATIONS`, with `replaced_by="marketplace"`);
- the **HACS installer script (`get.hacs.xyz`) refuses to run** and exits 1;
- the old HACS gets no more releases.

So our `install hacs` recipe (PR #3) **fails on every HA version the snap
installs by default** (image channel `stable`), and is not needed there.

Sources:
- Release post, sections «Welcome to the Marketplace» and «Thank you, HACS»: https://www.home-assistant.io/blog/2026/11/04/release-202611/
- Docs: https://www.home-assistant.io/integrations/marketplace/ (and `#coming-from-hacs`)
- Core PR #183502 «Add Marketplace integration» (frenck, merged 2026-10-01): https://github.com/home-assistant/core/pull/183502
- Frontend PR #54437 «Add Marketplace panel»: https://github.com/home-assistant/frontend/pull/54437
- Full changelog: https://www.home-assistant.io/changelogs/core-2026.11/
- Loader block, `homeassistant/loader.py` at tag 2026.11.1, lines 177–182: https://github.com/home-assistant/core/blob/2026.11.1/homeassistant/loader.py

## 2. What the core feature is

| Aspect | Finding | Source |
|---|---|---|
| Name / domain | Marketplace, `marketplace`. A system integration, set up by default; users never add it. | PR #183502 body; docs |
| UI | Settings → Marketplace, for **administrators only**. Before anything installs, each user must accept a risk notice once. "Until you accept the warning, Home Assistant cannot install or update Marketplace content, including from an automation." | docs «Opening the Marketplace» |
| YAML | No YAML configuration for the Marketplace itself. Installed integrations that use YAML still need it, plus a restart. | docs «Integrations» |
| Content types | Integrations (`custom_components/`), dashboard cards (`www/community/`, served at `/local/community/`; the old `/hacsfiles/` paths still work), themes (`themes/`), templates (`custom_templates/`). AppDaemon apps and python_scripts are **no longer managed**. It does not install add-ons ("apps"). | docs «What you can install», «Coming from HACS» |
| Catalog | Same catalog as HACS (`data-v2.hacs.xyz`). Files come from GitHub over HTTPS. "There are no signatures or checksums on what is installed." Optional China relays. | PR #183502 body; docs «Download source» |
| GitHub account | Not needed to browse, install or update catalog content. **Needed for custom repositories** (device-flow sign-in, read-only public scope). | docs «Connecting GitHub» |
| Updates | One `update` entity per installed repository, under Settings → Updates. Checked every 6 h (every 48 h for custom repositories). Updating an integration still needs a restart, and a repair offers to do it. Updates can be automated, but only after the warning has been accepted. | docs «Keeping installations up to date» |
| First install | Installing an integration needs no restart: it goes straight to the setup flow, unless the integration is YAML-configured. | release post «Browse and install» |
| Domain overrides | A community integration can still shadow a built-in domain; the install dialog warns about it. | docs «When an installation replaces a built-in integration» |
| Disable | Can be disabled from Devices & services. Installed content keeps working but gets no updates. Deleting the entry does not disable it; it comes back on the next start. | docs «Disabling the Marketplace» |

### Migration of an existing HACS install
On the first start of 2026.11 the Marketplace takes the HACS install over
(docs «Coming from HACS»; `marketplace/migration.py` at 2026.11.1):

- The HACS config entry is converted in place. Storage, update entities (same
  entity IDs), devices, dashboard resources and custom repositories carry over.
- **`custom_components/hacs` is deleted**, unless it is a symlink or git checkout,
  which is left alone and logged. Deletion happens only after the Marketplace has
  written its own repositories store (`_remove_legacy_files`).
- `/hacs` redirects to `/marketplace`. `integration_entities('hacs')` now returns
  nothing (use `'marketplace'`). The `hacs/*` websocket commands are gone, replaced
  by `marketplace/*` in a different format.
- A repository that HACS installed without a `hacs.json` stays installed but
  cannot be updated.

**Domain conflict:** the HACS domain is `hacs` and the core domain is
`marketplace`, so the two do not collide. Old HACS files left on disk on HA
2026.11 or later are not loaded, because the loader blocks the `hacs` domain.

## 3. Impact on our `install hacs` recipe (PR #3)

What the recipe does today on HA 2026.11 or later (read from
`cmd/launcher/install.go` and `get.hacs.xyz`; not run on a device):

1. Idempotency probe `test -d /config/custom_components/hacs`. After the migration
   the directory is gone, so the probe answers "not installed".
2. It runs `wget -O - https://get.hacs.xyz | bash`. The script reads
   `/config/.HA_VERSION`, and for a version of 2026.11 or later it prints
   *"HACS does not work on Home Assistant 2026.11 and newer (found X), as it has
   been replaced by the native Marketplace integration. See …/integrations/marketplace"*
   and exits 1 (get.hacs.xyz lines 57–69, fetched 2026-11-12).
3. `runInstall` returns `installing hacs: exit status 1`. HA is not restarted and
   the Activation text is not printed. The user sees the script's error line,
   followed by our generic wrapper error.

It does no damage, but the target is dead weight, and its messages are now wrong.
The "already installed" branch tells users that HACS "self-updates from the
Home Assistant UI", and the Activation text says to add "HACS" under
Devices & services.

For HA older than 2026.11 (only a user who pins `image.digest`/`image.channel`
to an old release), the old path still works, but HACS itself is frozen.

### Keep / replace / remove map

| Code path | Verdict | Why |
|---|---|---|
| `cmd/launcher/install.go:26-38` `installRegistry["hacs"]` (recipe, `get.hacs.xyz` pipe, Activation) | **Replace** (my recommendation) or **Remove** | On 2026.11 or later it can only fail. See the options below. |
| `cmd/launcher/install.go:129` "already installed (it self-updates from the Home Assistant UI)" | **Replace** the wording | True for HACS before 2026.11 only. For hass-ingress it was never true (the launcher copy does not self-update), which is the lav918 1.3.0 skew. |
| `cmd/launcher/install.go:39-63` `installRegistry["hass-ingress"]` | **Keep for now, then revise** (see §4) | Not affected by the HACS change. The Marketplace is now the better install path, because it updates. |
| `install` framework (`Recipe`, `parseInstallArgs`, `runInstall`, preflight, `--force`/`--no-restart`) | **Keep** | Still serves hass-ingress, and is generic. |
| `cmd/launcher/install_test.go:9-20,31-38` (uses `hacs` as the fixture target; `installTargets()` == `[hacs hass-ingress]`) | **Replace** with whatever the spec decides | Tests pin `hacs` as a real target. If it is removed, re-point the parse tests at `hass-ingress`. |
| `snap/snapcraft.yaml:21-23` (description: "Install HACS … `home-assistant.install hacs`") | **Replace** | Should say HACS ships with HA 2026.11 or later as Settings → Marketplace. |
| `README.md` (main) | none | It has no HACS mention on main. feat/addons adds `install hass-ingress` lines only. |
| `docs/superpowers/specs/2026-06-28-install-subcommand-design.md` | **Keep as history**, add a superseded note | A design record. Don't rewrite it. |
| `internal/docker/docker_test.go:129-133` (`ExecCheck` fixture path `/config/custom_components/hacs`) | **Keep** | Only a test string for `ExecCheck`, with no HACS semantics. |

### What `install hacs` should do on new HA versions: options for the spec
1. **Version-gated no-op (recommended).** Read `/config/.HA_VERSION` in the
   container, the same file `get.hacs.xyz` reads. For 2026.11 or later, print
   "HACS is built into Home Assistant 2026.11+ as the Marketplace: Settings →
   Marketplace (admin; accept the notice once)" and **exit 0**, with no restart.
   For an older version, keep the legacy path and add a one-line "frozen,
   upgrade HA" notice. Pro: scripts that call `install hacs` keep working, and
   pinned-old users are not stranded. Con: one more exec, plus a version parse.
   It needs an exec that captures stdout; I did not check whether
   `docker.Client` has one, since the current recipes use only `Exec` and
   `ExecCheck`.
2. **Unconditional guidance no-op.** Same message whatever the HA version, with no
   version read. It is simpler, but it strands users pinned to an old HA, who
   would have to run the script by hand.
3. **Remove the target.** `install hacs` then fails with "unknown install target".
   It is the cleanest code, but the most hostile to existing docs and muscle memory.
4. **Refuse (exit non-zero) with guidance on 2026.11 or later.** This is honest
   but breaks scripts for no benefit, because nothing is wrong.

The work is small and touches only `cmd/launcher/install*.go` and
`snap/snapcraft.yaml`, so it is **independent of PR #6**. PR #6 does not change
`install.go` or `install_test.go`: `git diff origin/main feat/addons` shows
changes only to README and snapcraft.yaml. That means snapcraft.yaml is the one
place the two branches could textually conflict.

## 4. Can hass_ingress be installed and updated through the Marketplace? **Yes.**

- **It is in the catalog.** `data-v2.hacs.xyz/integration/data.json` (fetched
  2026-11-12) lists `lovelylain/hass_ingress`: id 766376848, domain `ingress`,
  `last_version` 1.4.0, about 7,006 downloads. It is also in
  `hacs/default/integration`. No custom repository or GitHub account is needed.
- **It installs from a release asset.** Its `hacs.json` sets `zip_release: true`
  and `filename: ingress.zip`, and release 1.4.0 (2026-10-17) ships
  `ingress.zip`. The Marketplace downloads that asset
  (`_async_download_version`), whereas our recipe copies the **unreleased `main`
  branch tarball**.
- **It updates.** Every Marketplace install gets an update entity, so the lav918
  failure mode ("launcher installs never update": 1.3.0 is stuck while
  HA 2026.8 and later need 1.3.1 or newer) goes away once the Marketplace owns
  the install. Releases since 1.3.0: 1.3.1 (2026-08-07), 1.3.2 (2026-08-11),
  1.4.0 (2026-10-17).
- **Taking over a launcher-copied directory.** From reading
  `marketplace/repositories/base.py` `_async_write_content` (2026.11.1): when the
  target directory exists, it is **backed up, overwritten by the downloaded
  content, and restored if the write fails**. A Marketplace install over our
  copied `custom_components/ingress` should therefore just replace it and track
  it from then on. *This comes from reading the code. I did not test it on a
  device.*
- **No domain clash with core.** There is no `homeassistant/components/ingress`
  at 2026.11.1 (the GitHub contents API returns 404). Supervisor ingress lives
  under `hassio`.
- **Limit for automation:** each admin user has to accept the Marketplace risk
  notice in the UI once before anything installs, so the launcher cannot drive
  a Marketplace install headlessly on a fresh box. There are `marketplace/*`
  websocket commands, but the acceptance gate applies to them too (docs). That
  makes `install hass-ingress` either (a) guidance pointing at Settings →
  Marketplace → "Ingress", or (b) the current copy kept as an offline/headless
  fallback, preferably pinned to a release asset rather than `main`. Deciding
  between them is spec work (W-5 step 2) and needs Joachim.

## 5. Effect on PR #6 (feat/addons)

None that blocks it. PR #6's add-on panel auto-sync renders `ingress.*` into
`snap-ingress.yaml` for the `ingress` domain, and that YAML is consumed the same
way whoever installed the files, launcher or Marketplace. **Merge PR #6 as is.**
Do the HACS change as its own small branch, and rebase it over whichever lands
second, on the one shared file, `snap/snapcraft.yaml`.

Possible follow-up, not verified: hass_ingress 1.4.0's notes include «Configure
each ingress from a native Home Assistant row» (lovelylain/hass_ingress#115).
I did not check whether that changes or deprecates the YAML `ingress:` schema
that our renderer and PR #6 depend on. It is worth a look before W-5 step 2 or
before lav918 moves to 1.4.0.

## 6. The original ingress claim (before Q1)

There is **no generic ingress or iframe-proxy panel in core** in 2026.8 through
2026.11. Searching the four release posts and changelogs for "ingress" finds only
Supervisor add-on ingress items. Two of them are in 2026.11: «Add dedicated
websocket commands for add-on ingress sessions» and «Log failed hassio ingress
session validation at debug level». The 2026.9 changelog has one Supervisor
ingress line. hass_ingress is still an active third-party integration (last push
2026-11-07, not archived, no deprecation note) and is still needed for our panels.

## 7. What I could not verify

- I have not run `install hacs` against a real HA 2026.11 container. The failure
  path above comes from reading our code and the live `get.hacs.xyz`.
- I did not exercise the Marketplace taking over a launcher-copied
  `custom_components/ingress` on a device; that comes from reading the code only.
- I don't know whether a launcher-installed HACS that was **never set up**
  (files present, no config entry) gets *deleted* at the 2026.11 migration. It
  is blocked by the loader either way.
- I did not check which HA version the `ghcr.io/home-assistant/home-assistant:stable`
  tag resolves to today. 2026.11.0 and 2026.11.1 are released, so it is very
  likely 2026.11.x.
- I don't know lav918's current HA version, or whether HACS is installed there
  (no device access, by brief).
