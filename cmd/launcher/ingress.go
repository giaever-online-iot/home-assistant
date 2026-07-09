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

// runIngress renders the hass_ingress config from `ingress.*` snap config.
//   - `show`: print the rendered file (dry run, no side effects).
//   - `sync` (default): land it in /config, ensure configuration.yaml includes
//     it (without clobbering a user-managed `ingress:`), and restart HA.
func runIngress(cli *docker.Client, cfg config.Config, args []string) error {
	sub := "sync"
	if len(args) > 0 {
		sub = args[0]
	}
	body := ingress.Render(cfg.Ingress)

	switch sub {
	case "show":
		fmt.Print(body)
		return nil
	case "sync":
		if err := preflightContainer(cli, cfg); err != nil {
			return err
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
		fmt.Printf("Wrote %d ingress panel(s); restarting Home Assistant…\n", len(cfg.Ingress))
		return cli.Restart(dockerargs.ContainerName)
	default:
		return fmt.Errorf("unknown ingress subcommand %q (use: sync, show)", sub)
	}
}

// applyIngress is replaced with the real implementation in the next commit (Task 13).
func applyIngress(cli *docker.Client, cfg config.Config, force bool) error { return nil }
