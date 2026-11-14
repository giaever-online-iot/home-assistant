# Phase 3 follow-ups: volumes, HA restarts, model B, empty maps, log replay, ufw

**Date:** 2026-11-14
**Wish:** W-13 (Chim). **Classification: bounded.** Nine small fixes and doc notes in code that already exists (`internal/docker`, `internal/reconcile`, `internal/ingress`, `internal/config`, `cmd/launcher`, the configure hook, README). Nothing new architecturally: one optional hook in the reconcile ladder, one new docker-free launcher subcommand, and no new snap plugs or interfaces.
**Status:** Draft for review.
**Builds on:** `2026-07-09-addons-phase3-design.md` (phase 3, "P3 Dn" below) and `2026-11-12-hass-ingress-pinned-design.md` ("W-12 Dn" below). main is at 088f717.

## Why

PR #6 (phase 3) passed every check on lav918 on 2026-11-13 (W-1, rev 29). That run, and a second one on 2026-11-14, found product problems that did not block the merge: a disk leak, extra restarts of a live HA, broken localhost URLs under model B, leftover `{}` in `snap get -d`, log replay into the journal, and a ufw interaction reported by the frigate orchestrator. lav918 is a live cabin box, so every extra HA restart cuts Z-Wave and the UI for 30–60 s. Each finding gets a decision below: **fix**, **document** or **defer**.

## Findings and verdicts

| Finding | Verdict | Decisions |
|---|---|---|
| F-a: removing an add-on orphans its anonymous volumes | fix + warn | D2, D3 |
| F-b: `docker.network` change starts HA twice | fix | D4, D5 |
| F-c: panel add/remove restarts HA; manual sync of unchanged panels restarts HA | fix (unchanged / comment-only), defer (reload) | D6–D9 |
| F-d: model B breaks HA-side localhost URLs | fix (panels) + warn + document (integrations) | D10–D12 |
| F-e: trusted_proxies 400 under model B did not reproduce | document only | D13 |
| F-f: `"addons": {}` / `"docker": {}` left after unset | fix | D14, D15 |
| F-h: daemon restart replays old HA log lines | fix | D16 |
| F-i: ufw (legacy iptables, FORWARD DROP) cuts bridge add-ons off | document only | D17 |
| F-g: daemon exits 143 on SIGTERM | out of scope (W-10) | D18 |

## Goals, non-goals, anti-goals

- **Goals:** no anonymous-volume leak; at most one HA start per reconcile; no HA restart when the panels did not change; model B panels that point at host services keep working; `snap get -d` free of empty maps; the journal shows each HA log line once.
- **Non-goals:** calling `ingress.reload` (D9); debouncing several separate `snap set` calls; ufw detection; cleaning volumes that are already orphaned; changing model A behavior.
- **Anti-goals (would count as failure even if tests pass):** losing a named data volume (`ha-addon-<name>-data`, `homeassistant-config`) or a bind mount; recreating HA on upgrade for model A users; a reconcile that fails, or a configure hook that rejects `snap set`, because of anything cosmetic (panels, pruning, warnings); a journal that goes silent because `--since` lies in the future.

## Verified facts this design rests on

