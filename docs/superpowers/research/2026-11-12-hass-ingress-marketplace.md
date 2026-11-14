# Research: installing hass_ingress through the Marketplace (HA 2026.11+), and the 1.4.0 config change

Date: 2026-11-12 · Wish: W-12 (spun out of W-5 Q-A / Q-B) · Kind: research only (no code changed, no device touched)

Builds on `docs/superpowers/research/2026-11-12-hacs-in-core.md` (commit 9c2f312). The points it
settled are not repeated here: the Marketplace is upstreamed HACS (domain `marketplace`),
hass_ingress 1.4.0 is in the default catalog (repository id 766376848, domain `ingress`), and a
Marketplace install backs up and replaces an existing directory.

Source pins used throughout:

- **MP** = `https://github.com/home-assistant/core/blob/2026.11.1/homeassistant/components/marketplace/`
- **CORE** = `https://github.com/home-assistant/core/blob/2026.11.1/homeassistant/components/`
- **HI** = `https://github.com/lovelylain/hass_ingress/blob/1.4.0/custom_components/ingress/`

All source files were fetched raw at those tags on 2026-11-12. Line numbers refer to those tags.

## 0. Short answers

| # | Question | Answer |
|---|---|---|
| A1 | Install / update API | **Install a not-yet-installed repository:** websocket `marketplace/repository/install` with `{"repository": "766376848", "version"?: "1.4.0", "confirm_replace_built_in"?: false}`. Admin only, and the caller's user must have accepted the warning. **No service exists for a first install.** **Update:** the `update.install` service on the repository's update entity, which exists only once the repository is installed. The same websocket command also works. |
| A2 | Risk notice | Stored **per HA user id** in the Marketplace **config entry's data** (`warning_accepted: {user_id: {version, accepted_at}}`, which lands in `.storage/core.config_entries`). A non-UI client *can* satisfy it: websocket `marketplace/warning/accept` marks the calling admin's user as having accepted. ⚠ **If the launcher calls it, that bypasses a gate HA added on purpose. Joachim would have to decide that; I recommend against it.** |
| A3 | Auth | The launcher needs an **admin user's access token** (a long-lived token). There is no headless way to get one on an onboarded box short of editing HA's auth storage (don't). `docker exec` inside the container only saves the network hop. It is still an authenticated websocket on `localhost:8123`. |
| A4 | Adopt a present dir? | **No.** A dir the launcher copied into `custom_components/ingress` is not detected as installed: it gets no update entity, and the Marketplace shows Ingress as "not installed". A Marketplace install over it **replaces** it (backup → write → restore on failure), and from then on it is tracked and updated. |
| A5 | 1.4.0 asset | `https://github.com/lovelylain/hass_ingress/releases/download/1.4.0/ingress.zip`, 61,359 bytes, **sha256 `7b2255fa3f25b97d465c17336cae8188bcc4dfefd52677682f084c1e39a54cac`**. GitHub publishes it in the release API's `digest` field, and I re-hashed a download myself and got the same value. The zip's **root is the integration directory** (`manifest.json`, `__init__.py`, … at top level), with no `custom_components/ingress/` prefix. |
| B | 1.4.0 YAML | **YAML `ingress:` still works, and the schema only gained new keys.** Every key `Render` emits is accepted unchanged. Existing (1.3.x) installs keep a YAML-mode config entry through the upgrade. **The new risk:** the config entry now has a *mode*. If it is ever set to **Local UI** (the default choice in the setup form), YAML is **silently ignored** and `ingress sync` / PR #6 stop having any effect. |

**Recommendation:** a **pinned, checksum-verified 1.4.0 copy plus Marketplace guidance** (option 4
below). Add a check in `ingress sync` that warns when hass_ingress's config entry is not in YAML
mode. Do **not** drive the Marketplace API from the launcher.

## A. The Marketplace install path

### A1. Websocket and service API

Every `marketplace/*` command is registered with `@websocket_api.require_admin` and wrapped by
`marketplace_command()`. That wrapper refuses with `not_loaded` when the Marketplace entry is not
loaded. It refuses with `warning_not_accepted` when `requires_accepted_warning=True` and the
**connection's user** has not accepted the notice
(MP `websocket/decorators.py#L102-L147`, gate at L127-L136).

| Command | Payload | Gate | Source |
|---|---|---|---|
| `marketplace/info` | — | admin | MP `websocket/__init__.py#L95-L125`; returns `warning_accepted` for the caller (L122) |
| `marketplace/repositories/list` | (categories filter) | admin | MP `websocket/repositories.py#L62-L90` |
| `marketplace/repository/info` | `repository_id: str` | admin | MP `websocket/repository.py#L76-L139`; returns `update_entity_id` (L133-L135) |
| **`marketplace/repository/install`** | **`repository: str` (catalog id), `version?: ref`, `confirm_replace_built_in?: bool=false`** | **admin + warning** | MP `websocket/repository.py#L254-L312` |
| `marketplace/repository/version` | `repository`, `version` | admin + warning | MP `websocket/repository.py#L171-L211` |
| `marketplace/repository/uninstall` | `repository` | admin | MP `websocket/repository.py#L315-L395` |
| `marketplace/warning/accept` | — | admin | MP `websocket/__init__.py#L153-L169` |

- **First install.** `{"id": N, "type": "marketplace/repository/install", "repository": "766376848"}`.
  `repository` is the catalog id. The handler looks it up with `repositories.get_by_id` (L272).
  When no `version` is given and the repository is in the default catalog, the Marketplace
  installs the version the catalog names (1.4.0) **without using the GitHub API**
  (MP `repositories/base.py#L2131-L2147`). For a repository with `zip_release` that means the
  `ingress.zip` asset (L1262-L1267).
- **No service for a first install.** The only `hass.services` calls in the integration are
  restart and frontend-reload calls. It registers no install service.
- **Updates.** `update.py` creates entities only for `repositories.list_installed` (MP `update.py#L35-L46`).
  Their `async_install` gates on the warning (L121-L133) and calls
  `repository.async_install_repository(ref=…)` (L147). So **`update.install`** on that entity works
  once the repository is installed. Updating an integration raises a fixable `restart_required`
  repair (MP `repositories/integration.py#L191-L216`).
- **Built-in clash** (`confirm_replace_built_in`) does not apply. There is no core `ingress`
  component, so `async_replaces_built_in` is false (MP `repositories/integration.py#L233-L241`;
  the 404 is in the prior report §4).

### A2. Where the acceptance of the risk notice is stored, and whether a client can satisfy it

- **Per user, in the Marketplace config entry.** `async_accept_warning(user_id)` writes
  `config_entry.data["warning_accepted"][user_id] = {"version": WARNING_VERSION, "accepted_at": …}`
  through `async_update_entry` (MP `base.py#L508-L525`). It is read back by `warning_acceptances` /
  `warning_accepted` (MP `base.py#L470-L486`). `WARNING_VERSION = 1`, and bumping it makes every
  user accept again (MP `const.py#L57-L60`). Config-entry data lives in `.storage/core.config_entries`.
- **Removed users lose their acceptance** (MP `__init__.py#L306-L325`).
- **Automations** (a context with no user) may install or update when **any** user has accepted
  (MP `update.py#L123-L127`). Docs: "You can also use an automation to install updates after
  someone accepts the Marketplace warning" (https://www.home-assistant.io/integrations/marketplace/,
  «Keeping installations up to date»).
