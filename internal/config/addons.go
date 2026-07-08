// Add-on config: port specs, the AddonSpec schema, and its validation helpers.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// PortSpec is one published add-on port. IP defaults to loopback so add-on
// UIs are reachable by HA (host netns) but invisible to the LAN — HA's auth
// stays in front. LAN exposure requires an explicit ip in the config value.
type PortSpec struct {
	IP, Host, Container string
}

// ParsePortSpec parses "port", "host:container" or "ip:host:container".
// (IPv6 ips are not supported — three colon-separated parts maximum.)
func ParsePortSpec(v string) (PortSpec, error) {
	parts := strings.Split(v, ":")
	switch len(parts) {
	case 1:
		if !validPort(parts[0]) {
			return PortSpec{}, fmt.Errorf("invalid port %q", v)
		}
		return PortSpec{IP: "127.0.0.1", Host: parts[0], Container: parts[0]}, nil
	case 2:
		if !validPort(parts[0]) || !validPort(parts[1]) {
			return PortSpec{}, fmt.Errorf("invalid port mapping %q (want host:container)", v)
		}
		return PortSpec{IP: "127.0.0.1", Host: parts[0], Container: parts[1]}, nil
	case 3:
		if parts[0] == "" || !validPort(parts[1]) || !validPort(parts[2]) {
			return PortSpec{}, fmt.Errorf("invalid port mapping %q (want ip:host:container)", v)
		}
		return PortSpec{IP: parts[0], Host: parts[1], Container: parts[2]}, nil
	default:
		return PortSpec{}, fmt.Errorf("invalid port mapping %q", v)
	}
}

func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}
