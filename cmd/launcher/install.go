package main

import (
	"fmt"
	"io"
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
	Installed  string // printed when Dest already exists (no --force)
	Install    func(*docker.Client) error
	// Gate, if set, runs after preflight and before the dir probe. proceed=false
	// prints notice and exits 0 with nothing installed, whatever --force says.
	Gate func(c *docker.Client) (proceed bool, notice string, err error)
}

// installRegistry maps an install target to its recipe. Extend this to add more
// installable integrations (or, later, a generic owner/repo target).
var installRegistry = map[string]Recipe{
	"hacs": {
		Name: "hacs",
		Dest: "/config/custom_components/hacs",
		Gate: hacsGate,
		Install: func(c *docker.Client) error {
			// HACS's documented container install: fetch + extract into
			// /config/custom_components/hacs from inside the running container.
			return c.Exec(dockerargs.ContainerName, "bash", "-c", "wget -O - https://get.hacs.xyz | bash")
		},
		Activation: "\nHACS files installed. To finish (one-time, in the UI):\n" +
			"  1. Home Assistant → Settings → Devices & services → Add integration → HACS.\n" +
			"  2. Acknowledge the prompts, then complete the GitHub device authorization.\n" +
			"HACS gets no further releases; upgrading Home Assistant to 2026.11 or later moves it to the built-in Marketplace.\n",
		Installed: "hacs is already installed. It updates from its own page in the Home Assistant UI, but gets no new releases. Use --force to reinstall.",
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
		Installed: `hass-ingress is already installed. This copy does not update itself: run "home-assistant.install hass-ingress --force" to refresh it from upstream.`,
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
func runInstall(cli *docker.Client, cfg config.Config, args []string, out io.Writer) error {
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
	if recipe.Gate != nil {
		proceed, notice, err := recipe.Gate(cli)
		if err != nil {
			return err
		}
		if notice != "" {
			fmt.Fprintln(out, notice)
		}
		if !proceed {
			return nil
		}
	}
	if !force {
		installed, err := cli.ExecCheck(dockerargs.ContainerName, "test", "-d", recipe.Dest)
		if err != nil {
			return err
		}
		if installed {
			fmt.Fprintln(out, recipe.Installed)
			return nil
		}
	}
	if err := recipe.Install(cli); err != nil {
		return fmt.Errorf("installing %s: %w", recipe.Name, err)
	}
	if !noRestart {
		fmt.Fprintf(out, "Restarting Home Assistant to load %s …\n", recipe.Name)
		if err := cli.Restart(dockerargs.ContainerName); err != nil {
			return fmt.Errorf("restarting after install: %w", err)
		}
	}
	fmt.Fprint(out, recipe.Activation)
	return nil
}
