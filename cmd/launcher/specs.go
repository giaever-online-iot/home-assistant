package main

import (
	"sort"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
	"github.com/giaever-online-iot/home-assistant/internal/reconcile"
)

// buildContainerSpecs is the desired container set: HA first (the core is
// never hostage to an add-on), then add-ons sorted by name for deterministic
// reconcile order and output.
func buildContainerSpecs(cfg config.Config) []reconcile.ContainerSpec {
	specs := []reconcile.ContainerSpec{{
		Name:     dockerargs.ContainerName,
		Image:    dockerargs.ImageRef(cfg),
		WantHash: dockerargs.SpecHash(cfg),
		RunArgs:  dockerargs.BuildRunArgs(cfg),
	}}
	names := make([]string, 0, len(cfg.Addons))
	for n := range cfg.Addons {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		a := cfg.Addons[n]
		specs = append(specs, reconcile.ContainerSpec{
			Name:     dockerargs.AddonContainerName(n),
			Image:    a.Image,
			WantHash: dockerargs.AddonSpecHash(n, a),
			RunArgs:  dockerargs.BuildAddonRunArgs(n, a),
		})
	}
	return specs
}
