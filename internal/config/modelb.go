package config

import (
	"net/url"
	"sort"
)

// HostGatewayName is the name Home Assistant uses for the host under model B
// (docker.network=ha-addons): docker maps it through
// `--add-host=host.docker.internal:host-gateway` (D10, Docker ≥ 20.10).
// Docker's host-gateway resolves to the default bridge (docker0) gateway IP,
// or the daemon's configured host-gateway-ip: an address of the host.
const HostGatewayName = "host.docker.internal"

// modelBWarning is D12's text: what model B breaks, and the way out.
const modelBWarning = `docker.network="ha-addons": discovery and Bluetooth need host networking, and Home Assistant's "localhost" becomes its own container — integrations set up with localhost URLs (e.g. zwave_js ws://localhost:3000) lose their connection; point them at host.docker.internal; the host service must listen beyond 127.0.0.1`

// LoopbackToHostGateway rewrites a URL whose host is exactly localhost,
// 127.0.0.1 or ::1 to HostGatewayName, keeping scheme, port, path and query
// (D11). ok is false, and raw comes back unchanged, for any other host or a
// URL without a host.
func LoopbackToHostGateway(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw, false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return raw, false
	}
	host := HostGatewayName
	if p := u.Port(); p != "" {
		host += ":" + p
	}
	u.Host = host
	return u.String(), true
}

// modelBWarnings is D12: the model B warning, then one line per explicit
// panel that render rewrites (sorted by name).
func (c Config) modelBWarnings() []string {
	warnings := []string{modelBWarning}
	names := make([]string, 0, len(c.Ingress))
	for n := range c.Ingress {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if u, ok := LoopbackToHostGateway(c.Ingress[n].URL); ok {
			warnings = append(warnings, "ingress."+n+": url rewritten to "+u+" under docker.network="+addonsNetworkName)
		}
	}
	return warnings
}
