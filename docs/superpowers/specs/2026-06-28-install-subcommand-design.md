# `home-assistant install <target>` — Design Spec

**Date:** 2026-06-28
**Status:** Proposed (for review). Follow-on to the v1 Container snap (PR #1); a separate change, not a blocker for merge.

## Goal

An extensible `install` subcommand that places supported, add-on-adjacent payloads into Home Assistant's `/config` — beginning with **HACS** — so the user gets a one-command, idempotent, correct install instead of hand-running `docker exec` incantations.

## Scope

- **IN:** installing **custom integrations** (files under `/config/custom_components/`), starting with HACS; restarting HA so it loads them; printing clear post-install (activation) guidance.
- **OUT (deliberately, separate later work):** *activating* OAuth integrations (UI-only — see below); add-on **containers** (roadmap phase 3); an app-store (phase 4); generic arbitrary-repo install (a later extension of the same registry).

## Architecture

A **registry** maps a target name → a **recipe**. `install <target>` resolves the target, runs its recipe against the running container's `/config`, optionally restarts HA, and prints activation guidance. The launcher already owns docker access (`docker.Client`), so no new privileges.

### Recipe model

```go
type Recipe struct {
    Name       string                      // "hacs"
    Dest       string                      // "/config/custom_components/hacs" — idempotency probe
    Install    func(c *docker.Client) error // performs the fetch+place inside the container
    Activation string                      // guidance printed after a successful install
}
// registry: map[string]Recipe — v1 has exactly one entry, "hacs".
```

### HACS recipe

HACS is a custom integration; its files live in `/config/custom_components/hacs/`. The recipe runs HACS's **documented container method** inside the running container:

```
docker exec homeassistant bash -c "wget -O - https://get.hacs.xyz | bash"
```

- Writes into the `/config` volume → persists across container recreation/updates.
- **Fallback** (if HA's image ever lacks `wget`/`unzip`): launcher downloads the HACS release zip on the host and `docker cp`s the extracted tree into the volume. (Implementation detail; exec path is primary.)
- **Security decision (flag for review):** piping a remote script (`get.hacs.xyz | bash`) is the upstream-documented method, but it's a trust decision. Preferred hardening: fetch the pinned HACS GitHub **release zip + verify checksum**, with the script as a convenience fallback. Default to the hardened path if low-cost.

### Restart

A newly-installed integration isn't loaded until HA reloads. `install` restarts the container by default (`docker restart homeassistant`), with `--no-restart` to defer. Safe: the daemon's reconcile is idempotent and the config volume is intact across restart.

### Idempotency

If `Dest` exists already (`docker exec homeassistant test -d <dest>`), report *"already installed — HACS self-updates from the UI"* and skip, unless `--force` re-runs the recipe.

## The "add integrations from the CLI" question

Installing **files** is automatable (this spec). **Activating** an integration is not uniformly so:

| Path | Headless? | Plan |
|---|---|---|
| YAML integrations | yes | a later `--configure` helper that merges a stanza into `/config/configuration.yaml` |
| REST config-flow (non-OAuth) | yes | a later optional driver (needs a long-lived token) |
| OAuth / device-auth (HACS, Google, …) | **no** | print the manual UI step |
| `.storage/core.config_entries` direct write | unsafe | **not used** (HA clobbers it at runtime) |

So **v1 `install` = files + restart + guidance.** Activation automation is a deliberately separate, opt-in follow-up — and for HACS specifically the GitHub device-auth is unavoidably a one-time UI step.

## Snap surface

- New app `install` → `command: bin/launcher install`, `plugs: [docker]` (same as the others).
- `home-assistant.install <target> [--no-restart] [--force]`.
- Unknown/empty target → print available targets, exit non-zero.
- `requireDocker()` + `preflightContainer()` apply (install needs a running container to exec into; if it isn't up, the preflight ladder already gives a clear message).

## Testing

- Pure pieces unit-tested: registry/target resolution, the idempotency-probe + install arg building (docker calls behind the faked `docker.Client`).
- The real fetch is validated on-device (like the rest of the snap).

## Future (same registry, later)

- `install hass-ingress` (roadmap phase 2 — a new recipe, same mechanism).
- Generic `install <owner>/<repo>` for arbitrary HACS-style integrations (with the same checksum/trust care).
- `--configure` activation automation for YAML / non-OAuth integrations.
