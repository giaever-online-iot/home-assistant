# Phase 2 — Ingress (surface companion UIs in the HA sidebar)

**Date:** 2026-06-28
**Status:** Proposed (for review). Roadmap phase 2; builds on the `install` subcommand (phase 1). A separate PR off `main`.

## Goal

Let users put a companion service's web UI (e.g. `zwave-js-ui` at `:8091`, later add-on containers) into the Home Assistant sidebar — Supervisor-style ingress, via the maintained `hass_ingress` custom integration — driven by `snap set`.

## Scope

- **IN:** an `install hass-ingress` recipe; an `ingress.*` config schema; a `home-assistant.ingress` command that renders the `hass_ingress` YAML from config, lands it in `/config`, and restarts HA.
- **OUT:** the add-on *containers* themselves (phase 3 — ingress here points at any reachable URL, including existing companion snaps); auth-token rewrite rules and other advanced `hass_ingress` options (pass-through later).

## Design

### 1. Install recipe (`install hass-ingress`)
A new entry in the phase-1 `installRegistry`. `hass_ingress` is a custom integration (`custom_components/hass_ingress/`); no `get.*` script, so the recipe fetches the repo tarball and extracts that one directory inside the running container:
```
docker exec homeassistant bash -c '
  set -e; cd /tmp
  wget -qO hi.tar.gz https://github.com/lovelylain/hass_ingress/archive/refs/heads/main.tar.gz
  tar xzf hi.tar.gz
  mkdir -p /config/custom_components
  rm -rf /config/custom_components/hass_ingress
  cp -r hass_ingress-main/custom_components/hass_ingress /config/custom_components/
  rm -rf hi.tar.gz hass_ingress-main'
```
`Dest=/config/custom_components/hass_ingress` (idempotency probe). `Activation`: "restart HA, then configure panels with `snap set home-assistant ingress.<name>.url=…` and `home-assistant.ingress sync`." (A later hardening can pin a release tag + checksum, same as the HACS Task-6 note.)

### 2. Config schema
```
snap set home-assistant ingress.zwave.url="http://localhost:8091"
snap set home-assistant ingress.zwave.title="Z-Wave JS UI"
snap set home-assistant ingress.zwave.icon="mdi:z-wave"
```
`internal/config`: `Ingress map[string]IngressSpec` with `URL` (required), `Title`, `Icon`, `WorkMode` (default `ingress`), `RequireAdmin` (default false). `Validate`: each entry needs a non-empty `URL`; warn on an unknown `WorkMode`.

### 3. Rendering + landing the config (the crux)
`hass_ingress` reads a top-level `ingress:` key from `configuration.yaml`. We **own a separate file** and **include it**, rather than editing the user's blocks:

- Render `internal/ingress.Render(map[string]IngressSpec) string` → the YAML body (pure, unit-tested).
- Write it to **`/config/snap-ingress.yaml`** (inside the container, via `docker exec … "cat > …"`).
- Ensure `configuration.yaml` contains exactly `ingress: !include snap-ingress.yaml`:
  - if there is **no** `ingress:` key → append the include (idempotent).
  - if `ingress: !include snap-ingress.yaml` is **already** present → no-op.
  - if some **other** `ingress:` exists → do **not** touch it; print a clear warning telling the user to remove it or fold our include in. (No silent edits to user content.)
- Restart HA (`docker restart homeassistant`).

### 4. Surface: `home-assistant.ingress` command
`home-assistant.ingress sync` (and bare `home-assistant.ingress` == `sync`): gated by `preflightContainer`; renders + lands the file + ensures the include + restarts. `home-assistant.ingress show` prints the rendered YAML without applying (dry run). snapcraft.yaml gains an `ingress` app (`plugs: [docker]`).

> Why a command and not auto-on-reconcile: writing into the `/config` volume needs the container running, and editing `configuration.yaml` is sensitive — an explicit, idempotent `sync` the user runs after `snap set` is safer and predictable than mutating their config on every reconcile. (Auto-sync can be revisited once it's proven.)

## Testing
- Pure + unit-tested: `ingress.Render` (entries → YAML; ordering stable), config `Ingress` parse/validate, the "ensure include" decision (no-`ingress`/already-present/foreign-`ingress` → append/no-op/warn) as a pure function over the file text.
- On-device: `install hass-ingress` → `snap set ingress.zwave.url=http://localhost:8091 …` → `home-assistant.ingress sync` → Z-Wave JS UI appears in the HA sidebar.

## Out-of-scope / next
- Phase 3 add-on **containers** (this ingress then points at them on the `ha-addons` bridge).
- Advanced `hass_ingress` options (headers/token rewrite, `auth`/`hassio` work-modes), pinned-release install, auto-sync on reconcile.
