package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/reconcile"
)

// T2: only an exact data-dir or volumes.* destination covers an image VOLUME.
func TestUncoveredVolumes(t *testing.T) {
	mosq := []string{"/mosquitto/log", "/mosquitto/data"}
	cases := []struct {
		name string
		spec config.AddonSpec
		want string
	}{
		{"data-dir elsewhere", config.AddonSpec{DataDir: "/mosquitto/config"}, "/mosquitto/data,/mosquitto/log"},
		{"data-dir on one", config.AddonSpec{DataDir: "/mosquitto/data"}, "/mosquitto/log"},
		{"volumes entry with opts", config.AddonSpec{DataDir: "/mosquitto/data", Volumes: map[string]string{"x": "x:/mosquitto/log:ro"}}, ""},
		{"prefix covers nothing", config.AddonSpec{DataDir: "/mosquitto"}, "/mosquitto/data,/mosquitto/log"},
		{"trailing slash is the same path", config.AddonSpec{DataDir: "/mosquitto/data/"}, "/mosquitto/log"},
		{"nothing set", config.AddonSpec{}, "/mosquitto/data,/mosquitto/log"},
	}
	for _, tc := range cases {
		if got := strings.Join(uncoveredVolumes(mosq, tc.spec), ","); got != tc.want {
			t.Errorf("%s: uncovered = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := uncoveredVolumes(nil, config.AddonSpec{}); len(got) != 0 {
		t.Errorf("an image without VOLUMEs: %q, want none", got)
	}
}

// T3: the warning is checked only for add-ons that were just created or
// recreated; inspect errors are silent.
func TestWarnUncoveredVolumes(t *testing.T) {
	const key = "image inspect -f {{json .Config.Volumes}} "
	f := haFake("2026.11.1")
	for _, img := range []string{"a:1", "b:1", "c:1", "d:1"} {
		f.outputs[key+img] = `{"/data":{}}`
	}
	f.errs[key+"e:1"] = errors.New("No such image")
	cfg := config.Config{Addons: map[string]config.AddonSpec{
		"a": {Image: "a:1"}, "b": {Image: "b:1"}, "c": {Image: "c:1"}, "d": {Image: "d:1"}, "e": {Image: "e:1"},
	}}
	results := []reconcile.Result{
		{Name: "homeassistant", Action: reconcile.ActionCreated},
		{Name: "ha-addon-a", Action: reconcile.ActionCreated},
		{Name: "ha-addon-b", Action: reconcile.ActionRecreated},
		{Name: "ha-addon-c", Action: reconcile.ActionStarted},
		{Name: "ha-addon-d", Action: reconcile.ActionNone},
		{Name: "ha-addon-e", Action: reconcile.ActionCreated},
		{Name: "ha-addon-old", Action: reconcile.ActionRemoved},
	}
	var buf bytes.Buffer
	warnUncoveredVolumes(docker.NewWithRunner(f), cfg, results, &buf)
	want := "warning: addons.a: image a:1 declares VOLUME /data not covered by data-dir or volumes.*; their content is discarded when the add-on is recreated or removed\n" +
		"warning: addons.b: image b:1 declares VOLUME /data not covered by data-dir or volumes.*; their content is discarded when the add-on is recreated or removed\n"
	if buf.String() != want {
		t.Errorf("warnings =\n%s\nwant\n%s", buf.String(), want)
	}
	if n := f.count("image inspect"); n != 3 {
		t.Errorf("image inspect ran %d times, want 3 (a, b, e); calls %q", n, f.calls)
	}
	for _, img := range []string{"c:1", "d:1", "ghcr.io"} {
		if n := f.count(key + img); n != 0 {
			t.Errorf("%s was inspected; only created/recreated add-ons are", img)
		}
	}
}
