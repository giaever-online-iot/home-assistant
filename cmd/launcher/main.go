// cmd/launcher/main.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/giaever-online-iot/home-assistant/internal/backup"
	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
	"github.com/giaever-online-iot/home-assistant/internal/reconcile"
)

// The docker snap's docker-executables content slot exports its whole root
// (read: [.]); the docker CLI is at bin/docker, so under our mount target
// $SNAP/docker-snap it lands at $SNAP/docker-snap/bin/docker. It is statically
// linked and talks to the daemon at /var/run/docker.sock (granted by the docker
// interface), so no extra library/socket env is needed.
func dockerBin() string { return filepath.Join(os.Getenv("SNAP"), "docker-snap", "bin", "docker") }

func loadConfig() (config.Config, error) {
	// snapctl needs explicit key(s): a bare `snapctl get -d` exits non-zero on a
	// fresh install with no config set (seen on Ubuntu Core), which aborts the
	// configure hook and rolls back the whole install. Query each config namespace
	// and tolerate "unset" so an unconfigured install resolves to all-defaults.
	merged := map[string]json.RawMessage{}
	for _, ns := range []string{"image", "docker"} {
		out, err := exec.Command("snapctl", "get", "-d", ns).Output()
		if err != nil {
			continue // namespace not set on this install → defaults apply
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(out, &doc); err != nil {
			return config.Config{}, fmt.Errorf("parsing snap config %q: %w", ns, err)
		}
		for k, v := range doc {
			merged[k] = v
		}
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return config.Config{}, fmt.Errorf("encoding snap config: %w", err)
	}
	return config.Parse(data)
}

func snapctlSet(key, value string) error {
	return exec.Command("snapctl", "set", key+"="+value).Run()
}

// dockerNotConnected prints an actionable message when the docker CLI is absent
// and exits non-zero. It is called before any subcommand that requires docker.
func dockerNotConnected() {
	fmt.Fprintln(os.Stderr, "Docker is not available to this snap. Install Docker and connect the interfaces:")
	fmt.Fprintln(os.Stderr, "  sudo snap install docker")
	fmt.Fprintln(os.Stderr, "  sudo snap connect home-assistant:docker docker:docker-daemon")
	fmt.Fprintln(os.Stderr, "  sudo snap connect home-assistant:docker-executables docker:docker-executables")
	os.Exit(1)
}

// requireDocker exits with an actionable message if the docker CLI binary is
// not reachable. Must be called before any subcommand that invokes docker.
func requireDocker() {
	if _, err := os.Stat(dockerBin()); err != nil {
		dockerNotConnected()
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: launcher <daemon|reconcile|update|backup|rollback|check-config|cli|validate>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cmd string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	// "validate" is docker-free: it only checks the snap config and exits.
	if cmd == "validate" {
		return runValidate(cfg)
	}
	// All other subcommands require the docker CLI to be connected.
	requireDocker()
	cli := docker.New(dockerBin())
	switch cmd {
	case "daemon":
		return runDaemon(cli, cfg)
	case "reconcile":
		return applyReconcile(cli, cfg, false)
	case "update":
		if _, err := snapshot(cli, cfg, "pre-update"); err != nil {
			return err
		}
		if err := cli.Pull(dockerargs.ImageRef(cfg)); err != nil {
			return err
		}
		return applyReconcile(cli, cfg, true)
	case "backup":
		name, err := snapshot(cli, cfg, "manual")
		if err == nil {
			fmt.Println("snapshot:", name)
		}
		return err
	case "rollback":
		return runRollback(cli, cfg)
	case "check-config":
		return cli.Exec(dockerargs.ContainerName, "python", "-m", "homeassistant", "--script", "check_config", "--config", "/config")
	case "cli":
		return cli.Exec(dockerargs.ContainerName, "/bin/bash")
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// runValidate loads the snap config, prints any warnings, and exits non-zero
// on a fatal validation error. It does NOT invoke docker.
func runValidate(cfg config.Config) error {
	warnings, err := cfg.Validate()
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		return err
	}
	fmt.Println("config ok")
	return nil
}

func applyReconcile(cli *docker.Client, cfg config.Config, force bool) error {
	warnings, err := cfg.Validate()
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		return err
	}
	action, err := reconcile.Reconcile(cli, cfg, force)
	if err != nil {
		return err
	}
	fmt.Println("reconcile:", action)
	return nil
}

// snapshot tars the current config and records the running image digest.
func snapshot(cli *docker.Client, cfg config.Config, prefix string) (string, error) {
	src := dockerargs.ConfigSource(cfg)
	image := dockerargs.ImageRef(cfg)
	digest, _ := cli.ImageDigest(dockerargs.ContainerName) // best-effort
	name := prefix + "-" + time.Now().Format("20060102-150405")
	if err := cli.Run(backup.SnapshotArgs(src, image, name, digest)); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	return name, nil
}

func runRollback(cli *docker.Client, cfg config.Config) error {
	image := dockerargs.ImageRef(cfg)
	lsout, err := cli.Capture(backup.ListArgs(image))
	if err != nil {
		return err
	}
	name := backup.ParseLatest(lsout)
	if name == "" {
		return fmt.Errorf("no snapshots found to roll back to")
	}
	meta, _ := cli.Capture(backup.ReadMetaArgs(image, name))
	digest := strings.TrimSpace(meta)

	if err := cli.Remove(dockerargs.ContainerName); err != nil {
		return err
	}
	if err := cli.Run(backup.RestoreArgs(dockerargs.ConfigSource(cfg), image, name)); err != nil {
		return err
	}
	if digest != "" {
		if err := snapctlSet("image.digest", digest); err != nil {
			return err
		}
		cfg.ImageDigest = digest
	} else {
		fmt.Fprintln(os.Stderr, "warning: snapshot has no recorded image digest; restoring config without pinning the image")
	}
	fmt.Println("rolled back to:", name)
	return applyReconcile(cli, cfg, true)
}

func runDaemon(cli *docker.Client, cfg config.Config) error {
	if err := applyReconcile(cli, cfg, false); err != nil {
		return err
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		_ = cli.Stop(dockerargs.ContainerName)
		os.Exit(0)
	}()
	return cli.FollowLogs(dockerargs.ContainerName)
}