- **Can a non-UI client satisfy it? Technically yes.** `marketplace/warning/accept` is an ordinary
  admin websocket command that records acceptance for the **calling** user. It contains no check
  that a human saw the text.
- ⚠ **Safety-gate flag.** The docs state the intent: "Until you accept the warning, Home Assistant
  cannot install or update Marketplace content, including from an automation", and the warning
  says "Nothing published in the Marketplace is tested, audited, or supported by the Home Assistant
  project" (docs «Opening the Marketplace»). A launcher that calls `marketplace/warning/accept`,
  or writes `warning_accepted` into `.storage`, accepts that risk on behalf of a person who never
  read it. **That is a bypass of a deliberate gate. It needs Joachim's decision, and my
  recommendation is not to do it.** The legitimate pattern is for a human admin to accept once in
  the UI. After that, any token of *that* user, or any automation, can install.

### A3. Auth a launcher would need

What the launcher has today: the snap's plugs are `docker` and `docker-executables` only
(`snap/snapcraft.yaml#L45-L52`, apps L65-L80; there is no `network` plug). The `docker.Client` can
`Exec`, `ExecCheck`, `ReadFile`, `WriteFile` and `Capture` inside the container
(`internal/docker/docker.go#L62-L153`). It holds no HA credential.

- **Token type needed:** an access token for an **admin** user. The commands are
  `require_admin`, and the warning check is keyed on `connection.user.id`, so that user must also
  have accepted.
