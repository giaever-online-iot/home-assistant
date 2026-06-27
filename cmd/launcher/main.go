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

	"github.com/charmbracelet/lipgloss"

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

// dockerSteps returns the ordered remediation commands for whichever docker
// prerequisites are missing, or nil when docker is fully available. It is pure
// (no I/O) so it can be unit-tested. execConn/sockConn report whether the
// docker-executables/docker interfaces are connected; binary reports whether the
// docker CLI is present in the content mount (which implies docker-executables is
// connected to an installed provider).
func dockerSteps(execConn, sockConn, binary bool) []string {
	if execConn && sockConn && binary {
		return nil
	}
	var steps []string
	// docker-executables provides the CLI; if it is unconnected AND the binary is
	// absent, the docker snap (the content provider) is likely not installed.
	if !execConn && !binary {
		steps = append(steps, "sudo snap install docker")
	}
	if !execConn {
		steps = append(steps, "sudo snap connect home-assistant:docker-executables docker:docker-executables")
	}
	if !sockConn {
		steps = append(steps, "sudo snap connect home-assistant:docker docker:docker-daemon")
	}
	// Connected but the CLI is still missing → the provider snap looks broken.
	if execConn && !binary {
		steps = append(steps, "sudo snap install docker")
	}
	return steps
}

// snapctlIsConnected reports whether the named plug is connected, via
// `snapctl is-connected <plug>` (exit 0 == connected). Works inside the snap.
func snapctlIsConnected(plug string) bool {
	return exec.Command("snapctl", "is-connected", plug).Run() == nil
}

func statOK(path string) bool { _, err := os.Stat(path); return err == nil }

// stderrIsTTY reports whether stderr is a terminal, so guidance is styled for a
// human at a prompt but stays plain text in the systemd journal.
func stderrIsTTY() bool {
	fi, _ := os.Stderr.Stat()
	return fi != nil && fi.Mode()&os.ModeCharDevice != 0
}

// renderGuidance formats a title plus ordered command steps: a lipgloss-styled
// box when stderr is a terminal, plain indented text otherwise.
func renderGuidance(title string, steps []string) string {
	if !stderrIsTTY() {
		var b strings.Builder
		fmt.Fprintln(&b, title)
		for _, s := range steps {
			fmt.Fprintln(&b, "  "+s)
		}
		return b.String()
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	cmdStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("11")).
		Padding(0, 1)
	lines := []string{titleStyle.Render(title), ""}
	for _, s := range steps {
		lines = append(lines, cmdStyle.Render(s))
	}
	return box.Render(strings.Join(lines, "\n")) + "\n"
}

// requireDocker exits with targeted, dynamic guidance when docker is not fully
// available to the snap — printing only the steps that are actually missing, and
// nothing when docker is ready. Must be called before any subcommand that
// invokes docker.
func requireDocker() {
	steps := dockerSteps(
		snapctlIsConnected("docker-executables"),
		snapctlIsConnected("docker"),
		statOK(dockerBin()),
	)
	if steps == nil {
		return
	}
	fmt.Fprint(os.Stderr, renderGuidance("Docker isn't ready for Home Assistant yet. Run:", steps))
	os.Exit(1)
}

// preflightContainer returns a clear error explaining which rung is missing
// (docker ready → image present → container running) before a command execs into
// the running container, instead of docker's raw "No such container: …".
func preflightContainer(cli *docker.Client, cfg config.Config) error {
	running, err := cli.Running(dockerargs.ContainerName)
	if err != nil {
		return err
	}
	if running {
		return nil
	}
	exists, err := cli.Exists(dockerargs.ContainerName)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("Home Assistant is not running yet — it may still be starting; the daemon will bring it up. Try again shortly, or run `home-assistant.reconcile`")
	}
	present, err := cli.ImageExists(dockerargs.ImageRef(cfg))
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("Home Assistant's image is still downloading — run `home-assistant.reconcile` to watch the progress, or wait for the daemon to finish")
	}
	return fmt.Errorf("Home Assistant's container hasn't been created yet — run `home-assistant.reconcile` (or wait for the daemon) to start it")
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
		// applyReconcile(force=true) pulls (streamed progress + bounded retry)
		// before recreating the container, so no separate pull is needed here.
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
		if err := preflightContainer(cli, cfg); err != nil {
			return err
		}
		return cli.Exec(dockerargs.ContainerName, "python", "-m", "homeassistant", "--script", "check_config", "--config", "/config")
	case "cli":
		if err := preflightContainer(cli, cfg); err != nil {
			return err
		}
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