- `Client.Remove` runs `docker rm -f <name>` without `-v` (`internal/docker/docker.go:110`). The same call serves recreate, orphan removal and rollback (`internal/reconcile/reconcile.go:68,120`, `cmd/launcher/main.go:343`).
- lav918 evidence (W-1 A1, line 37): `eclipse-mosquitto:2` left two anonymous volumes behind, for its image `VOLUME`s `/mosquitto/log` and `/mosquitto/data`. `data-dir` was `/mosquitto/config`, which is not an image VOLUME, so docker still created both anonymous volumes. This matches docker's rule that only a mount whose destination is exactly the VOLUME path replaces the anonymous volume.
- Docker documents that `docker rm -v` removes the container's anonymous volumes and that named volumes are not removed. Bind mounts are never removed. **UNVERIFIED locally** (no docker on the laptop); A1 checks it on the device.
- The configure hook restarts the daemon on every `snap set` (`snap/hooks/configure`). The daemon then runs `reconcileSet`: validate → `EnsureNetwork` → `reconcile.Set` → `applyIngress(force=false)` (`cmd/launcher/main.go:259-288`).
- `applyIngress` writes `snap-ingress.yaml` and calls `docker restart homeassistant` *after* `Set` has already recreated HA, so a network change gives two starts about 4 s apart (W-1 A1 lines 23 and 28).
- Manual `ingress sync` is `applyIngress(force=true)`: it always writes and restarts (`cmd/launcher/ingress.go:33`). That is the 2026-11-14 "unchanged panels, HA restarted" finding.
- `ShouldSync` compares whole trimmed files, header comments included (`internal/ingress/derive.go:67`). The W-1 S6 residual shows that the header text changed between snap revisions, so a header-only change would restart HA after a refresh.
- The include line in `configuration.yaml` is checked only after a write (`cmd/launcher/ingress.go:68-81`). When the file is unchanged, a missing include is never repaired.
- **hass_ingress 1.4.0** (the pinned zip; sha256 `7b2255fa…4cac` matches `hassIngressPinSHA256`): it registers `ingress.reload` with `async_register_admin_service` (`__init__.py:320`). The service re-reads the YAML and removes and re-adds the panels (`setup_domain`, `__init__.py:262-279`). `services.yaml` is 0 bytes. An admin service needs an authenticated admin caller, and the launcher holds no HA credential.
- `rawAddon.Ingress` is a pointer. `"ingress": {}` therefore parses as non-nil, which means "panel wanted" (`internal/config/addons.go:78,100`). An `addons.<x>: {}` fails validation with "image is required" (`addons.go:173-174`).
- `FollowLogs` runs `docker logs -f --tail 100` (`docker.go:113-115`). journald stamps the replayed lines with the current time, so on a quiet HA 100 lines can span about 50 min.
- In the W-1 run, `snap set` did **not** restart HA (StartedAt unchanged, W-1 A1 lines 12 and 32), even though the daemon's SIGTERM handler calls `docker stop`. That handler races the main path's exit (the W-10 exit-143 symptom), and in all six `snap set`s of that run the stop lost. D4's single start depends on that outcome: when the stop wins, HA is stopped when the new daemon reconciles, and D5's fallback (two starts) applies. See D4a.
- `ReadFile` swallows every error and returns "" (`docker.go:139-145`). `applyIngress` hands that "" to `EnsureInclude`, which returns just the include line, and writes it over `configuration.yaml` (`ingress.go:68-79`). Today that path runs only after a panel write. D8 would run it on every reconcile, so D8a hardens it first.
- No golden test pins HA's `SpecHash` today (`internal/dockerargs/dockerargs_test.go:64` only checks that it changes).

## Approaches considered (F-b, the only finding with real alternatives)

1. **Write the ingress files in the old container just before the swap (chosen, D4).** The ladder gets an optional hook that runs after the pull succeeds and before `docker rm`. HA's hook syncs the files into the shared `/config` volume through the still-running old container. The new container then boots with the final file, and the post-`Set` compare finds nothing to do. It reuses the existing exec-based I/O, and the ladder stays ingress-agnostic.
2. **Write after the new container starts, and skip the restart.** Rejected: HA may already have read `configuration.yaml` in its first seconds, so this is a race that sometimes drops the change silently.
3. **Fold the ingress body into HA's spec hash, so panel changes recreate instead of restart.** Rejected: every panel edit would then pull HA's image (network-dependent, slow on a Pi) for no gain.
4. **A throwaway helper container (`docker run --rm -v <config>:/config <ha-image> sh -c …`) to write before any start.** Rejected for now: it adds machinery for the rare *create* case (fresh install, rollback). D5 accepts a double start there.

## Decisions

**D1. Bounded, one branch (`fix/addons-followups`), one PR.** All nine findings touch code that already exists, with no new plugs and no store review. *Reason:* each fix is small and they share test fakes; splitting would cost more in merges than it buys in isolation.

