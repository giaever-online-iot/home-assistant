package config

// HostGatewayName is the name Home Assistant uses for the host under model B
// (docker.network=ha-addons): docker maps it to the bridge gateway through
// `--add-host=host.docker.internal:host-gateway` (D10, Docker ≥ 20.10).
const HostGatewayName = "host.docker.internal"
