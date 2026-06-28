package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/config"
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
	"hass-ingress": {
		Name: "hass-ingress",
		// The integration's dir (and HA domain) is `ingress`, not `hass_ingress`.
		Dest: "/config/custom_components/ingress",
		Install: func(c *docker.Client) error {
			// Fetch lovelylain/hass_ingress and copy its custom_components/ingress
			// into /config from inside the running container. --strip-components=1
			// avoids hard-coding the branch-named top dir; the test -d gives a clear
			// error if the repo layout ever changes (instead of a cryptic cp stat).
			return c.Exec(dockerargs.ContainerName, "bash", "-c",
				"set -e; cd /tmp; "+
					"wget -qO hi.tgz https://github.com/lovelylain/hass_ingress/archive/refs/heads/main.tar.gz; "+
					"rm -rf hi-src; mkdir hi-src; "+
					"tar xzf hi.tgz -C hi-src --strip-components=1; "+
					"test -d hi-src/custom_components/ingress || { echo 'hass_ingress: custom_components/ingress not found (repo layout changed)'; exit 1; }; "+
					"mkdir -p /config/custom_components; "+
					"rm -rf /config/custom_components/ingress; "+
					"cp -r hi-src/custom_components/ingress /config/custom_components/; "+
					"rm -rf hi.tgz hi-src")
		},
		Activation: "\nhass_ingress installed. Next:\n" +
			"  1. Define panels:  snap set home-assistant ingress.<name>.url=http://localhost:<port> ingress.<name>.title=\"<Title>\"\n" +
			"  2. Apply:          home-assistant.ingress sync\n" +
			"Each panel then appears in the Home Assistant sidebar.\n",
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
// --no-restart) restarts the container so HA loads it. Argument/target
// validation happens BEFORE the docker preflight so a usage error fails fast —
// the preflight's docker calls can be slow.
func runInstall(cli *docker.Client, cfg config.Config, args []string) error {
	target, noRestart, force, err := parseInstallArgs(args)
	if err != nil {
		return err
	}
	recipe, ok := resolveTarget(target)
	if !ok {
		return fmt.Errorf("unknown install target %q; targets: %s", target, strings.Join(installTargets(), ", "))
	}
	// Cheap validation done — only now pay for the (possibly slow) container preflight.
	if err := preflightContainer(cli, cfg); err != nil {
		return err
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