### F-a: anonymous volumes

**D2. `Client.Remove` becomes `docker rm -f -v <name>`, for every caller (add-on recreate, orphan removal, HA recreate, rollback).** *Reason:* an anonymous volume can never be reached by the replacement container (a recreate gets a fresh one), so leaving it behind only leaks disk. On recreate it leaks on every `home-assistant.update`, which force-recreates all add-ons (P3 §6). `-v` never removes named volumes or bind mounts, so `ha-addon-<name>-data` (P3 D5), `homeassistant-config`, `docker.config-dir` binds and `volumes.*` entries are safe. The one behavior change is that an anonymous volume from `docker.extra-args -v /path` is now deleted on recreate instead of orphaned. Its content was already invisible to the new container, so the only thing lost is manual recovery from the orphan. This goes in the README.

**D3. Warn when an add-on image declares VOLUMEs that the config does not cover, at create or recreate.** After `Set`, for each add-on whose action was `created` or `recreated`, run `docker image inspect -f '{{json .Config.Volumes}}' <image>`. A path counts as covered only when it exactly equals `data-dir` or the destination of a `volumes.*` entry (the part after the first `:`, before any `:opts`). Message: `warning: addons.<name>: image <image> declares VOLUME <p1>, <p2> not covered by data-dir or volumes.*; their content is discarded when the add-on is recreated or removed`. Inspect errors are silent. The warning never fails the reconcile. *Reason:* with D2, that discard is final, so the user should hear about it before the data matters. The mosquitto case on lav918 is exactly this. Only create/recreate is checked, so a steady-state boot stays quiet. **Already-orphaned volumes are not cleaned:** anonymous volumes carry no label, so the snap cannot prove which ones are its own. The README gives the manual check (`docker volume ls -qf dangling=true`, then inspect before removing).

### F-b: one HA start per reconcile

**D4. The reconcile ladder gets an optional `BeforeSwap func()` on `ContainerSpec`.** `Reconcile` calls it once, in the recreate branch only, after `Pull` succeeds and before `Remove`. It is not called on create, start, no-op, or when the pull fails. It returns nothing, and the ladder ignores what it does. In `reconcileSet` (not in the pure `buildContainerSpecs`), HA's spec gets a hook that calls `syncIngressFiles(cli, cfg)`: the file and include half of D6–D8, which never restarts and is quiet except for one `note:` line when it fails. The post-`Set` `applyIngress` is unchanged in shape. Because the files are already current, its compare comes out "unchanged", so there is no write and no restart. *Reason:* the replacement HA reads the final file at its first boot, which gives one start. The old container is still running at that point, so the existing `docker exec` write path works. The ladder stays free of ingress knowledge (P3 D6), and a hook failure falls back to today's behavior (write and restart after `Set`) instead of losing the change.

**D5. Accepted leftovers: the create case and a stopped-container recreate still start twice.** When HA does not exist (fresh install, rollback) or exists but is stopped, the hook cannot `exec`. The post-`Set` write+restart runs as today. When `Remove` or `Run` fails after the hook wrote the files, the post-`Set` compare sees no change and does not restart. HA comes up with the new file on the next successful reconcile, because the hash still differs. *Reason:* all of these are rare. The cure is approach 4, which is not worth its machinery now.

**D4a. D4 is guaranteed only when HA is running at reconcile time. The `docker stop` race in the SIGTERM handler is W-10's to settle.** When the old daemon's SIGTERM handler stops HA before exiting, the new daemon sees a stopped container and D5's fallback applies. *Reason:* the handler (`main.go:378-384`) is W-10's scope (D18). In the W-1 run the stop lost every time, so the common path is one start. Writing through `docker cp` (which also works on a stopped container) was considered and rejected, because it adds a second write path for a case W-10 should remove at the source. A follow-up note for W-10: decide whether a config-driven daemon restart should stop HA at all, and keep the D16 cursor write first. A3 records `docker inspect -f '{{.State.FinishedAt}}' homeassistant` so a lost race can be told apart from a D4 bug.

