package ingress

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StoragePath is where Home Assistant keeps its config entries; hass_ingress's
// entry carries the mode (ui, agent or yaml) in data.mode.
const StoragePath = "/config/.storage/core.config_entries"

// ModeStatus is what EntryMode found about hass_ingress's config entry.
type ModeStatus int

const (
	ModeNoFile     ModeStatus = iota // storage absent/empty: fresh HA, nothing to check
	ModeNoEntry                      // no `ingress` entry: the import creates it on the next start
	ModeYAML                         // data.mode == "yaml": fine
	ModeOther                        // data.mode is anything else ("ui", "agent", …)
	ModeUnreadable                   // not JSON, or the entry has no string data.mode
)

// EntryMode reads hass_ingress's mode from core.config_entries. Only entries
// whose domain is "ingress" count; the first one that is not YAML decides.
func EntryMode(storage string) (ModeStatus, string) {
	if strings.TrimSpace(storage) == "" {
		return ModeNoFile, ""
	}
	var doc struct {
		Data struct {
			Entries []struct {
				Domain string         `json:"domain"`
				Data   map[string]any `json:"data"`
			} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(storage), &doc); err != nil {
		return ModeUnreadable, ""
	}
	found := false
	for _, e := range doc.Data.Entries {
		if e.Domain != "ingress" {
			continue
		}
		found = true
		mode, ok := e.Data["mode"].(string)
		if !ok {
			return ModeUnreadable, ""
		}
		if mode != "yaml" {
			return ModeOther, mode
		}
	}
	if !found {
		return ModeNoEntry, ""
	}
	return ModeYAML, "yaml"
}

// ModeWarning is the stderr text for a status, ending with a newline; "" when
// there is nothing to say (D11).
func ModeWarning(status ModeStatus, mode string) string {
	switch status {
	case ModeOther:
		return fmt.Sprintf("warning: hass_ingress is in %q mode, so it ignores configuration.yaml and the snap's ingress panels will not appear.\n", mode) +
			"  Switch it to YAML: Home Assistant → Settings → Devices & services → Ingress → ⋮ → Reconfigure → YAML.\n"
	case ModeUnreadable:
		return "warning: could not read hass_ingress's configuration mode from " + StoragePath + ".\n" +
			"  If the panels do not appear, check that Ingress is in YAML mode (Settings → Devices & services → Ingress → ⋮ → Reconfigure).\n"
	default:
		return ""
	}
}
