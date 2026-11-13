// Package ingress renders the hass_ingress configuration from the snap config
// and wires it into the user's configuration.yaml without clobbering their
// existing content. Pure functions — no I/O — so they are unit-testable.
package ingress

import (
	"fmt"
	"sort"
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

// IncludeFile is the snap-managed file (relative to /config) that holds the
// ingress entries; configuration.yaml references it via `ingress: !include`.
const IncludeFile = "snap-ingress.yaml"

const includeLine = "ingress: !include " + IncludeFile

// Render emits the body of the snap-managed ingress file: the mapping that
// becomes the value of hass_ingress's top-level `ingress:` key (it is included
// via `!include`, so it must NOT repeat the `ingress:` key itself). Entries are
// sorted by name for stable, diff-friendly output.
func Render(entries map[string]config.IngressSpec) string {
	var b strings.Builder
	b.WriteString("# Managed by the home-assistant snap — panels come from `snap set home-assistant ingress.*` and `addons.*`.\n")
	b.WriteString("# Synced automatically on reconcile (manual: `home-assistant.ingress sync`). Do not edit by hand.\n")
	if len(entries) == 0 {
		b.WriteString("{}\n")
		return b.String()
	}
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		e := entries[n]
		fmt.Fprintf(&b, "%s:\n", n)
		fmt.Fprintf(&b, "  url: %s\n", yamlStr(e.URL))
		if e.Title != "" {
			fmt.Fprintf(&b, "  title: %s\n", yamlStr(e.Title))
		}
		if e.Icon != "" {
			fmt.Fprintf(&b, "  icon: %s\n", yamlStr(e.Icon))
		}
		if e.WorkMode != "" && e.WorkMode != "ingress" {
			fmt.Fprintf(&b, "  work_mode: %s\n", yamlStr(e.WorkMode))
		}
		if e.RequireAdmin {
			b.WriteString("  require_admin: true\n")
		}
	}
	return b.String()
}

// yamlStr double-quotes a scalar so URLs (with `:`) and titles (with spaces or
// special characters) are always valid YAML.
func yamlStr(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// EnsureInclude makes configuration.yaml include the snap-managed ingress file.
// It returns the (possibly updated) content, whether it changed, and whether a
// conflicting non-snap `ingress:` key already exists — in which case the content
// is left untouched and the caller should warn rather than overwrite user config.
func EnsureInclude(configYAML string) (out string, changed, conflict bool) {
	for _, ln := range strings.Split(configYAML, "\n") {
		// A top-level `ingress:` key starts at column 0.
		if strings.HasPrefix(ln, "ingress:") {
			t := strings.TrimSpace(ln)
			if strings.Contains(t, "!include") && strings.Contains(t, IncludeFile) {
				return configYAML, false, false // our include is already present
			}
			return configYAML, false, true // some other ingress: key — don't touch it
		}
	}
	out = configYAML
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out + includeLine + "\n", true, false
}
