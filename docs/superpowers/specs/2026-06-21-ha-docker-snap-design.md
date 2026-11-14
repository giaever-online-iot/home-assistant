# Home Assistant Docker Snap — Design Spec

- **Date:** 2026-06-21
- **Status:** Design approved (pending spec review). Next step: implementation plan (writing-plans skill).
- **Publisher:** Giaever.online (`giaever-online`)
- **Replaces:** the legacy `home-assistant-snap` (Home Assistant Core packaged in strict confinement)

> **Process gate:** No implementation artifacts (`snapcraft.yaml`, launcher daemon, hooks, scripts) are to be written until this spec is reviewed AND an implementation plan is written and approved. Sequence: brainstorm → spec → plan → approval → implement.

---

## 1. Background & motivation

The legacy `home-assistant-snap` packages **Home Assistant Core** — the Python application installed via pip into a strictly-confined snap. As of release **2025.12**, Home Assistant has **deprecated and ceased support for the Core and Supervised installation methods**; the only officially supported methods are **Home Assistant OS** and **Home Assistant Container**. The legacy snap therefore rests on a deprecated foundation and carries structural problems:

- Strict confinement breaks integrations that install Python dependencies at runtime (HACS / custom components), e.g. `No module named 'grpclib'`.
- Bundled-library version skew (e.g. the snap's SQLite recreating the database).
- HA source must be patched to detect the snap environment.
- A snap rebuild is required for **every** HA release; in practice the published `stable` channel lagged ~7 months behind upstream.

**Goal:** Release a new snap that deploys Home Assistant via the **Container** method — running upstream's official OCI image `ghcr.io/home-assistant/home-assistant` — so that HA runs exactly as upstream builds and tests it, HA updates decouple from snap rebuilds, and the confinement-related breakage class disappears. **Ubuntu Core support is a hard requirement.**

## 2. Decision: Design A (docker-client)

The snap is a thin **launcher** that uses the snapd **`docker` interface** to drive the official Canonical **`docker` snap**, which runs the Home Assistant container.

**Rejected alternative — Design B (embedded daemon):** bundle a container runtime via the super-privileged **`docker-support`** interface. Rejected because it **cannot be installed on Ubuntu Core via a store/model**: super-privileged interfaces require a Canonical-granted snap-declaration that a brand/dedicated-store owner cannot self-issue (and the docs reserve `docker-support`'s provider side to "the Docker project"). The `docker` interface is **not** super-privileged, so Design A distributes cleanly to Ubuntu Core. Both designs require **strict** confinement (Ubuntu Core forbids classic).

## 3. Architecture

```
┌─ Ubuntu Core / Classic Linux ───────────────────────────────┐
│                                                              │
│  home-assistant snap (strict)        docker snap (strict)    │
│   ├─ launcher daemon ───docker.sock──► dockerd               │
│   ├─ configure hook                     └─► [ HA container ]  │
│   ├─ helper commands                         --network=host  │
│   └─ plugs:                                  --privileged     │
│       • docker                               /dev, /run/dbus  │
│       • docker-executables (content,         -v <config>      │
│         default-provider: docker)                            │
│       • home, network, network-bind                          │
│                                                              │
│  companion snaps (hacs / configurator / code-server)         │
│   └─ home interface ──► ~/home-assistant (shared config dir) │
└──────────────────────────────────────────────────────────────┘
```

The launcher contains **no** Home Assistant code. snapd confines only the launcher; Home Assistant runs unmodified inside upstream's image, where its filesystem is a normal writable container layer (so runtime dependency installs, HACS, etc. behave exactly as in any Docker HA install).

## 4. Components

| Component | Responsibility |
|---|---|
| **Launcher daemon** (`daemon: simple`) | On start, reconcile the HA container to the desired state derived from snap config (recreate if image/args changed), start it, stream container logs to journald (`snap logs home-assistant`). On stop, stop the container. Container also carries `--restart=unless-stopped` as a backstop. |
| **`configure` hook** | Read snap options via `snapctl get`, validate them, and trigger a reconcile so `snap set` changes take effect. |
| **Helper commands** | `home-assistant.update` (snapshot → pull → recreate), `home-assistant.rollback` (restore last snapshot + re-pin prior image), `home-assistant.backup` (manual snapshot), `home-assistant.logs`, `home-assistant.check-config`, `home-assistant.cli`. |
| **docker snap (dependency)** | Provides the Docker daemon + `docker` slot. Auto-installed via the `docker-executables` content plug (`default-provider: docker`). |

## 5. Configuration model (declarative)

Snap configuration is the **desired state**; the configure hook reconciles the container to it. Options use nested keys (`snap set home-assistant <key>=<value>`). Three tiers:

| Tier | Examples | Behavior |
|---|---|---|
| **Managed** (fixed) | `--name`, `-v <config>:/config`, `--restart`, container identity | Always set by the launcher; not user-settable. |
| **Overridable** (safe default) | `image.channel`, `image.registry`, `docker.network` (host), `docker.privileged` (true), `docker.bluetooth` (true), `docker.timezone`, `docker.devices.*` | User-settable; **validated** in the hook, which warns loudly on dangerous overrides (e.g. `docker.network` ≠ `host` breaks discovery/Bluetooth). |
| **Additive** | `docker.extra-args`, `docker.volumes.*`, `docker.environment.*` | Appended to the run command; never conflicts with managed args. |

Example:

```bash
snap set home-assistant image.channel=stable          # stable|beta|dev|<pinned e.g. 2026.6.4>
snap set home-assistant docker.timezone=Europe/Oslo
snap set home-assistant docker.devices.zigbee=/dev/serial/by-id/usb-ITead_Sonoff_Zigbee_3.0...
snap set home-assistant docker.volumes.media=/mnt/media:/media
snap set home-assistant docker.environment.PYTHONWARNINGS=ignore
snap set home-assistant docker.extra-args="--cap-add=NET_ADMIN --shm-size=256m"
```

Implementation note (for the plan, not now): snap config values return as strings / JSON subtrees, so the hook coerces booleans/integers and parses the nested device/volume/environment maps.

## 6. Container run specification

The launcher composes this from managed defaults + config (channel/tag and `<config>` shown as placeholders resolved at runtime):

```
docker run -d --name homeassistant \
  --network=host \
  --privileged \
  --restart=unless-stopped \
  -e TZ=<docker.timezone> \
  -v <config-dir>:/config \
  -v /run/dbus:/run/dbus:ro \        # when docker.bluetooth=true (default)
  --device=<each docker.devices.* entry> \
  <docker.extra-args> \
  ghcr.io/home-assistant/home-assistant:<image.channel>
```

This matches Home Assistant's officially documented Container run. `--privileged` gives the container the device access used for USB coordinators **and** disables AppArmor confinement for the container — which is what resolves Bluetooth's D-Bus AppArmor mediation, so no separate `--security-opt apparmor=unconfined` is needed while privileged. `-v /run/dbus` is added when `docker.bluetooth=true` (default). A future non-privileged hardening pass (§14) would instead need `--device` + a device-cgroup rule for USB and an explicit `--security-opt apparmor=unconfined` for Bluetooth.

## 7. Configuration & data persistence

- **`/config` location (Decision 1 — confirmed):** **default = a docker named volume** `homeassistant-config` (robust, zero-config, works on headless Ubuntu Core). Setting `docker.config-dir=<absolute path>` switches to a **bind-mount** — required for the companion snaps (`home-assistant-hacs`, `home-assistant-configurator`, `code-server`) to reach the config via their `home` interface, and for direct file editing. The bind path must be under a user `$HOME` or removable-media.
- **Conclusively ruled out (verified against snapd source + the docker snap's `snapcraft.yaml`):**
  - The launcher's own `$SNAP_DATA`/`$SNAP_COMMON` (`/var/snap/home-assistant/...`): the *separate* docker snap cannot read another snap's data dir.
  - A **content interface** to expose our data to docker: content flows producer→consumer, so docker would have to declare the *plug* — only Canonical can change the docker snap. Its existing content plugs (`gpu-2404`, `graphics-core22`) are graphics-stack-specific; its slots run the wrong direction.
- **Path constraint:** the docker snap shares host `/dev`, `/run`, `/sys`, `/etc` into its namespace, but **arbitrary** custom host paths (e.g. `/opt/...`) silently fail to bind-mount. Hence `/config` must be a named volume, a `$HOME` path, or removable-media.

## 8. Hardware support matrix (verified)

Verdicts verified against snapd source (`docker_support.go`, `mount-support.c`), Canonical's docker-snap test reports, and Canonical's official HA-on-Ubuntu-Core guide.

| Capability | Verdict | Reason |
|---|---|---|
| Zigbee USB (`/dev/ttyUSB*`, `/dev/ttyACM*`) | **Works** | `docker-support`+`privileged-containers` grants the container `/dev/** mrwkl`; host `/dev` is shared into the snap namespace. `raw-usb`/`serial-port` not needed (confirmed by Canonical Core test + real Conbee II user). Pass via `--device=`. |
| Z-Wave USB | **Works** | Same serial-coordinator mechanism. |
| Bluetooth (BlueZ/D-Bus) | **Works — validate on-device** | `/run/dbus` mounts (host `/run` shared); HA BT is pure D-Bus; needs AppArmor off for the container (covered by `--privileged`; otherwise `--security-opt apparmor=unconfined`). Caveat: host BlueZ on Core comes from the `bluez` snap — confirm adapter/system-bus exposure on hardware. |
| Thread / Matter | **Works** | Canonical's documented pattern (Matter server container + host net + `/run/dbus`); HA connects over host network. |
| mDNS / zeroconf discovery | **Works — caveat** | Multicast works under `--network=host`. The HA Alpine image lacks `libnss-mdns`, so `.local` *hostname* resolution inside the container can fail (universal to Docker HA) — mitigate with the host `avahi` snap. |
| USB live hot-plug auto-discovery | **Degraded** | Only devices passed at container start are seen; live insertion isn't auto-detected without extra `/dev/bus/usb` plumbing. Document as static-passthrough only. |

## 9. Interfaces & confinement

- **Confinement:** `strict`, `grade: stable`, `base: core26` (Canonical's recommended base; Ubuntu 26.04 LTS).
- **Platforms:** `amd64`, `arm64` (match the upstream image's published architectures).
- **Plugs (launcher snap):** `docker` (drive the daemon), `docker-executables` (content, `default-provider: docker`, auto-install), `home` (reach `~/home-assistant`), `network`, `network-bind`.
- The launcher does **not** plug `raw-usb`/`serial-port`/`bluez` — that hardware access is the docker snap's responsibility via the privileged container.

## 10. Installation & connection UX

- `snap install home-assistant` auto-pulls the docker snap (content `default-provider`).
- **Connection (Decision 2 — approved):** cross-publisher interfaces do not auto-connect, so v1 ships with a documented manual connect:
  ```bash
  snap connect home-assistant:docker docker:docker-daemon
  ```
  In parallel, file a Snap Store **auto-connection** request (forum `store-requests`; *not* super-privileged, so realistic) so a later revision becomes one-command.
- On Ubuntu Core images we control, the **gadget snap's `connections:` stanza** pre-connects at first boot, and the model seeds both snaps.

## 11. Update model & rollback

- HA version is a snap option (`image.channel = stable|beta|dev|<pinned tag>`), applied by `docker pull` + recreate. This **decouples HA updates from snap rebuilds**, eliminating the legacy ~7-month `stable` lag. The snap itself updates only when the launcher changes.
- **Upgrade safety (snapshot + rollback):** because snap-revision rollback cannot sync HA (HA upgrades are `docker pull`s, not snap refreshes), upgrade safety is handled at the docker layer:
  - `home-assistant.update`: **snapshot** the config (tar `/config` into a `homeassistant-backups` volume) and **record the running image's digest**, *then* pull + force-recreate.
  - `home-assistant.rollback`: restore the latest snapshot and **re-pin the recorded image digest** via `image.digest` (`registry@sha256:…`), then recreate — returning HA *and* its config to the last-working pair. (Restoring the pre-upgrade config is necessary because HA migrates config forward on startup, so an image downgrade alone can fail to load.)
  - `image.digest` is normally empty; when set (by rollback) it overrides `image.channel`. To resume upgrades after a rollback, clear it: `snap unset home-assistant image.digest`.
- The configure hook recreates the container whenever any spec-affecting option changes (detected via the spec-hash label).

## 12. Companion ecosystem & service-backed integrations

- **Existing companion snaps** (`home-assistant-hacs`, `home-assistant-configurator`, `code-server`) reach config via the `home` interface at `~/home-assistant` — a shift from the legacy content-interface wiring, straightforward since they are all the same publisher.
- **Service-backed integrations** (Z-Wave JS, Matter Server, Mosquitto, ESPHome dashboard) are *not* HA integrations needing a Python lib — they need a **companion service**. With a daemon present, these can run as **sibling containers** that HA connects to over the host network (Canonical documents the Matter case). This is the add-on-like capability the legacy Core snap never had.
- **Decision 3 (approved):** v1 ships **HA-only**, with the HA container fully configurable via `docker.*` + `extra-args`. Companion *containers* are deferred to **v2** under a parallel `companions.*` config namespace (e.g. `companions.matter.enable=true`), following the same declarative reconcile pattern.

## 13. Error handling

- **docker socket unreachable** (plug not connected): launcher emits a clear, actionable message naming the `snap connect` command and exits non-zero (service shows failed in `snap services`).
- **Image pull failure:** retry with backoff; keep the existing container running; surface status via `snap logs`.
- **Container crash:** docker `--restart=unless-stopped` plus the launcher's reconcile re-asserts desired state.
- **Invalid config** (`snap set`): the configure hook validates and rejects (non-zero) with a message, leaving the running container untouched.
- **Missing config dir:** created on first run with sane permissions.

## 14. Security considerations

- The HA container runs `--privileged` + `apparmor=unconfined`. This is broad, but is the upstream-documented Container posture and is no wider than the legacy snap's `physical-memory-control` / `raw-usb` posture. Document the trade-off; revisit narrowing (specific `--cap-add`, AppArmor profile) in a later revision once the minimum needed for USB+BT is empirically established.
- Access to the Docker socket is privileged (full daemon control). The launcher only issues the managed run command + validated user args; `extra-args` is a documented power-user escape hatch.

## 15. Testing strategy

- **Build/CI:** `amd64` + `arm64`.
- **VM smoke test (classic):** install snap + docker snap, connect, assert HA reachable on `:8123`, assert config persists across restart, assert `image.channel` change recreates the container.
- **Ubuntu Core image test (hard requirement):** build a Core image seeding `docker` + `home-assistant` with gadget-pre-connected interfaces; verify boot-to-HA and a statically-passed serial device (`--device=`).
- **Bluetooth on real Core hardware:** validate host BlueZ (`bluez` snap) exposure end-to-end — the one open caveat.
- **Config-matrix tests:** managed/overridable/additive option handling, dangerous-override warnings, nested device/volume parsing.

## 16. Out of scope / non-goals

- **Add-ons / the add-on store** — require the Supervisor, which is not present (this is the Container method). Permanently out of scope.
- **Supervisor-delivered features** — one-click full backups, OTBR/Z-Wave JS/Matter *add-ons*. (Companion containers in v2 partially substitute.)
- **Companion containers** — deferred to v2.
- **Migrating legacy Core-snap data** — out of scope for v1 (note as a possible later utility).

## 17. Risks & open items

- **Bluetooth host-BlueZ on Ubuntu Core** — works in principle; needs hands-on validation (the `bluez` snap's system-bus exposure).
- **Auto-connection grant** — a Store request, not a guarantee; v1 works with documented manual connect regardless.
- **Less-trodden path** — Canonical's documented Core pattern is HA-as-a-native-snap + docker only for companions. The container route here is a deliberate choice to escape the legacy Core snap's problems; note it explicitly.
- **`--privileged` / `apparmor=unconfined`** — broad permissions; tracked for later narrowing (§14).
- **USB live hot-plug** — static passthrough only in v1.

## 18. References (verified sources)

- HA deprecation of Core/Supervised: https://www.home-assistant.io/blog/2025/05/22/deprecating-core-and-supervised-installation-methods-and-32-bit-systems/
- HA Container install + run/compose: https://www.home-assistant.io/installation/linux/
- snapd `docker` interface: https://snapcraft.io/docs/reference/interfaces/docker-interface/ (incl. `docker-executables` + `default-provider: docker`)
- snapd `docker-support` (super-privileged): https://snapcraft.io/docs/reference/interfaces/docker-support-interface/
- Super-privileged interfaces / Store review: https://snapcraft.io/docs/super-privileged-interfaces
- Ubuntu Core strict-only confinement: https://documentation.ubuntu.com/security/security-features/privilege-restriction/snap-confinement/
- Deploy Docker on Ubuntu Core: https://documentation.ubuntu.com/core/how-to-guides/container-deployment/deploy-docker-from-a-snap/
- Gadget `connections:` (pre-connect at first boot): https://documentation.ubuntu.com/core/reference/gadget-snap-format/
- Dedicated store super-privileged grants: https://ubuntu.com/internet-of-things/appstore/docs/explanation/snapd-interface-connections/
- USB passthrough via docker snap on Core (no `raw-usb` needed): https://github.com/canonical/docker-snap/issues/343 and https://github.com/canonical/docker-snap/issues/56
- snapd `docker_support.go` (`/dev/** mrwkl`, `controlsDeviceCgroup`): https://github.com/canonical/snapd/blob/master/interfaces/builtin/docker_support.go
- snap-confine `mount-support.c` (host `/dev`,`/run` shared into namespace): https://github.com/canonical/snapd/blob/master/cmd/snap-confine/mount-support.c
- HA Bluetooth requirements (D-Bus, not HCI): https://www.home-assistant.io/integrations/bluetooth/
- HA USB → aiousbwatcher/inotify (2025.2): https://github.com/home-assistant/core/pull/136676
- HA integration manifest requirements: https://developers.home-assistant.io/docs/creating_integration_manifest/
