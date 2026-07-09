# Phase 3 — Add-ons as launcher-managed containers + ingress

**Date:** 2026-07-09
**Status:** Approved design (brainstormed + section-approved). Roadmap phase 3; builds on the `install` subcommand (phase 1) and hass_ingress (phase 2). Supersedes the phase-3 sketch in `docs/superpowers/plans/2026-06-28-addons-ingress-store-roadmap.md` where they differ (notably: no "managed block" in `configuration.yaml` — phase 2's `snap-ingress.yaml` + `!include` mechanism is reused as-is).

## Goal

Run **network-only** add-on containers (Node-RED, Grafana, Mosquitto, …) alongside HA Core, declared entirely via `snap set`, managed by the launcher with the same reconcile guarantees as HA itself, and surfaced in the HA sidebar via hass_ingress — with no second command required.

## Scope

- **IN:** `addons.*` config schema; a dedicated `ha-addons` docker bridge; add-on container lifecycle (create/recreate/start/orphan-removal) through a generalized reconciler; per-add-on data volume; ingress panel derivation + auto-sync during reconcile.
- **OUT:** device/USB passthrough (`devices.*` is *reserved* and rejected — own spec, "phase 3.5"); add-on volumes in `backup`/`rollback` (documented limitation); the phase-4 catalog (`addon install <name>`); scheduled auto-update of add-on images; privileged/host-network add-ons (never — confinement-safe by design).

## Decisions (settled in brainstorming)

1. **Networking = model A default, model B opt-in.** HA stays on `--network=host` (mDNS/BT discovery intact); add-ons join a user-defined bridge `ha-addons` with ports published to the host. Opt-in `docker.network=ha-addons` moves HA onto the bridge too (container-name DNS, better isolation; discovery degrades — the existing validate warning stays).
2. **Ports bind loopback by default, LAN is explicit.** `ports.<label>=1880` → `-p 127.0.0.1:1880:1880` (reachable by HA for ingress, invisible to the LAN — HA's auth is not bypassable). `ports.<label>=0.0.0.0:1883:1883` publishes LAN-wide (e.g. MQTT for devices).
3. **USB deferred.** `addons.<name>.devices.*` is a *fatal* validation error ("device passthrough lands in a later release") so the key is reserved, not half-working.
4. **Ingress auto-syncs during reconcile** (compare-first; restart HA only when the rendered file actually changed). `home-assistant.ingress sync|show` remain for manual use and dry runs.
5. **Persistence = `data-dir` convention + `volumes` escape hatch.** `data-dir=/data` auto-mounts named volume `ha-addon-<name>-data`; the volume **survives add-on removal** (data deletion is a deliberate, documented `docker volume rm`).
6. **One shared reconcile ladder.** The existing single-container logic is refactored around a `ContainerSpec` value type; HA and add-ons flow through identical code. No behavior change for HA.

## Design

### 1. Config schema (`addons.<name>.*`)

New `addons` namespace added to `loadConfig`'s namespace list (`image`, `docker`, `ingress`, `addons`). Example:

```
sudo snap set home-assistant \
  addons.nodered.image=nodered/node-red:latest \
  addons.nodered.ports.ui=1880 \
  addons.nodered.data-dir=/data \
  addons.nodered.ingress.title="Node-RED" \
  addons.nodered.ingress.icon=mdi:nodejs
```

| Key | Meaning | Default |
|---|---|---|
| `image` | image reference | **required** |
| `ports.<label>` | `port` \| `host:container` \| `ip:host:container` | loopback bind unless `ip` given |
| `data-dir` | absolute container path for state → named volume `ha-addon-<name>-data` | none |
| `volumes.<label>` | extra mount `src:dst[:opts]` (as `docker.volumes.*`) | none |
| `environment.<key>` | env vars (uppercase names via the JSON-document form, as with `docker.environment`) | none |
| `ingress.title` / `.icon` / `.require-admin` | sidebar panel; any `ingress.*` key ⇒ panel wanted | no panel |
| `ingress.port` | port label the panel proxies to | the `ui` label, else the only port |
| `devices.*` | **reserved** — fatal validate error pointing at phase 3.5 | — |

Go: `Addons map[string]AddonSpec` on `config.Config`, parsed via the existing `rawConfig` pattern. Add-on names must match `[a-z0-9-]+` (they become container/volume names).

**Validation** (extends `Config.Validate`; the configure hook's docker-free `launcher validate` keeps `snap set` atomic — bad config is rejected before anything runs):

- *Fatal:* missing `image`; unparseable `ports` value; relative `data-dir`; `volumes` value without `:`; ambiguous ingress target (panel wanted + several ports + none labeled `ui` + no `ingress.port`); `ingress.port` naming a nonexistent label; name collision between a panel-wanting `addons.<x>` and explicit `ingress.<x>`; any `devices.*` key; invalid add-on name.
- *Warnings:* unchanged from phase 2.

### 2. Networking

- `internal/docker` gains `EnsureNetwork(name)` — `network inspect` → `network create` when missing; idempotent; called by reconcile before any container that joins the bridge (always before add-ons; under model B, before HA too).
- **Model A (default):** add-ons `--network=ha-addons`, ports published per the loopback-default rule. HA (host netns) reaches them at `http://127.0.0.1:<hostport>`.
- **Model B (opt-in, `docker.network=ha-addons`):** HA joins the bridge; add-ons reachable as `http://ha-addon-<name>:<containerport>`. Published ports still apply (LAN devices). The "prefer host networking" warning stays — degraded discovery is an informed choice. Because HA's `:8123` leaves the host network namespace, `BuildRunArgs` appends `-p 8123:8123` whenever `docker.network` is `ha-addons`, so HA stays reachable from the LAN.

### 3. Container args & fingerprint (`internal/dockerargs`)

- `AddonContainerName(name)` → `ha-addon-<name>`; discovery label `io.giaever.home-assistant.addon=<name>` (orphan lookup); the existing spec-hash label carries `AddonSpecHash(name, spec)`.
- `BuildAddonRunArgs(name, spec)` mirrors `BuildRunArgs`: `run -d --name ha-addon-<name> --restart=unless-stopped --network=ha-addons`, labels, `-p` per parsed port (sorted by label), `-v ha-addon-<name>-data:<data-dir>` when set, sorted `volumes`/`environment`, then the image.
- Port-spec parsing is a small pure function: 1 part = container port (host port equal, ip `127.0.0.1`); 2 parts = `host:container` (ip `127.0.0.1`); 3 parts = `ip:host:container` verbatim.

### 4. Reconcile: one ladder, a set, orphans (`internal/reconcile`)

```go
type ContainerSpec struct { Name, Image, WantHash string; RunArgs []string }
func Reconcile(d Docker, s ContainerSpec, force bool) (Action, error)  // today's ladder, unchanged behavior
func Set(d Docker, specs []ContainerSpec, force bool) ([]Result, error)  // Result = {Name, Action, Err}
```

- `Reconcile` keeps today's exact ladder (exists? → pull+run; hash differs or force? → pull **then** remove+run, preserving the pull-before-remove downtime invariant; stopped? → start).
- `Set` reconciles HA **first**, then add-ons sorted by name. It then lists containers carrying the discovery label and removes any not in the desired set (**orphans**). Data volumes are never removed.
- **Failure isolation:** every spec is attempted; per-container errors are collected and reported together. A broken add-on image cannot block HA or its siblings. Exit code reflects container convergence only. The daemon treats add-on-only failures as warnings — HA alone governs service health (a broken add-on must not flap the snap service); the `reconcile`/`update` CLI commands still exit non-zero on any container error.
- Existing HA reconcile tests keep passing with unchanged behavior (they adapt to construct a `ContainerSpec`).

### 5. Ingress derivation + auto-sync (`internal/ingress`)

- A pure merge builds the final entry map: explicit `ingress.*` entries + one derived entry per panel-wanting add-on. Derived URL: model A → `http://127.0.0.1:<hostport>`; model B → `http://ha-addon-<name>:<containerport>`. Title defaults to the add-on name. Collisions were already fatal at validate, so the merge is total.
- **Auto-sync, after the container set converges:** render → `ReadFile /config/snap-ingress.yaml` → if identical, do nothing (no write, no HA restart — boot-time reconcile stays a no-op). If different: `WriteFile`, `EnsureInclude` (phase-2 conflict rule unchanged: a foreign `ingress:` key is warned about, never touched), restart HA.
- If panels are wanted but `custom_components/ingress` is absent: warn `run home-assistant.install hass-ingress`. Ingress failures degrade to warnings — containers are the substance, panels are cosmetic.
- `home-assistant.ingress show|sync` now operate on the merged map.

### 6. Command semantics

| Command | Behavior |
|---|---|
| daemon start / `reconcile` / configure hook | reconcile the whole set (no force) + ingress auto-sync |
| `update` | pre-update snapshot (as today), then force-reconcile the set — HA **and** add-ons pulled + recreated (safe: state is on volumes) |
| `backup` / `rollback` | unchanged; HA config only. Add-on volumes are **not** snapshotted (documented) |
| `ingress show` / `sync` | dry-run / manual apply of the merged ingress map |

No new snapcraft apps are required.

## Error handling summary

| Failure | Outcome |
|---|---|
| invalid `addons.*` config | `snap set` rejected atomically by the configure hook |
| add-on image bad / pull fails | that add-on errors; HA + siblings converge; errors reported together |
| ingress write/render problem | warning; reconcile exit unaffected |
| foreign `ingress:` key in configuration.yaml | warning + remediation text; user content never touched |
| add-on removed from config | container removed (via discovery label); data volume kept |

## Testing

Pure/unit, test-first:

- `addons` config parse + every validation rule (fatal and non-fatal paths).
- Port-spec parsing: `1880`, `8080:1880`, `0.0.0.0:1883:1883`, garbage.
- `BuildAddonRunArgs` golden args; `AddonSpecHash` stability and sensitivity.
- `reconcile.Set` against the fake `Docker`: create, recreate-on-hash-change, start-if-stopped, orphan removal, volume non-removal, failure isolation, HA-first ordering.
- Ingress derivation per networking model; merge with explicit entries; sync-only-on-change decision (pure comparison).

On-device (Pi / Ubuntu Core):

1. Node-RED end-to-end: three `snap set` keys → container up → sidebar panel appears (no second command).
2. LAN opt-in: Mosquitto with `0.0.0.0:1883:1883`, reachable from another host; a loopback-bound add-on is not.
3. Removal: unset the add-on → container gone, `ha-addon-nodered-data` volume still present; re-add → flows intact.
4. Model B smoke test: `docker.network=ha-addons`, panel URL flips to container-name form, HA still reachable at `:8123` via its published port.

## Out of scope / next

- **Phase 3.5:** `devices.*` (serial/USB passthrough, `serial-port`/`raw-usb` plugs, `snap connect` guidance).
- **Phase 4:** curated catalog + `addon install <name>` (CLI-only; no in-HA store UI).
- Add-on volume backup; scheduled add-on image auto-updates.