### F-c: restart only when the panels changed

**D6. A pure `ingress.Decide(current, rendered string, panels int) (write, restart bool)` replaces `ShouldSync`:**

| Situation | write | restart |
|---|---|---|
| no panels and no file (fresh install, P3 rule kept) | no | no |
| trimmed content equal | no | no |
| equal once `#` comment lines are dropped (header-only change) | **yes** | **no** |
| anything else | yes | yes |

The include line is handled separately (D8). *Reason:* HA needs a restart only when the YAML changes meaning. A header-only change, as between snap revisions (W-1 S6 residual), is now written quietly. Render quotes every value, so a `#` is only ever at line start in a comment.

**D7. Manual `home-assistant.ingress sync` is compare-first too. `ingress sync --force` keeps today's unconditional write+restart.** On no change it prints `ingress: panels unchanged; Home Assistant not restarted (use --force to restart anyway)`. *Reason:* this is the 2026-11-14 finding. `--force` stays as the repair hatch, for example after hand edits or when a restart is wanted anyway. W-12 D11/D12 are kept: the mode check still runs before any write.

**D8. The include line is checked whenever there is at least one panel, not only after a write.** If `EnsureInclude` adds the line, write `configuration.yaml` and restart, even when `snap-ingress.yaml` itself was unchanged. The conflict case (a foreign `ingress:` key) now warns on every reconcile that has panels. *Reason:* without this, compare-first would take away `sync`'s repair role. A broken include is a real fault, so a loud warning is correct (W-12 D11 spirit). It costs one extra `docker exec cat` per reconcile with panels.

**D8a. `configuration.yaml` is never created and never written from a failed read.** A new client read tells three outcomes apart: present (content), absent (`test -e` exits non-zero), and error (docker could not run, or `cat` failed on a file that exists). The include line is written only when the file is present. Absent gives `warning: configuration.yaml not found yet; the ingress include is added on the next reconcile`. Error gives a warning and no write. *Reason:* today one failed `cat` can replace the user's whole `configuration.yaml` with a single line. D8 runs this path on every reconcile with panels, and D4 runs it seconds before `rm`, so the exposure grows unless the read is hardened first (anti-goal: losing user data). Never creating the file matters for the create case (D5): HA writes its default `configuration.yaml` at first boot only when the file is missing (**UNVERIFIED** in HA's source), so a snap-created one-liner would suppress HA's defaults.

**D9. Reload instead of restart is deferred.** `ingress.reload` exists in 1.4.0 but is an admin service (see the facts above). To call it, the launcher would need a long-lived HA admin token in snap config, which is a security decision of its own. Batching separate `snap set` calls (a debounce) is also out: it adds latency and a timer to a daemon that is a plain one-shot reconcile today. The README says that several add-ons in **one** `snap set` cost one restart. *Reason:* both levers cost more than the 30–60 s restart they save. A token-based reload can be its own wish if Joachim wants it (open question Q1).

### F-d: model B and localhost

**D10. Under model B, HA runs with `--add-host=host.docker.internal:host-gateway`. `SpecHash` gains an `addhost=` line only when `Network == ha-addons`.** *Reason:* this gives HA a stable name for the host (the bridge gateway IP differs per network). Model A's hash and args stay byte-identical, so model A users are not recreated on upgrade (anti-goal). A golden test pins today's model A hash (T10). Model B users get one recreate on upgrade, which is acceptable. `host-gateway` needs Docker ≥ 20.10. **UNVERIFIED** on lav918's docker snap version; A6 covers it.

