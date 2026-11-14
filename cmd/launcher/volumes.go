package main

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
	"github.com/giaever-online-iot/home-assistant/internal/reconcile"
)

// uncoveredVolumes returns, sorted, the image VOLUME paths that neither
// data-dir nor the destination of a volumes.* entry covers (D3). Only the
// exact path counts: docker replaces a VOLUME's anonymous volume only with a
// mount at that same path, so a parent directory covers nothing.
func uncoveredVolumes(imageVolumes []string, spec config.AddonSpec) []string {
	covered := map[string]bool{}
	if spec.DataDir != "" {
		covered[path.Clean(spec.DataDir)] = true
	}
	for _, v := range spec.Volumes {
		// host:container[:opts] — the destination is the second field.
		if parts := strings.Split(v, ":"); len(parts) >= 2 {
			covered[path.Clean(parts[1])] = true
		}
	}
	var out []string
	for _, p := range imageVolumes {
		if !covered[path.Clean(p)] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// warnUncoveredVolumes warns, for each add-on this reconcile created or
// recreated, about image VOLUMEs outside data-dir and volumes.*: `rm -v`
// discards their content on the next recreate or removal (D2, D3). Inspect
// errors are silent, and the warning never fails the reconcile.
func warnUncoveredVolumes(cli *docker.Client, cfg config.Config, results []reconcile.Result, w io.Writer) {
	byContainer := make(map[string]string, len(cfg.Addons))
	for n := range cfg.Addons {
		byContainer[dockerargs.AddonContainerName(n)] = n
	}
	for _, r := range results {
		if r.Err != nil || (r.Action != reconcile.ActionCreated && r.Action != reconcile.ActionRecreated) {
			continue
		}
		name, ok := byContainer[r.Name]
		if !ok {
			continue // Home Assistant itself
		}
		a := cfg.Addons[name]
		vols, err := cli.ImageVolumes(a.Image)
		if err != nil {
			continue
		}
		if un := uncoveredVolumes(vols, a); len(un) > 0 {
			fmt.Fprintf(w, "warning: addons.%s: image %s declares VOLUME %s not covered by data-dir or volumes.*; their content is discarded when the add-on is recreated or removed\n",
				name, a.Image, strings.Join(un, ", "))
		}
	}
}