- **Long-lived token, headless:** created only by websocket `auth/long_lived_access_token`, which is
  `ws_require_user()`, meaning you already need an authenticated session
  (CORE `auth/__init__.py#L636-L665`). That is a chicken-and-egg problem on any box that is
  already onboarded.
- **Onboarding route:** `POST /api/onboarding/users` has `requires_auth = False`, but it works only
  until the user step is done ("User step already done", 403). It creates the **owner/admin** and
  returns an auth code (CORE `onboarding/views.py#L176-L249`). A launcher using it would be
  creating the box's owner account and choosing its password. That is not acceptable as a side
  effect of `install`, and it is impossible on an existing box anyway.
- **Writing `.storage/auth` to mint a token:** possible in principle with `WriteFile`, but it edits
  HA's credential store under a running process that rewrites it. **Don't.**
- **`trusted_networks` auth provider for 127.0.0.1:** would loosen HA login for everyone on
  that network. It is a security change to the user's install, so it is out of scope.
- **`docker exec` to localhost:** reachable. `python3` and aiohttp are in the HA image, so a script
  could open `ws://127.0.0.1:8123/api/websocket` from inside the container with no `network`
  plug. **It still needs the token above.** It removes the network-plug issue, not the auth one.
- **The only clean variant:** the user creates a long-lived token for their own admin user, after
  accepting the notice in the UI, and hands it to the snap (for example `snap set … ha.token=…`).
  That is a new secret in snap config (visible to `snap get` for root), and it adds a manual step
  that is larger than "click Install in the Marketplace".

### A4. Adoption of an existing `custom_components/ingress`

