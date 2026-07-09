package dockerargs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

const (
	// AddonNetwork is the docker bridge all add-on containers join (and HA
	// too, under the opt-in model B via docker.network=ha-addons).
	AddonNetwork = "ha-addons"
	// AddonLabelKey marks launcher-managed add-on containers; orphan cleanup
	// only ever removes containers carrying it.
	AddonLabelKey = "io.giaever.home-assistant.addon"

	addonPrefix = "ha-addon-"
)

func AddonContainerName(name string) string { return addonPrefix + name }

// AddonDataVolume is the named volume behind data-dir. It deliberately
// survives add-on removal — deleting data is a manual `docker volume rm`.
func AddonDataVolume(name string) string { return addonPrefix + name + "-data" }

// BuildAddonRunArgs returns the full `docker run` argument list for one
// add-on (including "run"). Ports were validated; unparseable entries are
// skipped rather than crashing the args builder.
func BuildAddonRunArgs(name string, s config.AddonSpec) []string {
	args := []string{
		"run", "-d",
		"--name", AddonContainerName(name),
		"--restart=unless-stopped",
		"--label", SpecHashLabel + "=" + AddonSpecHash(name, s),
		"--label", AddonLabelKey + "=" + name,
		"--network=" + AddonNetwork,
	}
	for _, l := range sortedKeys(s.Ports) {
		ps, err := config.ParsePortSpec(s.Ports[l])
		if err != nil {
			continue
		}
		args = append(args, "-p", ps.IP+":"+ps.Host+":"+ps.Container)
	}
	if s.DataDir != "" {
		args = append(args, "-v", AddonDataVolume(name)+":"+s.DataDir)
	}
	for _, k := range sortedKeys(s.Volumes) {
		args = append(args, "-v", s.Volumes[k])
	}
	for _, k := range sortedKeys(s.Environment) {
		args = append(args, "-e", k+"="+s.Environment[k])
	}
	return append(args, s.Image)
}

// AddonSpecHash fingerprints everything that affects the container. Ingress
// fields are deliberately excluded: a panel tweak must not recreate the
// container (the panel lives in HA's config, not in docker).
func AddonSpecHash(name string, s config.AddonSpec) string {
	h := sha256.New()
	fmt.Fprintf(h, "name=%s\nimage=%s\ndata=%s\n", name, s.Image, s.DataDir)
	for _, k := range sortedKeys(s.Ports) {
		fmt.Fprintf(h, "port:%s=%s\n", k, s.Ports[k])
	}
	for _, k := range sortedKeys(s.Volumes) {
		fmt.Fprintf(h, "vol:%s=%s\n", k, s.Volumes[k])
	}
	for _, k := range sortedKeys(s.Environment) {
		fmt.Fprintf(h, "env:%s=%s\n", k, s.Environment[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}
