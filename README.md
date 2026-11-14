# Home Assistant
[![PR Build Snap](https://github.com/giaever-online-iot/home-assistant/actions/workflows/pr-build-snap.yml/badge.svg?branch=main)](https://github.com/giaever-online-iot/home-assistant/actions/workflows/pr-build-snap.yml)

Open source home automation that puts local control and privacy first. Powered by a worldwide community of tinkerers and DIY enthusiasts.

## Add-ons

Run add-on containers (network-only) beside Home Assistant, with an optional sidebar panel (requires `home-assistant.install hass-ingress`):

```bash
snap set home-assistant \
  addons.nodered.image=nodered/node-red:latest \
  addons.nodered.ports.ui=1880 \
  addons.nodered.data-dir=/data \
  addons.nodered.ingress.title="Node-RED"
```

Ports bind to 127.0.0.1 by default (reachable through the sidebar only); publish LAN-wide with an explicit ip, e.g. `addons.mqtt.ports.broker=0.0.0.0:1883:1883`. Unsetting an add-on removes its container but keeps its data volume (`ha-addon-<name>-data`).

### Notes

- **Environment variables**: Uppercase names require the JSON-document form: `sudo snap set home-assistant addons.x.environment='{"TZ":"Europe/Oslo"}'`
- **Panels**: An add-on gets a sidebar panel when it has at least one `ingress.*` key, e.g. `addons.nodered.ingress.title="Node-RED"`. An empty map counts as unset, at every level.
- **Include line**: The snap adds `ingress: !include snap-ingress.yaml` to `configuration.yaml` only when `configuration.yaml` exists and is not empty; it never creates it. Until it has content, every reconcile with panels prints a warning and the panels are not loaded.
- **Restarts**: hass_ingress reads its YAML only at startup, so Home Assistant restarts (30–60 s) whenever the panels change. Several add-ons set in **one** `snap set` cost one restart; separate `snap set` calls cost one each. `home-assistant.ingress sync` restarts only when the panels changed or the include line had to be added; `home-assistant.ingress sync --force` always writes the panel file and restarts.
- **Anonymous volumes**: Recreating or removing an add-on deletes its anonymous volumes (`docker rm -v`). Named volumes (`ha-addon-<name>-data`, `homeassistant-config`) and bind mounts are never deleted. An image `VOLUME` that neither `data-dir` nor a `volumes.*` entry covers at exactly that path is anonymous: the reconcile warns about it, and its content is discarded on the next recreate or removal. The same goes for an anonymous `-v /path` in `docker.extra-args`. Volumes orphaned by earlier releases are not cleaned up: list them with `docker volume ls -qf dangling=true`, check each with `docker volume inspect`, and remove only the ones you recognise.
- **Devices**: The `devices.*` namespace is reserved for a later release.
- **Backups**: Add-on volumes are not included in `backup` or `rollback` snapshots.

### Bridge mode (model B)

`docker.network=ha-addons` moves Home Assistant onto the ha-addons bridge. Panels switch to container-name URLs and `:8123` is auto-published, but discovery, mDNS and Bluetooth degrade. Home Assistant's `localhost` is now its own container:

- Explicit panels whose URL host is `localhost`, `127.0.0.1` or `[::1]` are rendered with `host.docker.internal` instead. Home Assistant runs with `--add-host=host.docker.internal:host-gateway` (Docker 20.10 or later). Snap config is not changed, so switching back to `docker.network=host` restores the original URL. The host service must listen beyond `127.0.0.1` for the panel to work.
- Integrations set up with localhost URLs (e.g. zwave_js `ws://localhost:3000`) lose their connection. The snap cannot change them; point them at `host.docker.internal` in Home Assistant.
- A reverse proxy on the host (e.g. cloudflared) may reach Home Assistant from the bridge gateway (e.g. 172.18.0.1) instead of 127.0.0.1. If Home Assistant answers 400, add the ha-addons subnet to `http.trusted_proxies`. Find the subnet with `docker network inspect ha-addons -f '{{(index .IPAM.Config 0).Subnet}}'`.

### Firewall (ufw) on Ubuntu Core

Reported behaviour, not reproduced in this repository: with the `ufw` snap enabled, ufw uses legacy iptables with a FORWARD policy of DROP, while docker's rules live in nf_tables. Packets have to pass both, so bridge add-ons lose outbound traffic and their published ports. Allow routed traffic with `sudo ufw default allow routed`, or more narrowly with `sudo ufw route allow in on br-<id>` and `sudo ufw route allow out on br-<id>`, where `<id>` is the first 12 characters of `docker network inspect ha-addons -f '{{.Id}}'`.
