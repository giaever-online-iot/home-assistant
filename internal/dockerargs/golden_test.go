package dockerargs

import (
	"reflect"
	"testing"
)

// Golden values computed on main 088f717, before any W-13 change (T10). A
// changed model A hash would recreate every model A user's Home Assistant on
// upgrade (W-13 anti-goal), so these must never be edited to make a test pass.
const (
	goldenHashDefault = "e64d29989689f9d174f148ce7e5cf8de2d0691be12160419c484ab4208847b3a"
	goldenHashRich    = "b6f4bd2862cab556c07f45dbe6bebbd6e7160833cf351aeda7c6ec09441f6496"
	// Model B's hash before D10; T11 checks that D10 moves it.
	preD10HashModelB = "2345565b3c8bd2eaf7132456b7a9bcf068659b37ce680a224d029c481bc8bbe8"

	richModelA = `{"image":{"digest":"sha256:abc"},"docker":{"bluetooth":false,"config-dir":"/srv/ha","timezone":"Europe/Oslo","devices":{"z":"/dev/ttyUSB0"},"volumes":{"m":"/mnt/media:/media"},"environment":{"FOO":"bar"},"extra-args":"--cap-add=NET_ADMIN"}}`
)

func TestGoldenModelA(t *testing.T) {
	cases := []struct {
		name, json, hash string
		args             []string
	}{
		{"default", `{}`, goldenHashDefault, []string{
			"run", "-d", "--name", "homeassistant", "--restart=unless-stopped",
			"--label", "io.giaever.home-assistant.spec-hash=" + goldenHashDefault,
			"--network=host", "--privileged",
			"-v", "homeassistant-config:/config", "-v", "/run/dbus:/run/dbus:ro",
			"ghcr.io/home-assistant/home-assistant:stable",
		}},
		{"rich", richModelA, goldenHashRich, []string{
			"run", "-d", "--name", "homeassistant", "--restart=unless-stopped",
			"--label", "io.giaever.home-assistant.spec-hash=" + goldenHashRich,
			"--network=host", "--privileged",
			"-v", "/srv/ha:/config", "-e", "TZ=Europe/Oslo", "--device=/dev/ttyUSB0",
			"-v", "/mnt/media:/media", "-e", "FOO=bar", "--cap-add=NET_ADMIN",
			"ghcr.io/home-assistant/home-assistant@sha256:abc",
		}},
	}
	for _, tc := range cases {
		if got := SpecHash(cfg(tc.json)); got != tc.hash {
			t.Errorf("%s: SpecHash = %s, want the golden %s", tc.name, got, tc.hash)
		}
		if got := BuildRunArgs(cfg(tc.json)); !reflect.DeepEqual(got, tc.args) {
			t.Errorf("%s: BuildRunArgs =\n %q\nwant the golden\n %q", tc.name, got, tc.args)
		}
	}
}
