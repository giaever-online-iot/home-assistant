package ingress

import (
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
)

// DeriveEntries turns panel-wanting add-ons into ingress entries. The URL
// depends on the networking model: on the ha-addons bridge (model B) HA
// reaches add-ons by container name; on host networking (model A) via the
// loopback-published host port.
func DeriveEntries(addons map[string]config.AddonSpec, network string) map[string]config.IngressSpec {
	entries := map[string]config.IngressSpec{}
	for name, a := range addons {
		if a.Ingress == nil {
			continue
		}
		ps, err := a.IngressPortSpec()
		if err != nil {
			continue // fatal at validate; unreachable after a successful Validate
		}
		// Model A: HA reaches the add-on via the published host port. 127.0.0.1
		// and 0.0.0.0 are both reachable from HA's own (host) netns via
		// loopback; any other ip binds the port there exclusively, so the
		// panel must target that ip instead.
		host := "127.0.0.1"
		if ps.IP != "127.0.0.1" && ps.IP != "0.0.0.0" {
			host = ps.IP
		}
		url := "http://" + host + ":" + ps.Host
		if network == dockerargs.AddonNetwork {
			url = "http://" + dockerargs.AddonContainerName(name) + ":" + ps.Container
		}
		title := a.Ingress.Title
		if title == "" {
			title = name
		}
		entries[name] = config.IngressSpec{
			URL:          url,
			Title:        title,
			Icon:         a.Ingress.Icon,
			WorkMode:     "ingress",
			RequireAdmin: a.Ingress.RequireAdmin,
		}
	}
	return entries
}

// MergedEntries is the full panel map: derived add-on panels plus explicit
// ingress.* entries. Collisions are rejected at validate; if one slips
// through anyway the explicit entry wins (deterministic).
func MergedEntries(c config.Config) map[string]config.IngressSpec {
	m := DeriveEntries(c.Addons, c.Network)
	for k, v := range c.Ingress {
		m[k] = v
	}
	return m
}

// ShouldSync decides whether reconcile's auto-sync must (re)write the ingress
// file and restart HA. current is the file's present content ("" when absent
// — and note docker's ReadFile trims whitespace, hence trimmed comparison).
// A fresh install with no panels must stay untouched: no write, no include
// line, no restart.
func ShouldSync(current, rendered string, panels int) bool {
	if panels == 0 && strings.TrimSpace(current) == "" {
		return false
	}
	return strings.TrimSpace(current) != strings.TrimSpace(rendered)
}
