package ingress

import (
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

func TestShouldSync(t *testing.T) {
	cases := []struct {
		current, rendered string
		panels            int
		want              bool
	}{
		{"", "{}", 0, false},                 // nothing wanted, nothing there: fresh install stays untouched
		{"", "nodered:\n", 1, true},          // first panel
		{"nodered:", "nodered:\n", 1, false}, // ReadFile trims; trimmed-equal = no restart
		{"old:", "new:\n", 1, true},          // changed
		{"old:", "{}\n", 0, true},            // last panel removed → write the empty map
	}
	for i, c := range cases {
		if got := ShouldSync(c.current, c.rendered, c.panels); got != c.want {
			t.Errorf("case %d: ShouldSync = %v, want %v", i, got, c.want)
		}
	}
}
