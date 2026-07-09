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
- **Bridge mode**: `docker.network=ha-addons` opts Home Assistant onto the ha-addons bridge. Panels switch to container-name URLs, `:8123` is auto-published, but discovery and mDNS degrade.
- **Devices**: The `devices.*` namespace is reserved for a later release.
- **Backups**: Add-on volumes are not included in `backup` or `rollback` snapshots.
