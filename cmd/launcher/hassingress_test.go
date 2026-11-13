package main

import (
	"errors"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
)

func TestParsePluginVersion(t *testing.T) {
	cases := []struct {
		raw string
		v   [3]int
		ok  bool
	}{
		{"1.4.0", [3]int{1, 4, 0}, true},
		{"1.3.2", [3]int{1, 3, 2}, true},
		{"1.10.0", [3]int{1, 10, 0}, true},
		{"1.4", [3]int{1, 4, 0}, true},
		{"1.4.2", [3]int{1, 4, 2}, true},
		{"1.5.0b1", [3]int{1, 5, 0}, true},
		{" 1.4.0\n", [3]int{1, 4, 0}, true}, // Review Focus 3
		{"", [3]int{}, false},
		{"v1.4.0", [3]int{}, false},
		{"1", [3]int{}, false},
		{"1.x.0", [3]int{}, false},
		{"1.4.0.1", [3]int{}, false},
		{".4.0", [3]int{}, false},
		{"1.4.b1", [3]int{}, false}, // the last field must start with a digit
	}
	for _, tc := range cases {
		v, ok := parsePluginVersion(tc.raw)
		if v != tc.v || ok != tc.ok {
			t.Errorf("parsePluginVersion(%q) = (%v, %v), want (%v, %v)", tc.raw, v, ok, tc.v, tc.ok)
		}
	}
}

func TestComparePluginVersion(t *testing.T) {
	pin := [3]int{1, 4, 0}
	cases := []struct {
		raw  string
		want int // vs 1.4.0
	}{
		{"1.4.0", 0},
		{"1.3.2", -1},
		{"1.10.0", 1}, // numeric, not string (M5)
		{"1.4", 0},
		{"1.4.2", 1},
		{"1.5.0b1", 1},
		{"0.9.9", -1},
		{"2.0.0", 1},
	}
	for _, tc := range cases {
		v, ok := parsePluginVersion(tc.raw)
		if !ok {
			t.Fatalf("parsePluginVersion(%q) not ok", tc.raw)
		}
		if got := comparePluginVersion(v, pin); got != tc.want {
			t.Errorf("comparePluginVersion(%v, %v) = %d, want %d", v, pin, got, tc.want)
		}
	}
}

const keyManifest = "exec homeassistant cat /config/custom_components/ingress/manifest.json"

// withManifest makes the fake's cat of the ingress manifest answer content;
// "" leaves the file missing (cat fails, as in production).
func withManifest(f *fakeRunner, content string) *fakeRunner {
	if content == "" {
		f.errs[keyManifest] = errors.New("exit status 1")
	} else {
		f.outputs[keyManifest] = content
	}
	return f
}

func manifestJSON(version string) string {
	return `{"domain": "ingress", "name": "Ingress", "version": "` + version + `"}`
}

const (
	wantPointer   = "To get updates, install Ingress from Home Assistant → Settings → Marketplace (2026.11 and later; accept the one-time risk notice first). It replaces this copy and keeps it up to date. If Home Assistant asks how to configure Ingress, choose YAML."
	wantOlder132  = `hass-ingress 1.3.2 is installed; this snap pins 1.4.0. Run "home-assistant.install hass-ingress --force" to upgrade.`
	wantEqual     = "hass-ingress 1.4.0 (the pinned version) is already installed."
	wantNewer142  = "hass-ingress 1.4.2 is installed, newer than the 1.4.0 this snap pins (probably from the Marketplace); leaving it alone."
	wantRefuse142 = "hass-ingress 1.4.2 is installed, newer than the 1.4.0 this snap pins; not downgrading. To force the pinned version, remove /config/custom_components/ingress first."
)

func TestHassIngressGate(t *testing.T) {
	cases := []struct {
		ha       string // .HA_VERSION; "" = missing
		manifest string // manifest.json; "" = missing
		force    bool
		proceed  bool
		notice   string
	}{
		{"2026.11.1", "", false, true, wantPointer},
		{"2026.11.1", "", true, true, wantPointer},
		{"2026.11.1", manifestJSON("1.3.2"), false, false, wantOlder132 + "\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.3.2"), true, true, wantPointer},
		{"2026.11.1", manifestJSON("1.4.0"), false, false, wantEqual + "\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.4.0"), true, true, wantPointer},
		{"2026.11.1", manifestJSON("1.4"), false, false, wantEqual + "\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.4.2"), false, false, wantNewer142 + "\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.4.2"), true, false, wantRefuse142 + "\n" + wantPointer}, // M4
		{"2026.11.1", manifestJSON("1.10.0"), true, false, "hass-ingress 1.10.0 is installed, newer than the 1.4.0 this snap pins; not downgrading. To force the pinned version, remove /config/custom_components/ingress first.\n" + wantPointer},
		{"2026.11.1", manifestJSON("1.5.0b1"), false, false, "hass-ingress 1.5.0b1 is installed, newer than the 1.4.0 this snap pins (probably from the Marketplace); leaving it alone.\n" + wantPointer},
		{"2026.10.3", manifestJSON("1.3.2"), false, false, wantOlder132}, // no pointer below 2026.11 (M10)
		{"2026.10.3", manifestJSON("1.4.2"), true, false, wantRefuse142},
		{"2026.10.3", "", false, true, ""},
		{"", "", false, true, wantPointer}, // unknown HA version counts as 2026.11+ (D7)
		{"garbage", manifestJSON("1.4.0"), false, false, wantEqual + "\n" + wantPointer},
		{"2026.11.1", "not json", false, true, wantPointer},              // unknown installed version
		{"2026.11.1", `{"domain": "ingress"}`, false, true, wantPointer}, // no version
		{"2026.11.1", manifestJSON("v1.4.0"), false, true, wantPointer},  // unparsable version
		{"2026.11.1", `{"version": 1.4}`, false, true, wantPointer},      // Review Focus 3: a number, not a string
	}
	for _, tc := range cases {
		f := withManifest(haFake(tc.ha), tc.manifest)
		proceed, notice, err := hassIngressGate(docker.NewWithRunner(f), tc.force)
		if err != nil {
			t.Errorf("gate(ha %q, manifest %q, force %v) err = %v", tc.ha, tc.manifest, tc.force, err)
			continue
		}
		if proceed != tc.proceed || notice != tc.notice {
			t.Errorf("gate(ha %q, manifest %q, force %v) = (%v,\n%q)\nwant (%v,\n%q)",
				tc.ha, tc.manifest, tc.force, proceed, notice, tc.proceed, tc.notice)
		}
	}
}
