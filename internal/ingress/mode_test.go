package ingress

import "testing"

// storage wraps entries in core.config_entries' outer shape.
func storage(entries string) string {
	return `{"version": 1, "minor_version": 5, "key": "core.config_entries", "data": {"entries": [` + entries + `]}}`
}

const (
	entryYAML  = `{"domain": "ingress", "data": {"mode": "yaml"}, "version": 2, "source": "import", "disabled_by": null}`
	entryUI    = `{"domain": "ingress", "data": {"mode": "ui"}, "version": 2, "source": "user", "disabled_by": null}`
	entryAgent = `{"domain": "ingress", "data": {"mode": "agent"}, "version": 2}`
	entryOther = `{"domain": "mqtt", "data": {"mode": "ui"}, "version": 1}`
)

func TestEntryMode(t *testing.T) {
	cases := []struct {
		name    string
		storage string
		status  ModeStatus
		mode    string
	}{
		{"absent", "", ModeNoFile, ""},
		{"blank", " \n", ModeNoFile, ""},
		{"not json", "not json", ModeUnreadable, ""},
		{"no entries", `{"data":{"entries":[]}}`, ModeNoEntry, ""},
		{"yaml", storage(entryYAML), ModeYAML, "yaml"},
		{"ui", storage(entryUI), ModeOther, "ui"},
		{"agent", storage(entryAgent), ModeOther, "agent"},
		{"no mode", storage(`{"domain": "ingress", "data": {}}`), ModeUnreadable, ""},
		{"mode null", storage(`{"domain": "ingress", "data": {"mode": null}}`), ModeUnreadable, ""},          // Review Focus 4
		{"another domain in ui mode only", storage(entryOther), ModeNoEntry, ""},                             // M9
		{"another domain in ui mode, ingress yaml", storage(entryOther + "," + entryYAML), ModeYAML, "yaml"}, // Review Focus 4
		{"two ingress entries, the second ui", storage(entryYAML + "," + entryUI), ModeOther, "ui"},
	}
	for _, tc := range cases {
		status, mode := EntryMode(tc.storage)
		if status != tc.status || mode != tc.mode {
			t.Errorf("%s: EntryMode = (%d, %q), want (%d, %q)", tc.name, status, mode, tc.status, tc.mode)
		}
	}
}

func TestModeWarning(t *testing.T) {
	cases := []struct {
		status ModeStatus
		mode   string
		want   string
	}{
		{ModeNoFile, "", ""},
		{ModeNoEntry, "", ""},
		{ModeYAML, "yaml", ""},
		{ModeOther, "ui", "warning: hass_ingress is in \"ui\" mode, so it ignores configuration.yaml and the snap's ingress panels will not appear.\n" +
			"  Switch it to YAML: Home Assistant → Settings → Devices & services → Ingress → ⋮ → Reconfigure → YAML.\n"},
		{ModeOther, "agent", "warning: hass_ingress is in \"agent\" mode, so it ignores configuration.yaml and the snap's ingress panels will not appear.\n" +
			"  Switch it to YAML: Home Assistant → Settings → Devices & services → Ingress → ⋮ → Reconfigure → YAML.\n"},
		{ModeUnreadable, "", "warning: could not read hass_ingress's configuration mode from /config/.storage/core.config_entries.\n" +
			"  If the panels do not appear, check that Ingress is in YAML mode (Settings → Devices & services → Ingress → ⋮ → Reconfigure).\n"},
	}
	for _, tc := range cases {
		if got := ModeWarning(tc.status, tc.mode); got != tc.want {
			t.Errorf("ModeWarning(%d, %q) =\n%q\nwant\n%q", tc.status, tc.mode, got, tc.want)
		}
	}
}
