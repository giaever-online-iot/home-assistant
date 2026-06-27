package dockerargs

import (
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

func cfg(json string) config.Config { c, _ := config.Parse([]byte(json)); return c }
func join(a []string) string        { return strings.Join(a, " ") }

func TestConfigSource(t *testing.T) {
	if got := ConfigSource(cfg(`{}`)); got != "homeassistant-config" {
		t.Errorf("default source = %q, want named volume", got)
	}
	if got := ConfigSource(cfg(`{"docker":{"config-dir":"/srv/ha"}}`)); got != "/srv/ha" {
		t.Errorf("bind source = %q", got)
	}
}

func TestImageRef(t *testing.T) {
	if got := ImageRef(cfg(`{}`)); got != "ghcr.io/home-assistant/home-assistant:stable" {
		t.Errorf("ref = %q", got)
	}
	if got := ImageRef(cfg(`{"image":{"digest":"sha256:abc"}}`)); got != "ghcr.io/home-assistant/home-assistant@sha256:abc" {
		t.Errorf("digest ref = %q", got)
	}
}

func TestBuildRunArgsDefaults(t *testing.T) {
	got := join(BuildRunArgs(cfg(`{}`)))
	for _, want := range []string{
		"run -d", "--name homeassistant", "--restart=unless-stopped",
		"--network=host", "--privileged",
		"-v homeassistant-config:/config", "-v /run/dbus:/run/dbus:ro",
		"ghcr.io/home-assistant/home-assistant:stable",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n got: %s", want, got)
		}
	}
}

func TestBuildRunArgsBindAndNoBluetoothAndExtras(t *testing.T) {
	got := join(BuildRunArgs(cfg(`{"docker":{"bluetooth":false,"config-dir":"/srv/ha","timezone":"Europe/Oslo","devices":{"z":"/dev/ttyUSB0"},"environment":{"FOO":"bar"},"extra-args":"--cap-add=NET_ADMIN"}}`)))
	for _, want := range []string{"-v /srv/ha:/config", "-e TZ=Europe/Oslo", "--device=/dev/ttyUSB0", "-e FOO=bar", "--cap-add=NET_ADMIN"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n got: %s", want, got)
		}
	}
	if strings.Contains(got, "/run/dbus") {
		t.Errorf("dbus should be absent when bluetooth=false: %s", got)
	}
}

func TestImageIsLastArg(t *testing.T) {
	a := BuildRunArgs(cfg(`{}`))
	if a[len(a)-1] != "ghcr.io/home-assistant/home-assistant:stable" {
		t.Errorf("last arg = %q", a[len(a)-1])
	}
}

func TestSpecHashChanges(t *testing.T) {
	if SpecHash(cfg(`{}`)) == SpecHash(cfg(`{"image":{"channel":"beta"}}`)) {
		t.Error("spec hash should change with channel")
	}
	if SpecHash(cfg(`{}`)) == SpecHash(cfg(`{"image":{"digest":"sha256:x"}}`)) {
		t.Error("spec hash should change with digest")
	}
}