**D11. Under model B, the rendered URL of an explicit `ingress.<name>` panel whose host is exactly `localhost`, `127.0.0.1` or `[::1]` becomes `host.docker.internal`, with scheme, port and path kept.** Only the render changes: snap config is untouched, and switching back to model A restores the original URLs. Derived add-on URLs keep their container-name form (P3 §5). *Reason:* `docker.network` stays one knob. Today these panels are always broken under model B (lav918's zwave panel `http://localhost:8091`: curl 000), so the rewrite is never worse. It works whenever the host service listens beyond loopback. Rejected: rewriting snap config (that mutates user input, and the change would survive a switch back to model A).

**D12. The model B warning in `Validate` says what breaks, and names what was rewritten.** It becomes: `docker.network="ha-addons": discovery and Bluetooth need host networking, and Home Assistant's "localhost" becomes its own container — integrations set up with localhost URLs (e.g. zwave_js ws://localhost:3000) lose their connection; point them at host.docker.internal; the host service must listen beyond 127.0.0.1`. It adds one line per D11-rewritten panel: `ingress.<name>: url rewritten to http://host.docker.internal:<port> under docker.network=ha-addons`. *Reason:* integrations live in HA's `.storage` and are out of the snap's reach, so warning and documenting is all it can do. The warning prints in the daemon journal for sure. **UNVERIFIED** whether snapd shows a successful configure hook's stderr to `snap set`; A6 records what the user sees. The README's model B section repeats it.

### F-e: trusted_proxies

**D13. No code. Add a README note and an optional read-only diagnostic.** Note: *under model B, a reverse proxy on the host (e.g. cloudflared) may reach HA from the bridge gateway (e.g. 172.18.0.1) instead of 127.0.0.1; if HA answers 400, add the ha-addons subnet (`docker network inspect ha-addons -f '{{(index .IPAM.Config 0).Subnet}}'`) to `http.trusted_proxies`*. *Reason:* the 400 did not reproduce (one probe returned 200, with no forwarded/untrusted lines in the log). Designing against a failure nobody has seen would be speculative, and the cause (cloudflared's origin URL, docker's userland proxy vs DNAT) was not investigated. A7 is an optional read-only look the orchestrator may run while model B is briefly on for A6.

### F-f: empty maps

**D14. The parser treats an empty JSON object as absent, at every level:** `addons.<x>: {}` (no add-on), `addons.<x>.ingress: {}` (no panel), `ingress.<x>: {}` (no panel), and the empty `ports` / `volumes` / `environment` / `devices` maps. "Empty" is recursive, using the same rule as `EmptyPaths` (D15): an object whose every value is an empty object counts as empty. For example, `addons.x: {"ports":{}, "ingress":{}}` is no add-on. *Reason:* the recursive rule makes D14 enough on its own when D15 is dropped. Literal-only emptiness would still reject the one-key-at-a-time `snap unset` that ends with `image`. Further reason: snapd leaves empty parents behind when the last child is unset (lav918: `"addons": {}`, `"docker": {}`). Two latent bugs follow from that today. Unsetting an add-on's only panel key leaves `ingress: {}`, which still means "panel wanted". Unsetting an add-on's keys one at a time leaves `addons.<x>: {}`, which is fatal ("image is required"), so the `snap unset` is rejected. **UNVERIFIED** that snapd leaves `{}` at nested levels too (seen only at top level); T13 covers the parse either way, and A8 checks it on the device.

**D15. A new docker-free `launcher prune-config`, run by the configure hook after `validate`, unsets the maximal empty-object paths in the snap's namespaces (`image`, `docker`, `ingress`, `addons`).** "Maximal" means that if every child of a map is empty, the map itself is unset and not its children. The pure core is `config.EmptyPaths(doc map[string]any) []string`, sorted. The hook calls it as `"$SNAP/bin/launcher" prune-config || true`, so pruning can never reject a `snap set`. *Reason:* it tidies `snap get -d` (F-f) on top of D14's correctness fix. **UNVERIFIED** that `snapctl unset` inside the configure hook persists with the transaction and does not re-run the hook; A8 checks it. If it does not work, D14 alone still fixes the behavior, and D15 can be dropped without touching anything else.

### F-h: log replay

**D16. The daemon follows with `docker logs -f --since S` (no `--tail`), where `S = min(daemonStart, max(cursor, haStartedAt, daemonStart − 10 min))`.**
- `cursor` is an RFC3339Nano UTC time in `$SNAP_COMMON/daemon-logs-cursor` (it survives refreshes). The daemon writes it when `FollowLogs` returns, for any reason, and as the first action of the SIGTERM handler. Missing or unparseable means it is ignored.
- `haStartedAt` comes from `docker inspect -f '{{.State.StartedAt}}' homeassistant`. On error it is ignored.
- The pure core is `logsSince(cursor, startedAt *time.Time, daemonStart time.Time) time.Time`.

*Reason, case by case:* on a `snap set` restart, the cursor is seconds old, so there is no replay (the finding). After a reboot or a power reset (lav918 has those), HA's StartedAt is newer than any cursor, so HA's whole boot log appears once. With a stale or missing cursor and a long-running HA, the 10 min floor caps the replay. The outer `min(daemonStart, …)` means `--since` never lies in the future, so a Pi clock that comes up behind cannot silence the journal (anti-goal). Rejected: `--tail 0` or `--since daemonStart` alone, which would drop HA's boot lines after every reboot. *Interplay with W-10:* this decision adds only the cursor write. W-10 owns the SIGTERM exit code and the `docker stop` race, and must keep the cursor write first.

### F-i: ufw

**D17. Document only. No detection.** A README section "Firewall (ufw) on Ubuntu Core": with the `ufw` snap enabled, ufw uses legacy iptables with a FORWARD policy of DROP, while docker's rules live in nf_tables. Packets pass both, so bridge add-ons lose outbound traffic and their published ports. Fix it with `sudo ufw default allow routed`, or more narrowly with `sudo ufw route allow in on br-<id>` and `sudo ufw route allow out on br-<id>`, where `<id>` is the first 12 characters of `docker network inspect ha-addons -f '{{.Id}}'`. *Reason:* the strictly confined snap cannot read the legacy ruleset without the `firewall-control` plug (a store review and a broad privilege, just for a warning). This was reported by the frigate orchestrator on 2026-11-13 and has not been reproduced here: **UNVERIFIED** in this repo, so the README states it as reported behavior with the remedy.

### Scope

**D18. Out of scope:** F-g / SIGTERM exit 143 (W-10), ingress TLS options (W-14), add-on volume backup (P3 out of scope), a fixed bridge interface name for `ha-addons`, IPv6 binds.

**D19. Existing tests that encode replaced behavior change on purpose, and the plan names each one.** `TestIngressSyncChecksMode` expects a restart from a no-change manual sync. It becomes `sync --force` for the restart assertion, and gains a no-force case that asserts no restart. `TestShouldSync` moves to `TestDecide`. *Reason:* the change is intended (D7), and must not read as a weakened test.

## Test cases (for the plan's TDD tasks)

- **T1** `Client.Remove` issues exactly `rm -f -v <name>` (fake runner).
- **T2** D3 coverage: image VOLUMEs `{/mosquitto/data,/mosquitto/log}` with `data-dir=/mosquitto/config` → both reported. `data-dir=/mosquitto/data` → only `/mosquitto/log`. A `volumes.*` entry `x:/mosquitto/log:ro` covers `/mosquitto/log`. A prefix (`/mosquitto`) covers nothing. Pure helper `uncoveredVolumes(imageVolumes []string, spec AddonSpec) []string`, sorted.
- **T3** D3 wiring: a warning is printed only for `created` and `recreated` add-on results. There is no inspect call for `none`, `started` or HA. An inspect error gives no output and no error.
- **T4** `Reconcile` recreate branch: the hook is called exactly once, after `Pull` and before `Remove` (order recorded in the fake).
- **T5** The hook is not called on create, start, or no-op, or when `Pull` fails, and a nil hook is fine.
- **T6** `reconcileSet` with a changed network (HA hash differs, HA running, panels differ): the recorded calls show the `snap-ingress.yaml` write before `rm -f -v homeassistant`, and zero `restart homeassistant` calls. HA's only start is the new container's `run`.
- **T7** `reconcileSet` when the HA container is absent: the write and one restart happen after `Set` (D5 fallback).
- **T8** `Decide`: each table row, plus whitespace-only differences → no write and no restart.
- **T9** Manual `sync`: unchanged → no write, no restart, and the "panels unchanged" message. `sync --force` → write and restart. The include line missing but the file unchanged → `configuration.yaml` written and HA restarted (D8). A foreign `ingress:` key with unchanged panels → warning, no write.
- **T10** Golden: `SpecHash(cfg({}))` and `BuildRunArgs(cfg({}))` equal their values on main 088f717 (computed in the plan's first task, before any change).
- **T11** Model B: args contain `--add-host=host.docker.internal:host-gateway` and the hash differs from the same config without it. Model A args contain no `--add-host`.
- **T12** D11 render under model B: `http://localhost:8091` → `http://host.docker.internal:8091`. `http://127.0.0.1:3000/x` → `http://host.docker.internal:3000/x`. `http://[::1]:80` → `http://host.docker.internal:80`. `http://localhost.example:80` and `http://192.168.1.5:80` are unchanged. Model A leaves everything unchanged. Derived add-on panels are untouched.
- **T13** D14: `{"addons":{"x":{}}}` → no add-on and validate OK. `{"addons":{"x":{"image":"i","ports":{"ui":1},"ingress":{}}}}` → `Ingress == nil`. `{"ingress":{"z":{}}}` → no panel and validate OK. `{"addons":{"x":{"ports":{},"ingress":{}}}}` → no add-on and validate OK (recursive).
- **T13a** D8a: `configuration.yaml` absent → no write and the "not found yet" warning. A read error (fake: `test -e` exits 0, `cat` fails) → no write and a warning. Present without the include → written with the include appended to the original content. No path ever writes `configuration.yaml` with content made from "".
- **T14** `EmptyPaths`: `{"addons":{}}` → `[addons]`. `{"docker":{},"ingress":{"a":{"url":"u"}}}` → `[docker]`. `{"addons":{"x":{"ingress":{}}, "y":{"image":"i"}}}` → `[addons.x]`. `{"addons":{"x":{"ingress":{}}}}` → `[addons]`. Scalars, `false` and `""` are never reported.
- **T15** The configure hook script runs `validate` before `prune-config`, and `prune-config` is guarded with `|| true` (text test, in the style of `snapcraft_test.go`).
- **T16** `logsSince`: fresh cursor → cursor. HA started after the cursor → startedAt. Both older than 10 min → daemonStart − 10 min. Cursor in the future → daemonStart. All missing → daemonStart − 10 min.
- **T17** `FollowLogs` issues `logs -f --since <RFC3339Nano> homeassistant` with no `--tail`. The cursor file is written when follow returns.
- **T18** The D12 warning text names `host.docker.internal` and each rewritten panel. There is no such warning under model A.

## Mutation targets (each must turn a test red)

- **M1** Drop `-v` from `Remove` → T1.
- **M2** Treat a path prefix as coverage in `uncoveredVolumes` → T2.
- **M3** Call `BeforeSwap` before `Pull` → T4/T5 (the pull-fails case).
- **M4** Call `BeforeSwap` in the create branch too → T5.
- **M5** Remove the hook wiring in `reconcileSet` → T6 (a restart call appears).
- **M6** Make `Decide` restart on a comment-only diff → T8.
- **M7** Make manual `sync` ignore `--force` → T9.
- **M8** Skip the include check when the file is unchanged → T9.
- **M9** Add the `addhost=` hash line unconditionally → T10.
- **M10** Rewrite loopback URLs under model A too → T12.
- **M11** Match `localhost` as a substring (rewriting `localhost.example`) → T12.
- **M12** Treat `ingress: {}` as panel wanted (revert D14) → T13.
- **M12a** Make emptiness literal-only (not recursive) → T13, the recursive case.
- **M12b** Write `configuration.yaml` when the read failed (treat an error as "") → T13a.
- **M13** Report children instead of the maximal path in `EmptyPaths` → T14.
- **M14** Drop the outer `min(daemonStart, …)` → T16 (the future-cursor case).
- **M15** Drop the 10 min floor → T16.

## Device acceptance on lav918 (the orchestrator runs these; this worker does not touch the box)

Before starting: record the baseline HA `StartedAt`, `docker volume ls -qf dangling=true | wc -l`, `snap get -d home-assistant`, and the ingress routes. Restore everything at the end, as in W-1 S6.

- **A1 (F-a)** Add `addons.mqtest` (`eclipse-mosquitto:2`, `data-dir=/mosquitto/config`). The journal shows the D3 warning naming `/mosquitto/data` and `/mosquitto/log`. Unset it: the dangling-volume count equals the baseline, `ha-addon-mqtest-data` is still present, and `homeassistant-config` is still present.
- **A2 (F-a recreate)** With the add-on back, change one env var. It is recreated, and the dangling count is unchanged.
- **A3 (F-b)** `snap set docker.network=ha-addons`: HA `StartedAt` changes exactly once, and the journal has `reconcile homeassistant: recreated` and **no** `restarting Home Assistant…`. `snap-ingress.yaml` holds the model B URLs, and `/api/ingress/<panel>/` returns 302. Revert with `snap unset docker.network`, with the same one-start check. Also record `State.FinishedAt` before and after. If HA was stopped by the old daemon's SIGTERM handler (D4a), two starts are the expected D5 fallback, not a D4 failure; note it for W-10.
- **A4 (F-c)** `home-assistant.ingress sync` with unchanged panels: StartedAt unchanged and the "panels unchanged" line printed. `ingress sync --force`: one restart.
- **A5 (F-c header)** Change only the header comment of `snap-ingress.yaml` inside the container, then `home-assistant.reconcile`: the file is rewritten and StartedAt is unchanged.
- **A6 (F-d)** Under model B (during A3): the `zwave` panel URL is `http://host.docker.internal:8091`. From the HA container, `curl -s -o /dev/null -w '%{http_code}' http://host.docker.internal:8091` → record the code (it depends on what zwave-js-ui binds). `docker inspect homeassistant` shows the `ExtraHosts` entry. Record whether the D12 warning appeared on the `snap set` terminal or only in the journal. zwave_js disconnecting from `localhost:3000` is expected.
- **A7 (F-e, optional, read-only)** While model B is on: note the source address HA logs for a request through cloudflared, and cloudflared's origin URL.
- **A8 (F-f)** After unsetting the last add-on and `docker.network`, `snap get -d` has no `addons` and no `docker` key. Set `addons.x.image=… addons.x.ports.ui=… addons.x.ingress.title=T`, then `snap unset addons.x.ingress.title`: the panel is gone and `snap set` succeeded.
- **A9 (F-h)** `snap restart home-assistant.daemon` with nothing changed: the first journal lines after the restart carry HA timestamps no older than the previous daemon's stop. Then `docker restart homeassistant` followed by `snap restart home-assistant.daemon`: HA's startup lines appear once.
- **A10 (regression)** A refresh to the build under model A: `reconcile homeassistant: none`, StartedAt unchanged, `snap-ingress.yaml` header rewritten without a restart (D6), and zwave_js connected.

## Left to the builder

- Function names and file placement, apart from the pure cores named above.
- The exact wording of the `note:` line when the D4 hook fails.
- Whether the D3 inspect reuses `Capture` or gets its own client method.
- README layout; the D2, D3, D9, D11–D13 and D17 content must all be there.

## Open questions (for Joachim, via the orchestrator)

- **Q1 (D9): Should a token-based `ingress.reload` (no HA restart for panel changes) become its own wish?** 1) No, keep restarts (recommended: a stored long-lived admin token is a bigger risk than a 30–60 s restart on a small home box). 2) Yes, as a new wish, opt-in via a snap config token. Either way, this spec does not change.
