package ingress

import (
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

func parse(t *testing.T, in string) config.Config {
	t.Helper()
	c, err := config.Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDeriveEntriesModelA(t *testing.T) {
	c := parse(t, `{"addons":{
		"nodered":{"image":"i","ports":{"ui":"8080:1880"},"ingress":{"icon":"mdi:nodejs"}},
		"quiet":{"image":"i","ports":{"ui":80}}
	}}`)
	got := DeriveEntries(c.Addons, c.Network) // default network = host → model A
	if len(got) != 1 {
		t.Fatalf("only panel-wanting add-ons derive entries: %+v", got)
	}
	e := got["nodered"]
	if e.URL != "http://127.0.0.1:8080" { // host port, loopback
		t.Errorf("URL = %q", e.URL)
	}
	if e.Title != "nodered" { // title defaults to the add-on name
		t.Errorf("Title = %q", e.Title)
	}
	if e.Icon != "mdi:nodejs" || e.WorkMode != "ingress" {
		t.Errorf("entry = %+v", e)
	}
}

func TestDeriveEntriesModelAExplicitBindIP(t *testing.T) {
	c := parse(t, `{"addons":{
		"nodered":{"image":"i","ports":{"ui":"192.168.1.5:8080:80"},"ingress":{}}
	}}`)
	e := DeriveEntries(c.Addons, c.Network)["nodered"]
	if e.URL != "http://192.168.1.5:8080" { // bound to a specific ip: only that ip is reachable
		t.Errorf("URL = %q", e.URL)
	}

	c = parse(t, `{"addons":{
		"mqtt":{"image":"i","ports":{"ui":"0.0.0.0:1883:1883"},"ingress":{}}
	}}`)
	e = DeriveEntries(c.Addons, c.Network)["mqtt"]
	if e.URL != "http://127.0.0.1:1883" { // 0.0.0.0 is reachable via loopback too
		t.Errorf("URL = %q", e.URL)
	}
}

func TestDeriveEntriesModelB(t *testing.T) {
	c := parse(t, `{"docker":{"network":"ha-addons"},"addons":{
		"nodered":{"image":"i","ports":{"ui":"8080:1880"},"ingress":{"title":"Node-RED"}}
	}}`)
	e := DeriveEntries(c.Addons, c.Network)["nodered"]
	if e.URL != "http://ha-addon-nodered:1880" { // container name + container port
		t.Errorf("URL = %q", e.URL)
	}
	if e.Title != "Node-RED" {
		t.Errorf("Title = %q", e.Title)
	}
}

func TestMergedEntries(t *testing.T) {
	c := parse(t, `{
		"ingress":{"zwave":{"url":"http://localhost:8091","title":"Z-Wave"}},
		"addons":{"nodered":{"image":"i","ports":{"ui":1880},"ingress":{}}}
	}`)
	got := MergedEntries(c)
	if len(got) != 2 || got["zwave"].URL != "http://localhost:8091" || got["nodered"].URL == "" {
		t.Errorf("merged = %+v", got)
	}
}

// T8 (D6, replaces TestShouldSync per D19): write and restart only when the
// YAML changes meaning; a comment-only change is written without a restart.
func TestDecide(t *testing.T) {
	const (
		oldHdr = "# Managed by the home-assistant snap (old wording).\n"
		newHdr = "# Managed by the home-assistant snap — new wording.\n# Second header line.\n"
		body   = "nodered:\n  url: \"http://127.0.0.1:1880\"\n"
	)
	cases := []struct {
		name              string
		current, rendered string
		panels            int
		write, restart    bool
	}{
		{"fresh install: no panels, no file", "", newHdr + "{}\n", 0, false, false},
		{"first panel", "", newHdr + body, 1, true, true},
		{"trimmed-equal (ReadFile trims)", strings.TrimSpace(newHdr + body), newHdr + body, 1, false, false},
		{"whitespace at the ends only", "\n\n" + newHdr + body + "\n  \n", newHdr + body, 1, false, false},
		{"header-only change", oldHdr + body, newHdr + body, 1, true, false},
		{"header added to a headerless file", body, newHdr + body, 1, true, false},
		{"panel changed", newHdr + "old:\n  url: \"http://x\"\n", newHdr + body, 1, true, true},
		{"last panel removed", newHdr + body, newHdr + "{}\n", 0, true, true},
		{"header change on the empty map", oldHdr + "{}", newHdr + "{}\n", 0, true, false},
	}
	for _, tc := range cases {
		write, restart := Decide(tc.current, tc.rendered, tc.panels)
		if write != tc.write || restart != tc.restart {
			t.Errorf("%s: Decide = (write %v, restart %v), want (%v, %v)", tc.name, write, restart, tc.write, tc.restart)
		}
	}
}

// T12 (D11): under model B, explicit loopback panels render with the host's
// name; model A and derived add-on panels are untouched.
func TestMergedEntriesModelBLoopback(t *testing.T) {
	const body = `"ingress":{
		"zwave":{"url":"http://localhost:8091"},
		"lan":{"url":"http://localhost.example:80"}
	},"addons":{"nodered":{"image":"i","ports":{"ui":"8080:1880"},"ingress":{"title":"Node-RED"}}}`
	b := MergedEntries(parse(t, `{"docker":{"network":"ha-addons"},`+body+`}`))
	if got := b["zwave"].URL; got != "http://host.docker.internal:8091" {
		t.Errorf("model B zwave URL = %q, want http://host.docker.internal:8091", got)
	}
	if got := b["lan"].URL; got != "http://localhost.example:80" {
		t.Errorf("model B lan URL = %q, want it unchanged", got)
	}
	if got := b["nodered"].URL; got != "http://ha-addon-nodered:1880" {
		t.Errorf("model B derived URL = %q, want the container-name form", got)
	}
	a := MergedEntries(parse(t, `{`+body+`}`))
	if got := a["zwave"].URL; got != "http://localhost:8091" {
		t.Errorf("model A zwave URL = %q, want it unchanged", got)
	}
	if got := a["nodered"].URL; got != "http://127.0.0.1:8080" {
		t.Errorf("model A derived URL = %q", got)
	}
}