- "Installed" is Marketplace state, not a disk scan. `data.installed = True` is set only after the
  Marketplace itself writes the content (MP `repositories/base.py#L1253-L1260`), or when the
  HACS-storage migration carries a HACS install over (MP `migration.py`, prior report §2). The
  startup pass checks only the opposite direction: Marketplace-installed repos whose files were
  deleted by hand (MP `utils/data.py#L305-L330`). **Nothing marks an existing directory as
  installed.** So a launcher-copied dir gets no update entity, and the Marketplace lists Ingress as
  installable. (I found no adoption code path. I did not see the panel's display of it on a device.)
- **Install over it:** `_async_write_content` backs up whatever is at the local path, writes the
  download, and restores the backup on any failure (MP `repositories/base.py#L1269-L1340`). The
  pre-install conflict check looks only at *Marketplace-installed* repositories with the same domain
  (MP `repositories/integration.py#L170-L189`), so our copy does not block it. Because `ingress` is
  already loaded, a restart repair follows (L191-L216).
- **"Pinned copy that the Marketplace adopts" is therefore not possible** without writing the
  Marketplace's `marketplace.repositories` store ourselves, which is the same "edit HA internals"
  problem as A3. What *is* possible is a pinned copy that the Marketplace **replaces cleanly**
  when the user installs Ingress from it.

### A5. The 1.4.0 release asset

From `gh api repos/lovelylain/hass_ingress/releases` (2026-11-12):

| Tag | Published | Asset URL | Size | sha256 (GitHub `digest`) |
|---|---|---|---|---|
| 1.4.0 | 2026-10-17 | https://github.com/lovelylain/hass_ingress/releases/download/1.4.0/ingress.zip | 61,359 | `7b2255fa3f25b97d465c17336cae8188bcc4dfefd52677682f084c1e39a54cac` |
| 1.3.2 | 2026-08-11 | …/1.3.2/ingress.zip | 42,110 | `6d04e4952d6355872d659cf7adb2df26e036afdec3f10cd7e9282f912a80eaa9` |
| 1.3.1 | 2026-08-07 | …/1.3.1/ingress.zip | 42,110 | `09297d97e87cccd2f9d69c6c1682dd626accd3f15daa0a624bfb97b81fb65eb2` |
| 1.3.0 | 2026-03-22 | …/1.3.0/ingress.zip | 42,086 | `f727a305b48f09ce6f620d8fb4258152fb26c99030945ab1c3b9a8b92313d831` |

- I downloaded the 1.4.0 zip and ran `sha256sum`. It matches the API digest.
- The zip lists `manifest.json` (`"version": "1.4.0"`, `"single_config_entry": true`,
  `"config_flow": true`), `__init__.py`, `config_flow.py`, `www/…` **at its root**. A pinned
  install must extract it *into* `/config/custom_components/ingress/`. The upstream README's manual
  step ("put the `custom_components/ingress` folder…") describes the source tree, not the zip.
- `hacs.json` at 1.4.0: `zip_release: true`, `filename: ingress.zip`
  (https://github.com/lovelylain/hass_ingress/blob/1.4.0/hacs.json).
- The Marketplace itself verifies **no** checksum or signature: grepping for
  `sha256|digest|checksum|hashlib` finds nothing in the integration, which matches the docs' "There
  are no signatures or checksums". A launcher pin with sha256 is therefore *stricter* than the
  Marketplace path.
- 1.4.0's README states a minimum of **HA 2025.8** (PR #115 body). The snap's default image is
  stable (2026.11.x), so this matters only for users pinned to an old image.

### Options for the spec

| # | Option | Pros | Cons | Needs |
|---|---|---|---|---|
| 1 | **Guidance only.** `install hass-ingress` on HA ≥ 2026.11 prints "Settings → Marketplace → Ingress → Install (accept the notice once)" and exits 0. | HA owns install and updates; no safety gate touched; zero network code. | Not headless. A fresh box needs the UI before `ingress sync` does anything. Offline boxes can't install. After install, the setup form defaults to **Local UI** (see B), which silently disables our YAML unless the user picks YAML. | Version read of `/config/.HA_VERSION`; message text. |
| 2 | **Install via the Marketplace API.** | Headless once set up; gets update entities from day one. | Needs an admin token (A3) and an accepted warning for that user (A2). Either a manual token hand-off to the snap, or a gate bypass (⚠, no). Adds a websocket client, a secret in snap config, and GitHub/catalog reachability at install time. More moving parts than option 1 for the same manual prerequisite. | Token config key, ws client (in-container python or a `network` plug), error mapping. **Joachim's decision if acceptance were automated.** |
| 3 | **Pinned copy that the Marketplace adopts.** | — | **Not possible** (A4): no adoption path without writing Marketplace storage. | — |
| 4 | **Pinned, checksum-verified copy + guidance** *(recommended)*. Fetch the pinned `ingress.zip` (URL + sha256 in Go), verify, extract into `custom_components/ingress`, and print "for updates, install Ingress from Settings → Marketplace; it replaces this copy". | Headless and deterministic. Fixes the "copies unreleased `main`" problem. Stricter integrity than the Marketplace. Hands off cleanly to the Marketplace, which replaces the dir (A4). Works on HA < 2026.11 too (guidance gated by version). | Still stale until the user moves to the Marketplace or the snap bumps the pin. Needs a pin bump per hass_ingress release. Must not overwrite a Marketplace-managed copy (detect and leave it alone). | Pin constants; in-container download + `sha256sum` + extract; replace the "self-updates" message (`install.go:129`); optionally compare `manifest.json` `version` to the pin so `install` can upgrade an old launcher copy. |
| 5 | **No change.** | Nothing to do. | Keeps installing unreleased `main`; never updates (the lav918 1.3.0 failure); the "self-updates" message stays wrong. | — |

**Recommendation: option 4.** Add a small companion change for B: `ingress sync` should read
`/config/.storage/core.config_entries` (via `ReadFile`) and warn when the `ingress` entry's
`data.mode` is not `yaml`. Option 2 is not worth it. Its unavoidable manual step (create a token
after accepting the notice) is larger than option 1's (click Install), and the only fully headless
variant is a gate bypass. Deciding between 4 and 1 is spec work for Joachim.

Two open implementation points for the spec (not verified):

- whether the HA image has `unzip` (`python3 -m zipfile -e` is a safe fallback, since python3 is
  certainly there);
- how to tell a Marketplace-managed `custom_components/ingress` from ours. Candidates: the
  Marketplace's repositories store lists 766376848 as installed, or a marker file we write.

## B. hass_ingress 1.4.0 and the YAML `ingress:` configuration

Sources: release notes https://github.com/lovelylain/hass_ingress/releases/tag/1.4.0; PR #115
https://github.com/lovelylain/hass_ingress/pull/115 (merged 2026-10-14, merge commit 85ce6a7);
source at tag 1.4.0.

- **Not deprecated.** PR #115: "YAML and external-agent modes remain available. Switching from YAML
  to local UI requires confirmation before importing panels". README «YAML configuration» still
  documents `ingress:` in `configuration.yaml`.
- **Schema: additive only.** I diffed `CONFIG_SCHEMA` between 1.3.2 and 1.4.0. The only additions
  are the optional `enable_compress`, `tls_mode`, `tls_certificate` and `show_in_sidebar`; nothing
  was removed or retyped (HI `__init__.py#L54-L104`).
- **What `Render` emits vs. 1.4.0** (`internal/ingress/ingress.go#L24-L55`; PR #6 only changes the
  header comment, and `DeriveEntries` emits the same fields with `WorkMode: "ingress"`):

  | Render emits | 1.4.0 schema | OK? |
  |---|---|---|
  | slug key `name:` | `cv.schema_with_slug_keys` (L56) | yes (unchanged) |
  | `url: "<str>"` | `Required(url): Any(cv.string, {match,replace,default})` (L62-L71) | yes |
  | `title: "<str>"` | `Optional(title): cv.string` (L60) | yes |
  | `icon: "<str>"` | `Optional(icon): cv.icon` (L61) | yes (same validator as 1.3.2) |
  | `work_mode: "<x>"` (only if ≠ ingress) | `In(WorkMode)` = ingress/subapp/iframe/auth/hassio/custom (L74-L76; `const.py#L68-L74`) | yes |
  | `require_admin: true` | `Optional(require_admin, default=False): cv.boolean` (L59) | yes |
  | `{}` when empty | a slug-keyed mapping accepts `{}` | yes |

- **What changed: the config entry has a mode**, `ui` / `agent` / `yaml` (HI `const.py#L88-L91`; 1.3.x
  had only `agent` / `yaml`). It is a `single_config_entry` (manifest).
  - `async_setup`: when **no entry exists and `ingress:` is in the config**, it starts an `import`
    flow, which creates a **YAML-mode** entry (HI `__init__.py#L107-L118`; `config_flow.py#L650-L654`).
  - `async_setup_entry`: in YAML mode it re-reads the YAML (`async_integration_yaml_config`). In
    **UI mode it uses only the entry's subentries and ignores YAML** (HI `__init__.py#L121-L133`). The
    `ingress.reload` admin service does the same (L308-L320).
  - **Upgrade from 1.3.x:** a v1 YAML entry without stored panels migrates to v2 and stays YAML
    (HI `__init__.py#L213-L229`). On 1.3.2, `async_setup` created that import entry whenever none
    existed (https://github.com/lovelylain/hass_ingress/blob/1.3.2/custom_components/ingress/__init__.py#L108-L123),
    so boxes like lav918 already have a YAML-mode entry. **Their YAML keeps working after the upgrade.**
  - **Switching to UI** is a Reconfigure → "Local UI" step. The first time, it offers an explicit
    import of the YAML panels; once switched, "the `ingress:` section in `configuration.yaml` is
    ignored" (README «Move existing YAML panels to the interface»; HI `config_flow.py#L539-L563`,
    L624-L648).
  - **The setup form defaults to "Local UI"** (HI `config_flow.py#L556-L561`, default
    `ConfMode.UI`). Someone who adds Ingress through Devices & services, or through the setup page the
    Marketplace opens after an install (docs «Installing a repository»), and accepts the default
    creates a UI-mode entry. In that state `snap-ingress.yaml` is never read.
- **Verdict for our code:** `ingress sync` (write `snap-ingress.yaml`, ensure `ingress: !include`,
  restart; `cmd/launcher/ingress.go#L33-L58`) and PR #6's auto-sync **keep working on 1.4.0 as
  long as the entry is in YAML mode**, which is the state that existing installs and the YAML import
  path produce. They **fail silently** if a user switches the entry to Local UI, or creates it via
  the UI with the default. Mitigation: the mode check in `ingress sync` recommended above, plus a
  line in the `install hass-ingress` / Marketplace guidance saying "choose **YAML** if asked for a
  configuration mode".

## C. What I could not verify

- **Nothing was run against a live HA.** All Marketplace behaviour (websocket gates, no adoption,
  replace-over-copy, restart repair) comes from reading the 2026.11.1 source, not from a device.
- **What the Marketplace panel shows** for an un-adopted `custom_components/ingress` (I expect
  "Install"). I also did not check whether it prompts about overwriting.
- **The ordering on a fresh Marketplace install with `ingress: !include` already present:**
  whether the YAML import flow (from `async_setup`) or the frontend's opened setup form creates the
  single entry first. The source suggests either can win; a device test is needed.
- **Whether the HA 2026.11 image ships `unzip` and `sha256sum`** (busybox likely has both; not checked).
- **lav918's current hass_ingress entry mode** (no device access by brief). From the code it
  should be `yaml`.
- **Upstream CI on PR #115:** its body says the upstream workflow was `action_required` at the time,
  with checks passing on the fork. I did not re-check upstream Actions for the 1.4.0 tag.
