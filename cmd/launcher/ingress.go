package main

import (
	"fmt"
	"os"

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
//   - `sync` (default): force-apply — land it in /config, ensure the include,
//     restart HA.
func runIngress(cli *docker.Client, cfg config.Config, args []string) error {
	sub := "sync"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "show":
		fmt.Print(ingress.Render(ingress.MergedEntries(cfg)))
		return nil
	case "sync":
		return applyIngress(cli, cfg, true)
	default:
		return fmt.Errorf("unknown ingress subcommand %q (use: sync, show)", sub)
	}
}

// applyIngress lands the merged panel map in /config. force=true (manual
// `ingress sync`) always writes and restarts; force=false (auto-sync from
// reconcile) is change-driven — identical content means no write and no HA
// restart, so a boot-time reconcile is a true no-op. Callers treat auto-sync
// errors as warnings: panels are cosmetic, containers are the substance.
func applyIngress(cli *docker.Client, cfg config.Config, force bool) error {
	entries := ingress.MergedEntries(cfg)
	body := ingress.Render(entries)
	if err := preflightContainer(cli, cfg); err != nil {
		return err
	}
	if len(entries) > 0 {
		installed, err := cli.ExecCheck(dockerargs.ContainerName, "test", "-d", "/config/custom_components/ingress")
		if err == nil && !installed {
			fmt.Fprintln(os.Stderr, "warning: ingress panels are configured but hass_ingress is not installed — run `home-assistant.install hass-ingress`")
		}
	}
	if !force {
		current, _ := cli.ReadFile(dockerargs.ContainerName, ingressFilePath)
		if !ingress.ShouldSync(current, body, len(entries)) {
			return nil
		}
	}
	if err := cli.WriteFile(dockerargs.ContainerName, ingressFilePath, body); err != nil {
		return fmt.Errorf("writing %s: %w", ingressFilePath, err)
	}
	current, err := cli.ReadFile(dockerargs.ContainerName, configYAMLPath)
	if err != nil {
		return err
	}
	out, changed, conflict := ingress.EnsureInclude(current)
	switch {
	case conflict:
		fmt.Fprintf(os.Stderr,
			"warning: %s already defines its own `ingress:` key — leaving it untouched.\n"+
				"  Remove it (or fold in `!include %s`) for snap-managed ingress to apply.\n",
			configYAMLPath, ingress.IncludeFile)
	case changed:
		if err := cli.WriteFile(dockerargs.ContainerName, configYAMLPath, out); err != nil {
			return fmt.Errorf("updating %s: %w", configYAMLPath, err)
		}
		fmt.Printf("Added `ingress: !include %s` to configuration.yaml.\n", ingress.IncludeFile)
	}
	fmt.Printf("Wrote %d ingress panel(s); restarting Home Assistant…\n", len(entries))
	return cli.Restart(dockerargs.ContainerName)
}
