# Add-ons + Ingress + App-Store — Roadmap & Plan (phases 3–4)

**Date:** 2026-06-28
**Status:** Forward plan (for review). Builds on the `install` subcommand (phase 1) and `hass_ingress` install (phase 2). To be refined into a full spec when started; this captures the architecture, the load-bearing decisions, and the hard limits so we don't design into a wall.

**Why it's possible at all:** `hass_ingress` (Apache-2.0, maintained) reimplements Hass.io ingress as a pure-Python custom integration — no Supervisor, no host access. So we can surface add-on web UIs in the HA sidebar with auth. The Supervisor's *other* job — running/managing the add-on containers — is exactly what our launcher already does for HA Core, so we extend that. **No standard add-on manifest works (it's Supervisor-only), so the catalog/format is ours to invent.**

---

## Phase 3 — Add-ons as launcher-managed containers + ingress

**Goal:** run **network-only** "add-on" containers alongside HA Core, managed by the launcher (pull/run/reconcile/restart like HA itself), each surfaced in the HA sidebar via `hass_ingress`.

### Components

1. **Add-on config schema** — extend the existing `snap set` config:
   ```
   snap set home-assistant addons.nodered.image=nodered/node-red:latest
   snap set home-assistant addons.nodered.ports.ui=1880
   snap set home-assistant addons.nodered.ingress.title="Node-RED"
   snap set home-assistant addons.nodered.ingress.icon=mdi:nodejs
   snap set home-assistant addons.nodered.env.TZ=Europe/Oslo
   ```
   New `internal/config` sub-struct `Addons map[string]AddonSpec`; new `internal/dockerargs` builder `BuildAddonRunArgs(name, AddonSpec)` mirroring `BuildRunArgs`.

2. **Networking model — THE central decision.** Today HA runs `--network=host` (for device/mDNS discovery). That conflicts with "put HA on a docker bridge for free name-DNS." Two viable models:
   - **(A, recommended default) HA stays on host net; add-ons on a `ha-addons` bridge publishing ports to the host.** HA + ingress reach them at `http://localhost:<port>`. Keeps HA's discovery; cost is host-port management + the add-on ports being host-visible.
   - **(B, opt-in) HA + add-ons together on the `ha-addons` bridge.** Free container-name DNS (`http://nodered:1880`), better isolation; cost is HA loses host networking (discovery/mDNS integrations break). Offer via `snap set home-assistant docker.network=ha-addons`.
   Decide per-deployment; ship (A) as default, (B) as a documented opt-in. (This is the single biggest thing to settle in the phase-3 spec.)

3. **Add-on lifecycle** — the daemon's reconcile loop also reconciles each configured add-on container (pull → run → spec-hash compare → recreate on change), reusing the `reconcile` + `dockerargs.SpecHash` machinery. `home-assistant.reconcile` brings the whole set up; removing an add-on from config stops/removes its container.

4. **Ingress wiring** — `install hass-ingress` (phase 2 recipe), then **generate** `hass_ingress` config from the add-on specs into a *managed block* of `/config/configuration.yaml` (clearly delimited markers, never touching user content), reload ingress. Each add-on with an `ingress.*` block becomes a sidebar panel.

### Hard limits (from research — document, don't fight)

- **USB add-ons** (Zigbee/Z-Wave sticks, e.g. `/dev/ttyACM0`) need explicit `serial-port`/`raw-usb` snapd interfaces, manual `snap connect`, and are gated by Ubuntu Core auto-connect policy — **not automatic.** Plan a `hardware.*` config + a documented connect step; treat as a distinct sub-feature.
- **`--privileged` / `--cap-add NET_ADMIN` add-ons are out** under strict confinement. The add-on catalog must be limited to network-only/userspace add-ons.
- Feasible set: Node-RED, code-server, Mosquitto/MQTT, Zigbee2MQTT *over network*, ESPHome, Grafana, Uptime-Kuma, etc. Not feasible: anything needing host devices without an interface, or privileged host manipulation.

### Phase-3 task outline (refine into a spec first)

1. `config`: `Addons map[string]AddonSpec` + parse/validate (test-first).
2. `dockerargs`: `BuildAddonRunArgs` + `AddonSpecHash` (test-first).
3. `docker`/`reconcile`: reconcile a *set* of containers (HA + add-ons); add/remove on config change.
4. Networking: create/ensure `ha-addons` bridge; model (A) port-publish; model (B) opt-in.
5. `install hass-ingress` recipe + the managed-block `configuration.yaml` generator (test the block-merge purely).
6. USB/hardware sub-feature: `hardware.*` config + `serial-port`/`raw-usb` plugs + docs.
7. On-device validation (run Node-RED as an add-on, reach it via the HA sidebar).

---

## Phase 4 — A curated app catalog (NOT a full in-HA store UI)

**Goal:** let users install vetted add-ons by name from a curated catalog, instead of hand-writing `addons.*` config.

**Recommendation up front:** build the **CLI + curated manifest** path, and **explicitly do NOT build an in-HA store UI.** The research is clear that a real in-frontend add-on store is the high-cost part (a custom HA panel/integration + lifecycle UI, permanently chasing HA frontend changes) for marginal value over a good CLI. The 80/20 is: a catalog + `home-assistant.addon install <name>`.

### Components

1. **Our own manifest format** (no standard exists). One YAML/JSON per add-on:
   ```yaml
   name: node-red
   image: nodered/node-red:latest
   ports: { ui: 1880 }
   ingress: { title: Node-RED, icon: mdi:nodejs }
   requires_interfaces: []          # e.g. [serial-port] for USB add-ons
   notes: "..."
   ```
   Maps 1:1 onto the phase-3 `AddonSpec`.

2. **Catalog source** — a versioned git repo / embedded JSON of curated manifests (ours initially; community-contributable later). Fetched read-only.

3. **Runner** — `home-assistant.addon <install|remove|list> <name>`: resolves the manifest → writes the corresponding `addons.<name>.*` config (phase-3 schema) → reconciles → if `requires_interfaces`, prints the `snap connect` steps (can't self-connect). Reuses the phase-1 registry pattern.

4. **(Deferred / likely never) in-HA store UI** — a custom integration providing a catalog panel + install buttons that call back to the snap. High cost, frontend-coupled, security-sensitive (the panel would need a privileged channel to the host launcher). **Out of scope unless there's strong demand.**

### Phase-4 task outline

1. Manifest schema + parser (test-first).
2. Embedded/fetched catalog + `addon list`.
3. `addon install/remove` → write phase-3 config + reconcile + interface guidance.
4. Curate the initial catalog (network-only add-ons).
5. (Optional, separate) the in-HA panel — only if pursued.

---

## Sequencing & dependencies

```
phase 1 (install)  ──▶ phase 2 (install hass-ingress)  ──▶ phase 3 (add-on containers + ingress)  ──▶ phase 4 (catalog)
   (small)               (small)                              (medium; networking decision)            (medium; UI deferred)
```

Each is its own spec + plan + PR. Phase 3 is the real engineering (a container-set reconciler + the networking model + the managed `configuration.yaml` generator); phase 4 is mostly data + a thin runner if we hold the line against the in-HA store UI. **All of it stays confinement- and Ubuntu-Core-safe** — no Supervisor, no privileged/host-network/DinD anywhere — which is the whole point.
