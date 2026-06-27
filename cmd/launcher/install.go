package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
)

// Recipe describes how to install a payload (a custom integration) into Home
// Assistant's /config. Install runs inside the running container, writing to the
// config volume; Dest is the directory used as an idempotency probe.
type Recipe struct {
	Name       string
	Dest       string
	Activation string // printed after a successful install (manual UI steps)
	Install    func(*docker.Client) error
}

// installRegistry maps an install target to its recipe. Extend this to add more
// installable integrations (or, later, a generic owner/repo target).
var installRegistry = map[string]Recipe{
	"hacs": {
		Name: "hacs",
		Dest: "/config/custom_components/hacs",
		Install: func(c *docker.Client) error {
			// HACS's documented container install: fetch + extract into
			// /config/custom_components/hacs from inside the running container.
			return c.Exec(dockerargs.ContainerName, "bash", "-c", "wget -O - https://get.hacs.xyz | bash")
		},
		Activation: "\nHACS files installed. To finish (one-time, in the UI):\n" +
			"  1. Home Assistant → Settings → Devices & services → Add integration → HACS.\n" +
			"  2. Acknowledge the prompts, then complete the GitHub device authorization.\n" +
			"HACS updates itself from the UI thereafter.\n",
	},
}

func resolveTarget(name string) (Recipe, bool) {
	r, ok := installRegistry[name]
	return r, ok
}

// installTargets returns the registered target names, sorted (for messages).
func installTargets() []string {
	names := make([]string, 0, len(installRegistry))
	for n := range installRegistry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// parseInstallArgs parses `install <target> [--no-restart] [--force]` (flags in
// any order). The single non-flag argument is the target.
func parseInstallArgs(args []string) (target string, noRestart, force bool, err error) {
	for _, a := range args {
		switch a {
		case "--no-restart":
			noRestart = true
		case "--force":
			force = true
		default:
			if strings.HasPrefix(a, "-") {
				return "", false, false, fmt.Errorf("unknown flag %q", a)
			}
			if target != "" {
				return "", false, false, fmt.Errorf("unexpected extra argument %q", a)
			}
			target = a
		}
	}
	if target == "" {
		return "", false, false, fmt.Errorf("usage: install <target> [--no-restart] [--force]; targets: %s", strings.Join(installTargets(), ", "))
	}
	return target, noRestart, force, nil
}

// runInstall installs a registered target into HA's /config and (unless
// --no-restart) restarts the container so HA loads it. The container must be
// running — the caller runs preflightContainer first.
func runInstall(cli *docker.Client, args []string) error {
	target, noRestart, force, err := parseInstallArgs(args)
	if err != nil {
		return err
	}
	recipe, ok := resolveTarget(target)
	if !ok {
		return fmt.Errorf("unknown install target %q; targets: %s", target, strings.Join(installTargets(), ", "))
	}
	if !force {
		installed, err := cli.ExecCheck(dockerargs.ContainerName, "test", "-d", recipe.Dest)
		if err != nil {
			return err
		}
		if installed {
			fmt.Printf("%s is already installed (it self-updates from the Home Assistant UI). Use --force to reinstall.\n", recipe.Name)
			return nil
		}
	}
	if err := recipe.Install(cli); err != nil {
		return fmt.Errorf("installing %s: %w", recipe.Name, err)
	}
	if !noRestart {
		fmt.Printf("Restarting Home Assistant to load %s …\n", recipe.Name)
		if err := cli.Restart(dockerargs.ContainerName); err != nil {
			return fmt.Errorf("restarting after install: %w", err)
		}
	}
	fmt.Print(recipe.Activation)
	return nil
}
