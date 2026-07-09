package dockerargs

import (
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

func addonCfg(json string) config.AddonSpec {
	c, _ := config.Parse([]byte(json))
	for _, a := range c.Addons {
		return a
	}
	return config.AddonSpec{}
}

func TestBuildAddonRunArgs(t *testing.T) {
	a := addonCfg(`{"addons":{"nodered":{
		"image":"nodered/node-red:latest",
		"ports":{"ui":1880,"mqtt":"0.0.0.0:1883:1883"},
		"data-dir":"/data",
		"volumes":{"certs":"/etc/certs:/certs:ro"},
		"environment":{"foo":"bar"},
		"ingress":{"title":"Node-RED"}
	}}}`)
	got := strings.Join(BuildAddonRunArgs("nodered", a), " ")
	for _, want := range []string{
		"run -d", "--name ha-addon-nodered", "--restart=unless-stopped",
		"--network=ha-addons",
		"--label io.giaever.home-assistant.addon=nodered",
		"-p 0.0.0.0:1883:1883",   // explicit ip preserved
		"-p 127.0.0.1:1880:1880", // loopback default
		"-v ha-addon-nodered-data:/data",
		"-v /etc/certs:/certs:ro",
		"-e foo=bar",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n got: %s", want, got)
		}
	}
	args := BuildAddonRunArgs("nodered", a)
	if args[len(args)-1] != "nodered/node-red:latest" {
		t.Errorf("image must be the last arg, got %q", args[len(args)-1])
	}
}

func TestAddonSpecHash(t *testing.T) {
	base := `{"addons":{"x":{"image":"img:1","ports":{"ui":80}}}}`
	if AddonSpecHash("x", addonCfg(base)) != AddonSpecHash("x", addonCfg(base)) {
		t.Error("hash must be deterministic")
	}
	if AddonSpecHash("x", addonCfg(base)) == AddonSpecHash("x", addonCfg(`{"addons":{"x":{"image":"img:2","ports":{"ui":80}}}}`)) {
		t.Error("hash must change with the image")
	}
	if AddonSpecHash("x", addonCfg(base)) == AddonSpecHash("y", addonCfg(base)) {
		t.Error("hash must change with the add-on name")
	}
	// Panel changes must NOT recreate the container.
	withPanel := `{"addons":{"x":{"image":"img:1","ports":{"ui":80},"ingress":{"title":"X"}}}}`
	if AddonSpecHash("x", addonCfg(base)) != AddonSpecHash("x", addonCfg(withPanel)) {
		t.Error("ingress fields must not affect the spec hash")
	}
}
