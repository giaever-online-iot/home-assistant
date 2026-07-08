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

// AddonSpec is one launcher-managed add-on container (network-only; devices
// are reserved for a later release and rejected at validation).
type AddonSpec struct {
	Image       string
	Ports       map[string]string // label → "port" | "host:container" | "ip:host:container"
	DataDir     string            // absolute container path → named volume ha-addon-<name>-data
	Volumes     map[string]string
	Environment map[string]string
	Devices     map[string]string // parsed only to be rejected (reserved)
	Ingress     *AddonIngress     // nil = no sidebar panel wanted
}

// AddonIngress describes the add-on's sidebar panel. Port names a ports.*
// label; empty means auto (the "ui" label, else the only port).
type AddonIngress struct {
	Title        string
	Icon         string
	Port         string
	RequireAdmin bool
}

type rawAddon struct {
	Image       string            `json:"image"`
	Ports       map[string]any    `json:"ports"` // any: snapctl emits numbers unquoted
	DataDir     string            `json:"data-dir"`
	Volumes     map[string]string `json:"volumes"`
	Environment map[string]string `json:"environment"`
	Devices     map[string]string `json:"devices"`
	Ingress     *struct {
		Title        string `json:"title"`
		Icon         string `json:"icon"`
		Port         any    `json:"port"`
		RequireAdmin *bool  `json:"require-admin"`
	} `json:"ingress"`
}

func (r rawAddon) toSpec() AddonSpec {
	s := AddonSpec{
		Image:       r.Image,
		DataDir:     r.DataDir,
		Volumes:     r.Volumes,
		Environment: r.Environment,
		Devices:     r.Devices,
	}
	if len(r.Ports) > 0 {
		s.Ports = make(map[string]string, len(r.Ports))
		for k, v := range r.Ports {
			s.Ports[k] = anyToString(v)
		}
	}
	if r.Ingress != nil {
		s.Ingress = &AddonIngress{
			Title:        r.Ingress.Title,
			Icon:         r.Ingress.Icon,
			Port:         anyToString(r.Ingress.Port),
			RequireAdmin: boolOrDefault(r.Ingress.RequireAdmin, false),
		}
	}
	return s
}

// anyToString normalizes snapctl JSON scalars: integers arrive as float64 and
// %v prints them without a decimal point (1880.0 → "1880").
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
