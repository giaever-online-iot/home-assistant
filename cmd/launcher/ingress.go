package main

import (
	"fmt"
	"io"
	"os"
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
		if err == nil && installed {
			warnIngressMode(cli, os.Stderr)
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
	if _, err := ensureInclude(cli, os.Stderr); err != nil {
		return err
	}
	fmt.Printf("Wrote %d ingress panel(s); restarting Home Assistant…\n", len(entries))
	return cli.Restart(dockerargs.ContainerName)
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
