package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
	"github.com/giaever-online-iot/home-assistant/internal/ingress"
)

const (
	ingressFilePath = "/config/" + ingress.IncludeFile // /config/snap-ingress.yaml
	configYAMLPath  = "/config/configuration.yaml"
)

// runIngress renders the merged panel map (explicit ingress.* + add-on
// panels) from snap config.
//   - `show`: print the rendered file (dry run, no side effects).
//   - `sync` (default): compare-first, like reconcile's auto-sync (D7): land
//     the file and the include line, and restart HA only when the panels
//     changed. `sync --force` always writes and restarts (the repair hatch).
func runIngress(cli *docker.Client, cfg config.Config, args []string, stdout, stderr io.Writer) error {
	sub := "sync"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "show":
		fmt.Fprint(stdout, ingress.Render(ingress.MergedEntries(cfg)))
		return nil
	case "sync":
		force := false
		var opts []string
		if len(args) > 1 {
			opts = args[1:]
		}
		for _, a := range opts {
			if a != "--force" {
				return fmt.Errorf("unknown ingress sync option %q (use: --force)", a)
			}
			force = true
		}
		restarted, err := applyIngress(cli, cfg, force, stderr)
		if err == nil && !restarted {
			fmt.Fprintln(stdout, "ingress: panels unchanged; Home Assistant not restarted (use --force to restart anyway)")
		}
		return err
	default:
		return fmt.Errorf("unknown ingress subcommand %q (use: sync, show)", sub)
	}
}

// applyIngress lands the merged panel map in /config and restarts HA when
// that is needed for it to take effect; restarted says whether it did. It
// checks the hass_ingress install and mode first (W-12 D11, D12). force
// (`ingress sync --force`) writes and restarts unconditionally; otherwise it
// is change-driven (D6–D8), so a boot-time reconcile is a true no-op. Its
// messages go to w. Callers treat auto-sync errors as warnings: panels are
// cosmetic, containers are the substance.
func applyIngress(cli *docker.Client, cfg config.Config, force bool, w io.Writer) (restarted bool, err error) {
	entries := ingress.MergedEntries(cfg)
	if err := preflightContainer(cli, cfg); err != nil {
		return false, err
	}
	if len(entries) > 0 {
		installed, err := cli.ExecCheck(dockerargs.ContainerName, "test", "-d", "/config/custom_components/ingress")
		if err == nil && !installed {
			fmt.Fprintln(w, "warning: ingress panels are configured but hass_ingress is not installed — run `home-assistant.install hass-ingress`")
		}
		if err == nil && installed {
			warnIngressMode(cli, w)
		}
	}
	restart, err := syncIngressFiles(cli, cfg, force, w)
	if err != nil {
		return false, err
	}
	if !restart {
		return false, nil
	}
	fmt.Fprintf(w, "Wrote %d ingress panel(s); restarting Home Assistant…\n", len(entries))
	return true, cli.Restart(dockerargs.ContainerName)
}

// syncIngressFiles brings snap-ingress.yaml and configuration.yaml's include
// line up to date and reports whether HA must restart to pick them up. It
// never restarts HA itself, so it can also run in the old container just
// before a recreate (D4). force writes the file and asks for a restart even
// when nothing changed. The include line is checked whenever there is at
// least one panel, even when the file is unchanged (D8). Warnings go to w.
func syncIngressFiles(cli *docker.Client, cfg config.Config, force bool, w io.Writer) (restart bool, err error) {
	entries := ingress.MergedEntries(cfg)
	body := ingress.Render(entries)
	write := true
	restart = true
	if !force {
		current, _ := cli.ReadFile(dockerargs.ContainerName, ingressFilePath)
		write, restart = ingress.Decide(current, body, len(entries))
	}
	if write {
		if err := cli.WriteFile(dockerargs.ContainerName, ingressFilePath, body); err != nil {
			return false, fmt.Errorf("writing %s: %w", ingressFilePath, err)
		}
	}
	if len(entries) > 0 {
		added, err := ensureInclude(cli, w)
		if err != nil {
			return false, err
		}
		restart = restart || added
	}
	return restart, nil
}

// ensureInclude adds `ingress: !include snap-ingress.yaml` to
// configuration.yaml and reports whether it wrote the file. It never creates
// the file and never writes from a failed read (D8a): absent or unreadable
// gives a warning and no write. HA writes its default configuration.yaml at
// first boot only when the file is missing, so a snap-made one-liner would
// suppress those defaults. Only a failed write is an error.
func ensureInclude(cli *docker.Client, w io.Writer) (changed bool, err error) {
	current, present, err := cli.ReadFileIfExists(dockerargs.ContainerName, configYAMLPath)
	if err != nil {
		fmt.Fprintf(w, "warning: could not read %s (%v); the ingress include was not checked\n", configYAMLPath, err)
		return false, nil
	}
	if !present {
		fmt.Fprintln(w, "warning: configuration.yaml not found yet; the ingress include is added on the next reconcile")
		return false, nil
	}
	if strings.TrimSpace(current) == "" {
		// A blank read of a present file is also what a read racing HA's
		// first-boot write, or a truncated file, looks like (T13a).
		fmt.Fprintln(w, "warning: configuration.yaml is empty; the ingress include is added on the next reconcile once it has content")
		return false, nil
	}
	out, changed, conflict := ingress.EnsureInclude(current)
	switch {
	case conflict:
		fmt.Fprintf(w,
			"warning: %s already defines its own `ingress:` key — leaving it untouched.\n"+
				"  Remove it (or fold in `!include %s`) for snap-managed ingress to apply.\n",
			configYAMLPath, ingress.IncludeFile)
		return false, nil
	case changed:
		if err := cli.WriteFile(dockerargs.ContainerName, configYAMLPath, out); err != nil {
			return false, fmt.Errorf("updating %s: %w", configYAMLPath, err)
		}
		fmt.Fprintf(w, "Added `ingress: !include %s` to configuration.yaml.\n", ingress.IncludeFile)
		return true, nil
	}
	return false, nil
}
