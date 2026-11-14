# Home Assistant Docker Snap — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a strictly-confined Go "launcher" snap named `home-assistant` that drives the official `docker` snap to run, configure, update, and roll back the upstream Home Assistant container image.

**Architecture:** The snap contains no Home Assistant code — it is a small Go daemon plus helper commands that issue `docker` commands (via the `docker` socket plug and the `docker-executables` content interface) to reconcile a `homeassistant` container to the snap's configuration. Snap configuration (`snap set home-assistant …`) is the declarative desired state; a `configure` hook restarts the daemon, which recreates the container only when a stored spec-hash label changes. Upgrades snapshot the config and pin the image digest so they can be rolled back.

**Tech Stack:** Go (stdlib only), snapcraft `core26`, the `docker` + content interfaces, `snapctl` for config.

## Global Constraints

- Snap name: `home-assistant`; base `core26`; `confinement: strict`; `grade: stable`.
- Platforms: `amd64`, `arm64`.
- Go module path: `github.com/giaever-online-iot/home-assistant`; **stdlib only** (no third-party deps).
- Container name (fixed): `homeassistant`. Config named volume: `homeassistant-config`. Backups volume: `homeassistant-backups`.
- Image ref: `image.digest` when set → `<registry>@<digest>`; else `<registry>:<channel>`. Default registry `ghcr.io/home-assistant/home-assistant`, default channel `stable`.
- Managed (always-set) run args: `--name homeassistant`, `--restart=unless-stopped`, `-v <config-source>:/config`, `--label io.giaever.home-assistant.spec-hash=<hash>`.
- Overridable defaults: `docker.network=host`, `docker.privileged=true`, `docker.bluetooth=true`. Bluetooth adds `-v /run/dbus:/run/dbus:ro`.
- **`config-dir` default = a docker named volume** (`docker.config-dir=""`); an absolute path switches to a bind-mount. `$SNAP_DATA`/`$SNAP_COMMON` and content interfaces are *not* usable (the separate docker snap cannot reach our data).
- Plugs: `docker` (interface `docker`); `docker-executables` (content, `target: $SNAP/docker-snap`, `default-provider: docker`). Docker CLI invoked at `$SNAP/docker-snap/docker`.
- Out of scope for v1: add-ons/Supervisor, companion containers (`companions.*` is a v2 namespace).

---

## File Structure

```
home-assistant/                       (repo root = this worktree)
├── go.mod
├── cmd/launcher/main.go              entrypoint: daemon|reconcile|update|backup|rollback|check-config|cli
├── internal/
│   ├── config/config.go             Config struct, Parse (+ defaults), Validate
│   ├── config/config_test.go
│   ├── dockerargs/dockerargs.go     ConfigSource, ImageRef, BuildRunArgs (pure), SpecHash, constants
│   ├── dockerargs/dockerargs_test.go
│   ├── docker/docker.go             Client wrapping the docker CLI via a Runner
│   ├── docker/docker_test.go
│   ├── reconcile/reconcile.go       Reconcile(desired vs actual)
│   ├── reconcile/reconcile_test.go
│   ├── backup/backup.go             pure arg builders for snapshot/restore/list + ParseLatest
│   └── backup/backup_test.go
└── snap/
    ├── snapcraft.yaml
    └── hooks/configure              restarts the daemon to re-reconcile
```

---

## Task 1: Config parsing with defaults

**Files:** Create `go.mod`, `internal/config/config.go`; Test `internal/config/config_test.go`.

**Interfaces:**
- Produces: `config.Config{ImageRegistry, ImageChannel, ImageDigest, Network string; Privileged, Bluetooth bool; Timezone, ConfigDir string; Devices, Volumes, Environment map[string]string; ExtraArgs string}`; `config.Parse(data []byte) (Config, error)`.

- [ ] **Step 1: Initialize the module**

Run: `go mod init github.com/giaever-online-iot/home-assistant`
Expected: creates `go.mod`.

- [ ] **Step 2: Write the failing test**

```go
// internal/config/config_test.go
package config

import "testing"

func TestParseEmptyAppliesDefaults(t *testing.T) {
	c, err := Parse([]byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ImageRegistry != "ghcr.io/home-assistant/home-assistant" || c.ImageChannel != "stable" {
		t.Errorf("image defaults wrong: %q %q", c.ImageRegistry, c.ImageChannel)
	}
	if c.ImageDigest != "" {
		t.Errorf("digest should default empty, got %q", c.ImageDigest)
	}
	if c.Network != "host" || !c.Privileged || !c.Bluetooth {
		t.Errorf("defaults wrong: net=%q priv=%v bt=%v", c.Network, c.Privileged, c.Bluetooth)
	}
}

func TestParseOverrides(t *testing.T) {
	in := `{"image":{"channel":"beta","digest":"sha256:abc"},"docker":{"privileged":false,"timezone":"Europe/Oslo","devices":{"zigbee":"/dev/ttyUSB0"},"extra-args":"--shm-size=256m","config-dir":"/home/joachim/home-assistant"}}`
	c, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ImageChannel != "beta" || c.ImageDigest != "sha256:abc" {
		t.Errorf("image override wrong: %q %q", c.ImageChannel, c.ImageDigest)
	}
	if c.Privileged {
		t.Errorf("privileged should be false (explicit override)")
	}
	if c.Timezone != "Europe/Oslo" || c.Devices["zigbee"] != "/dev/ttyUSB0" || c.ExtraArgs != "--shm-size=256m" || c.ConfigDir != "/home/joachim/home-assistant" {
		t.Errorf("overrides wrong: %+v", c)
	}
}
```

- [ ] **Step 3: Run the test, verify it fails**

Run: `go test ./internal/config/`
Expected: FAIL — `undefined: Parse`.

- [ ] **Step 4: Implement `config.go`**

```go
// internal/config/config.go
package config

import (
	"encoding/json"
	"fmt"
)

// Config is the resolved launcher configuration with defaults applied.
type Config struct {
	ImageRegistry string
	ImageChannel  string
	ImageDigest   string // when set, overrides channel: registry@digest
	Network       string
	Privileged    bool
	Bluetooth     bool
	Timezone      string
	ConfigDir     string // "" => docker named volume; absolute path => bind-mount
	Devices       map[string]string
	Volumes       map[string]string
	Environment   map[string]string
	ExtraArgs     string
}

type rawConfig struct {
	Image struct {
		Registry string `json:"registry"`
		Channel  string `json:"channel"`
		Digest   string `json:"digest"`
	} `json:"image"`
	Docker struct {
		Network     string            `json:"network"`
		Privileged  *bool             `json:"privileged"`
		Bluetooth   *bool             `json:"bluetooth"`
		Timezone    string            `json:"timezone"`
		ConfigDir   string            `json:"config-dir"`
		Devices     map[string]string `json:"devices"`
		Volumes     map[string]string `json:"volumes"`
		Environment map[string]string `json:"environment"`
		ExtraArgs   string            `json:"extra-args"`
	} `json:"docker"`
}

const (
	defaultRegistry = "ghcr.io/home-assistant/home-assistant"
	defaultChannel  = "stable"
	defaultNetwork  = "host"
)

// Parse unmarshals `snapctl get -d` JSON and applies defaults.
func Parse(data []byte) (Config, error) {
	var raw rawConfig
	if len(data) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return Config{}, fmt.Errorf("parsing snap config: %w", err)
		}
	}
	return Config{
		ImageRegistry: orDefault(raw.Image.Registry, defaultRegistry),
		ImageChannel:  orDefault(raw.Image.Channel, defaultChannel),
		ImageDigest:   raw.Image.Digest,
		Network:       orDefault(raw.Docker.Network, defaultNetwork),
		Privileged:    boolOrDefault(raw.Docker.Privileged, true),
		Bluetooth:     boolOrDefault(raw.Docker.Bluetooth, true),
		Timezone:      raw.Docker.Timezone,
		ConfigDir:     raw.Docker.ConfigDir,
		Devices:       raw.Docker.Devices,
		Volumes:       raw.Docker.Volumes,
		Environment:   raw.Docker.Environment,
		ExtraArgs:     raw.Docker.ExtraArgs,
	}, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func boolOrDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}
```

- [ ] **Step 5: Run the test, verify it passes**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod internal/config/
git commit -m "feat(config): parse snap config with defaults"
```

---

## Task 2: Config validation

**Files:** Modify `internal/config/config.go` (add `Validate`); Test `internal/config/config_test.go`.

**Interfaces:** Consumes `config.Config`; Produces `(Config).Validate() (warnings []string, err error)`.

- [ ] **Step 1: Write the failing test**

```go
// append to internal/config/config_test.go
func TestValidateWarnsOnNonHostNetwork(t *testing.T) {
	c, _ := Parse([]byte(`{"docker":{"network":"bridge"}}`))
	warnings, err := c.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for non-host network")
	}
}

func TestValidateRejectsBadInputs(t *testing.T) {
	bad := []string{
		`{"docker":{"devices":{"z":"ttyUSB0"}}}`,
		`{"docker":{"volumes":{"m":"/mnt/media"}}}`,
		`{"docker":{"config-dir":"relative/path"}}`,
	}
	for _, in := range bad {
		c, _ := Parse([]byte(in))
		if _, err := c.Validate(); err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/config/`
Expected: FAIL — `c.Validate undefined`.

- [ ] **Step 3: Implement `Validate`**

Add `"strings"` to the import block in `config.go` (so it reads `"encoding/json"`, `"fmt"`, `"strings"`), then append:

```go
// Validate returns non-fatal warnings and a fatal error if the config is unusable.
func (c Config) Validate() (warnings []string, err error) {
	if c.Network != "host" {
		warnings = append(warnings, fmt.Sprintf(
			"docker.network=%q: device discovery and Bluetooth need host networking; prefer docker.network=host", c.Network))
	}
	if !c.Privileged {
		warnings = append(warnings, "docker.privileged=false: USB/Bluetooth passthrough may not work without privileged mode")
	}
	for name, dev := range c.Devices {
		if !strings.HasPrefix(dev, "/dev/") {
			return warnings, fmt.Errorf("docker.devices.%s=%q: device path must start with /dev/", name, dev)
		}
	}
	for name, vol := range c.Volumes {
		if !strings.Contains(vol, ":") {
			return warnings, fmt.Errorf("docker.volumes.%s=%q: volume must be host:container", name, vol)
		}
	}
	if c.ConfigDir != "" && !strings.HasPrefix(c.ConfigDir, "/") {
		return warnings, fmt.Errorf("docker.config-dir=%q: must be an absolute path", c.ConfigDir)
	}
	return warnings, nil
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): validate network, devices, volumes, config-dir"
```

---

## Task 3: Pure `docker run` argument builder

**Files:** Create `internal/dockerargs/dockerargs.go`, `internal/dockerargs/dockerargs_test.go`.

**Interfaces:**
- Produces: consts `ContainerName="homeassistant"`, `ConfigVolumeName="homeassistant-config"`, `SpecHashLabel="io.giaever.home-assistant.spec-hash"`; `ConfigSource(config.Config) string`; `ImageRef(config.Config) string`; `BuildRunArgs(config.Config) []string` (leading `"run"`); `SpecHash(config.Config) string`.

- [ ] **Step 1: Write the failing test**

```go
// internal/dockerargs/dockerargs_test.go
package dockerargs

import (
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

func cfg(json string) config.Config { c, _ := config.Parse([]byte(json)); return c }
func join(a []string) string        { return strings.Join(a, " ") }

func TestConfigSource(t *testing.T) {
	if got := ConfigSource(cfg(`{}`)); got != "homeassistant-config" {
		t.Errorf("default source = %q, want named volume", got)
	}
	if got := ConfigSource(cfg(`{"docker":{"config-dir":"/srv/ha"}}`)); got != "/srv/ha" {
		t.Errorf("bind source = %q", got)
	}
}

func TestImageRef(t *testing.T) {
	if got := ImageRef(cfg(`{}`)); got != "ghcr.io/home-assistant/home-assistant:stable" {
		t.Errorf("ref = %q", got)
	}
	if got := ImageRef(cfg(`{"image":{"digest":"sha256:abc"}}`)); got != "ghcr.io/home-assistant/home-assistant@sha256:abc" {
		t.Errorf("digest ref = %q", got)
	}
}

func TestBuildRunArgsDefaults(t *testing.T) {
	got := join(BuildRunArgs(cfg(`{}`)))
	for _, want := range []string{
		"run -d", "--name homeassistant", "--restart=unless-stopped",
		"--network=host", "--privileged",
		"-v homeassistant-config:/config", "-v /run/dbus:/run/dbus:ro",
		"ghcr.io/home-assistant/home-assistant:stable",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n got: %s", want, got)
		}
	}
}

func TestBuildRunArgsBindAndNoBluetoothAndExtras(t *testing.T) {
	got := join(BuildRunArgs(cfg(`{"docker":{"bluetooth":false,"config-dir":"/srv/ha","timezone":"Europe/Oslo","devices":{"z":"/dev/ttyUSB0"},"environment":{"FOO":"bar"},"extra-args":"--cap-add=NET_ADMIN"}}`)))
	for _, want := range []string{"-v /srv/ha:/config", "-e TZ=Europe/Oslo", "--device=/dev/ttyUSB0", "-e FOO=bar", "--cap-add=NET_ADMIN"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n got: %s", want, got)
		}
	}
	if strings.Contains(got, "/run/dbus") {
		t.Errorf("dbus should be absent when bluetooth=false: %s", got)
	}
}

func TestImageIsLastArg(t *testing.T) {
	a := BuildRunArgs(cfg(`{}`))
	if a[len(a)-1] != "ghcr.io/home-assistant/home-assistant:stable" {
		t.Errorf("last arg = %q", a[len(a)-1])
	}
}

func TestSpecHashChanges(t *testing.T) {
	if SpecHash(cfg(`{}`)) == SpecHash(cfg(`{"image":{"channel":"beta"}}`)) {
		t.Error("spec hash should change with channel")
	}
	if SpecHash(cfg(`{}`)) == SpecHash(cfg(`{"image":{"digest":"sha256:x"}}`)) {
		t.Error("spec hash should change with digest")
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/dockerargs/`
Expected: FAIL — `undefined: BuildRunArgs`.

- [ ] **Step 3: Implement `dockerargs.go`**

```go
// internal/dockerargs/dockerargs.go
package dockerargs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

const (
	ContainerName    = "homeassistant"
	ConfigVolumeName = "homeassistant-config"
	SpecHashLabel    = "io.giaever.home-assistant.spec-hash"
)

// ConfigSource returns the docker volume source for /config: a named volume by
// default, or the configured absolute bind path.
func ConfigSource(c config.Config) string {
	if c.ConfigDir == "" {
		return ConfigVolumeName
	}
	return c.ConfigDir
}

// ImageRef returns the pinnable image reference (registry@digest if a digest is
// set, otherwise registry:channel).
func ImageRef(c config.Config) string {
	if c.ImageDigest != "" {
		return c.ImageRegistry + "@" + c.ImageDigest
	}
	return c.ImageRegistry + ":" + c.ImageChannel
}

// BuildRunArgs returns the full `docker run` argument list (including "run").
func BuildRunArgs(c config.Config) []string {
	args := []string{
		"run", "-d",
		"--name", ContainerName,
		"--restart=unless-stopped",
		"--label", SpecHashLabel + "=" + SpecHash(c),
		"--network=" + c.Network,
	}
	if c.Privileged {
		args = append(args, "--privileged")
	}
	args = append(args, "-v", ConfigSource(c)+":/config")
	if c.Bluetooth {
		args = append(args, "-v", "/run/dbus:/run/dbus:ro")
	}
	if c.Timezone != "" {
		args = append(args, "-e", "TZ="+c.Timezone)
	}
	for _, k := range sortedKeys(c.Devices) {
		args = append(args, "--device="+c.Devices[k])
	}
	for _, k := range sortedKeys(c.Volumes) {
		args = append(args, "-v", c.Volumes[k])
	}
	for _, k := range sortedKeys(c.Environment) {
		args = append(args, "-e", k+"="+c.Environment[k])
	}
	if strings.TrimSpace(c.ExtraArgs) != "" {
		args = append(args, strings.Fields(c.ExtraArgs)...)
	}
	return append(args, ImageRef(c))
}

// SpecHash is a deterministic hash of all spec-affecting config.
func SpecHash(c config.Config) string {
	h := sha256.New()
	fmt.Fprintf(h, "ref=%s\n", ImageRef(c))
	fmt.Fprintf(h, "net=%s\npriv=%t\nbt=%t\ntz=%s\nsrc=%s\nextra=%s\n",
		c.Network, c.Privileged, c.Bluetooth, c.Timezone, ConfigSource(c), strings.TrimSpace(c.ExtraArgs))
	for _, k := range sortedKeys(c.Devices) {
		fmt.Fprintf(h, "dev:%s=%s\n", k, c.Devices[k])
	}
	for _, k := range sortedKeys(c.Volumes) {
		fmt.Fprintf(h, "vol:%s=%s\n", k, c.Volumes[k])
	}
	for _, k := range sortedKeys(c.Environment) {
		fmt.Fprintf(h, "env:%s=%s\n", k, c.Environment[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./internal/dockerargs/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dockerargs/
git commit -m "feat(dockerargs): pure run-arg builder, image ref, spec hash"
```

---

## Task 4: Docker CLI client (mockable)

**Files:** Create `internal/docker/docker.go`, `internal/docker/docker_test.go`.

**Interfaces:**
- Produces: `docker.Runner` (`Output(args ...string) (string, error)`, `Stream(args ...string) error`); `docker.New(bin string) *Client`; `docker.NewWithRunner(Runner) *Client`; methods `Run([]string) error`, `Capture([]string) (string,error)`, `Exists(name) (bool,error)`, `Running(name) (bool,error)`, `SpecHash(name,label) (string,error)`, `Pull(image) error`, `Remove(name) error`, `Start(name) error`, `Stop(name) error`, `FollowLogs(name) error`, `Exec(name string, cmd ...string) error`, `ImageDigest(name) (string,error)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/docker/docker_test.go
package docker

import (
	"strings"
	"testing"
)

type fakeRunner struct {
	outputs map[string]string
	calls   [][]string
}

func (f *fakeRunner) Output(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	return f.outputs[strings.Join(args, " ")], nil
}
func (f *fakeRunner) Stream(args ...string) error {
	f.calls = append(f.calls, args)
	return nil
}

func TestExistsAndRunning(t *testing.T) {
	f := &fakeRunner{outputs: map[string]string{
		"ps -a --filter name=^/homeassistant$ --format {{.Names}}": "homeassistant",
		"inspect -f {{.State.Running}} homeassistant":              "true",
	}}
	c := NewWithRunner(f)
	if ok, _ := c.Exists("homeassistant"); !ok {
		t.Error("Exists should be true")
	}
	if ok, _ := c.Running("homeassistant"); !ok {
		t.Error("Running should be true")
	}
}

func TestImageDigest(t *testing.T) {
	f := &fakeRunner{outputs: map[string]string{
		"inspect -f {{.Image}} homeassistant":                                       "sha256:imgid",
		"inspect -f {{range .RepoDigests}}{{println .}}{{end}} sha256:imgid":         "ghcr.io/home-assistant/home-assistant@sha256:deadbeef\n",
	}}
	c := NewWithRunner(f)
	got, _ := c.ImageDigest("homeassistant")
	if got != "sha256:deadbeef" {
		t.Errorf("ImageDigest = %q, want sha256:deadbeef", got)
	}
}

func TestRunAndCapturePassThrough(t *testing.T) {
	f := &fakeRunner{outputs: map[string]string{"x y": "out"}}
	c := NewWithRunner(f)
	if err := c.Run([]string{"run", "-d"}); err != nil {
		t.Fatal(err)
	}
	if out, _ := c.Capture([]string{"x", "y"}); out != "out" {
		t.Errorf("Capture = %q", out)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/docker/`
Expected: FAIL — `undefined: NewWithRunner`.

- [ ] **Step 3: Implement `docker.go`**

```go
// internal/docker/docker.go
package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner executes docker commands; abstracted for testing.
type Runner interface {
	Output(args ...string) (string, error)
	Stream(args ...string) error
}

type execRunner struct{ bin string }

func (r execRunner) Output(args ...string) (string, error) {
	out, err := exec.Command(r.bin, args...).Output()
	return strings.TrimSpace(string(out)), err
}

func (r execRunner) Stream(args ...string) error {
	cmd := exec.Command(r.bin, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run()
}

// Client wraps the docker operations the launcher needs.
type Client struct{ r Runner }

func New(bin string) *Client         { return &Client{r: execRunner{bin: bin}} }
func NewWithRunner(r Runner) *Client { return &Client{r: r} }

// Run streams `docker <args...>` (used for `run …`, `rm`, `pull`, etc.).
func (c *Client) Run(args []string) error { return c.r.Stream(args...) }

// Capture runs `docker <args...>` and returns trimmed stdout.
func (c *Client) Capture(args []string) (string, error) { return c.r.Output(args...) }

func (c *Client) Exists(name string) (bool, error) {
	out, err := c.r.Output("ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return false, err
	}
	return out == name, nil
}

func (c *Client) Running(name string) (bool, error) {
	out, err := c.r.Output("inspect", "-f", "{{.State.Running}}", name)
	if err != nil {
		return false, nil
	}
	return out == "true", nil
}

func (c *Client) SpecHash(name, label string) (string, error) {
	return c.r.Output("inspect", "-f", fmt.Sprintf("{{ index .Config.Labels %q }}", label), name)
}

// ImageDigest returns the repo digest ("sha256:…") of the named container's
// image, or "" if no repo digest is available (e.g. a locally-built image).
func (c *Client) ImageDigest(name string) (string, error) {
	imageID, err := c.r.Output("inspect", "-f", "{{.Image}}", name)
	if err != nil {
		return "", err
	}
	repo, err := c.r.Output("inspect", "-f", "{{range .RepoDigests}}{{println .}}{{end}}", imageID)
	if err != nil {
		return "", nil
	}
	first := strings.TrimSpace(repo)
	if first == "" {
		return "", nil
	}
	first = strings.SplitN(first, "\n", 2)[0]
	if at := strings.Index(first, "@"); at >= 0 {
		return first[at+1:], nil
	}
	return "", nil
}

func (c *Client) Pull(image string) error      { return c.r.Stream("pull", image) }
func (c *Client) Remove(name string) error     { return c.r.Stream("rm", "-f", name) }
func (c *Client) Start(name string) error      { return c.r.Stream("start", name) }
func (c *Client) Stop(name string) error       { return c.r.Stream("stop", name) }
func (c *Client) FollowLogs(name string) error { return c.r.Stream("logs", "-f", "--tail", "100", name) }
func (c *Client) Exec(name string, cmd ...string) error {
	return c.r.Stream(append([]string{"exec", name}, cmd...)...)
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./internal/docker/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/docker/
git commit -m "feat(docker): mockable docker CLI client with image digest"
```

---

## Task 5: Reconcile logic

**Files:** Create `internal/reconcile/reconcile.go`, `internal/reconcile/reconcile_test.go`.

**Interfaces:**
- Consumes: `config.Config`; `dockerargs.{ContainerName,SpecHashLabel,SpecHash,BuildRunArgs}`; local `Docker` interface (subset of `*docker.Client`: `Exists`, `Running`, `SpecHash`, `Remove`, `Run`, `Start`).
- Produces: `Action` consts; `Reconcile(d Docker, c config.Config, force bool) (Action, error)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/reconcile/reconcile_test.go
package reconcile

import (
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
)

type fakeDocker struct {
	exists, running    bool
	hash               string
	ran, removed, strt bool
}

func (f *fakeDocker) Exists(string) (bool, error)            { return f.exists, nil }
func (f *fakeDocker) Running(string) (bool, error)           { return f.running, nil }
func (f *fakeDocker) SpecHash(string, string) (string, error) { return f.hash, nil }
func (f *fakeDocker) Remove(string) error                    { f.removed = true; return nil }
func (f *fakeDocker) Run([]string) error                     { f.ran = true; return nil }
func (f *fakeDocker) Start(string) error                     { f.strt = true; return nil }

func mustCfg() config.Config { c, _ := config.Parse([]byte(`{}`)); return c }

func TestCreatesWhenMissing(t *testing.T) {
	f := &fakeDocker{exists: false}
	if act, _ := Reconcile(f, mustCfg(), false); act != ActionCreated || !f.ran {
		t.Fatalf("act=%v ran=%v", act, f.ran)
	}
}

func TestRecreatesOnHashMismatch(t *testing.T) {
	f := &fakeDocker{exists: true, hash: "stale"}
	if act, _ := Reconcile(f, mustCfg(), false); act != ActionRecreated || !f.removed || !f.ran {
		t.Fatalf("act=%v removed=%v ran=%v", act, f.removed, f.ran)
	}
}

func TestNoOpWhenMatchingAndRunning(t *testing.T) {
	c := mustCfg()
	f := &fakeDocker{exists: true, running: true, hash: dockerargs.SpecHash(c)}
	if act, _ := Reconcile(f, c, false); act != ActionNone {
		t.Fatalf("act=%v", act)
	}
}

func TestStartsWhenStopped(t *testing.T) {
	c := mustCfg()
	f := &fakeDocker{exists: true, running: false, hash: dockerargs.SpecHash(c)}
	if act, _ := Reconcile(f, c, false); act != ActionStarted || !f.strt {
		t.Fatalf("act=%v strt=%v", act, f.strt)
	}
}

func TestForceRecreates(t *testing.T) {
	c := mustCfg()
	f := &fakeDocker{exists: true, running: true, hash: dockerargs.SpecHash(c)}
	if act, _ := Reconcile(f, c, true); act != ActionRecreated {
		t.Fatalf("act=%v", act)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/reconcile/`
Expected: FAIL — `undefined: Reconcile`.

- [ ] **Step 3: Implement `reconcile.go`**

```go
// internal/reconcile/reconcile.go
package reconcile

import (
	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
)

// Docker is the subset of docker operations Reconcile needs (*docker.Client satisfies it).
type Docker interface {
	Exists(name string) (bool, error)
	Running(name string) (bool, error)
	SpecHash(name, label string) (string, error)
	Remove(name string) error
	Run(args []string) error
	Start(name string) error
}

type Action string

const (
	ActionNone      Action = "none"
	ActionStarted   Action = "started"
	ActionCreated   Action = "created"
	ActionRecreated Action = "recreated"
)

// Reconcile ensures the Home Assistant container matches c. When force is true
// the container is always recreated (used after pulling a new image).
func Reconcile(d Docker, c config.Config, force bool) (Action, error) {
	name := dockerargs.ContainerName
	want := dockerargs.SpecHash(c)
	args := dockerargs.BuildRunArgs(c)

	exists, err := d.Exists(name)
	if err != nil {
		return ActionNone, err
	}
	if !exists {
		if err := d.Run(args); err != nil {
			return ActionNone, err
		}
		return ActionCreated, nil
	}

	have, err := d.SpecHash(name, dockerargs.SpecHashLabel)
	if err != nil {
		return ActionNone, err
	}
	if force || have != want {
		if err := d.Remove(name); err != nil {
			return ActionNone, err
		}
		if err := d.Run(args); err != nil {
			return ActionNone, err
		}
		return ActionRecreated, nil
	}

	running, err := d.Running(name)
	if err != nil {
		return ActionNone, err
	}
	if !running {
		if err := d.Start(name); err != nil {
			return ActionNone, err
		}
		return ActionStarted, nil
	}
	return ActionNone, nil
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./internal/reconcile/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/
git commit -m "feat(reconcile): create/recreate/start by spec hash"
```

---

## Task 6: Backup/restore argument builders

**Files:** Create `internal/backup/backup.go`, `internal/backup/backup_test.go`.

**Interfaces:**
- Produces: const `BackupsVolume="homeassistant-backups"`; `SnapshotArgs(source, image, name, digest string) []string`; `RestoreArgs(source, image, name string) []string`; `ListArgs(image string) []string`; `ReadMetaArgs(image, name string) []string`; `ParseLatest(lsOutput string) string`.
- These return `docker run --rm …` arg lists that tar/untar `/config` against the `homeassistant-backups` volume using the HA image's shell. `source` comes from `dockerargs.ConfigSource(cfg)`; `image` from `dockerargs.ImageRef(cfg)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/backup/backup_test.go
package backup

import (
	"strings"
	"testing"
)

func join(a []string) string { return strings.Join(a, " ") }

func TestSnapshotArgs(t *testing.T) {
	got := join(SnapshotArgs("homeassistant-config", "img:tag", "pre-update-20260622-100000", "sha256:abc"))
	for _, want := range []string{
		"run --rm",
		"-v homeassistant-config:/config:ro",
		"-v homeassistant-backups:/backups",
		"--entrypoint sh",
		"img:tag",
		"tar czf /backups/pre-update-20260622-100000.tar.gz -C /config .",
		"/backups/pre-update-20260622-100000.image",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n got: %s", want, got)
		}
	}
}

func TestRestoreArgsWritable(t *testing.T) {
	got := join(RestoreArgs("/srv/ha", "img:tag", "manual-1"))
	if !strings.Contains(got, "-v /srv/ha:/config") || strings.Contains(got, "/srv/ha:/config:ro") {
		t.Errorf("restore source must be writable: %s", got)
	}
	if !strings.Contains(got, "tar xzf /backups/manual-1.tar.gz -C /config") {
		t.Errorf("missing untar: %s", got)
	}
}

func TestParseLatest(t *testing.T) {
	ls := "manual-20260601-090000.tar.gz\nmanual-20260601-090000.image\npre-update-20260622-100000.tar.gz\npre-update-20260622-100000.image\n"
	if got := ParseLatest(ls); got != "pre-update-20260622-100000" {
		t.Errorf("ParseLatest = %q", got)
	}
	if got := ParseLatest("nothing\n"); got != "" {
		t.Errorf("ParseLatest empty = %q", got)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./internal/backup/`
Expected: FAIL — `undefined: SnapshotArgs`.

- [ ] **Step 3: Implement `backup.go`**

```go
// internal/backup/backup.go
package backup

import (
	"fmt"
	"sort"
	"strings"
)

// BackupsVolume is the docker named volume holding snapshots.
const BackupsVolume = "homeassistant-backups"

// SnapshotArgs builds `docker run --rm` args that tar /config into the backups
// volume as <name>.tar.gz and write the image digest to <name>.image.
func SnapshotArgs(source, image, name, digest string) []string {
	script := fmt.Sprintf(
		"set -e; tar czf /backups/%s.tar.gz -C /config .; printf '%%s' %q > /backups/%s.image",
		name, digest, name)
	return []string{
		"run", "--rm",
		"-v", source + ":/config:ro",
		"-v", BackupsVolume + ":/backups",
		"--entrypoint", "sh",
		image, "-c", script,
	}
}

// RestoreArgs builds `docker run --rm` args that clear /config and untar the
// named snapshot back into it. /config is mounted writable here.
func RestoreArgs(source, image, name string) []string {
	script := fmt.Sprintf(
		"set -e; find /config -mindepth 1 -delete; tar xzf /backups/%s.tar.gz -C /config",
		name)
	return []string{
		"run", "--rm",
		"-v", source + ":/config",
		"-v", BackupsVolume + ":/backups",
		"--entrypoint", "sh",
		image, "-c", script,
	}
}

// ListArgs builds args that list the backups volume contents (one per line).
func ListArgs(image string) []string {
	return []string{
		"run", "--rm",
		"-v", BackupsVolume + ":/backups",
		"--entrypoint", "sh",
		image, "-c", "ls -1 /backups 2>/dev/null || true",
	}
}

// ReadMetaArgs builds args that print the recorded image digest for a snapshot.
func ReadMetaArgs(image, name string) []string {
	return []string{
		"run", "--rm",
		"-v", BackupsVolume + ":/backups",
		"--entrypoint", "sh",
		image, "-c", fmt.Sprintf("cat /backups/%s.image 2>/dev/null || true", name),
	}
}

// ParseLatest returns the newest snapshot base name (lexical sort works because
// names embed a sortable YYYYMMDD-HHMMSS timestamp), or "" if none.
func ParseLatest(lsOutput string) string {
	var names []string
	for _, line := range strings.Split(lsOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".tar.gz") {
			names = append(names, strings.TrimSuffix(line, ".tar.gz"))
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return names[len(names)-1]
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./internal/backup/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backup/
git commit -m "feat(backup): snapshot/restore/list arg builders"
```

---

## Task 7: Launcher entrypoint (subcommands)

**Files:** Create `cmd/launcher/main.go`.

**Interfaces:**
- Consumes everything above. Produces a `launcher` binary with subcommands: `daemon`, `reconcile`, `update`, `backup`, `rollback`, `check-config`, `cli`.

> Gate: `go build ./...` + `go vet ./...` + `go test ./...`. Runtime behavior is verified by the smoke test (Task 9).

- [ ] **Step 1: Implement `main.go`**

```go
// cmd/launcher/main.go
package main

import (
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

func dockerBin() string { return filepath.Join(os.Getenv("SNAP"), "docker-snap", "docker") }

func loadConfig() (config.Config, error) {
	out, err := exec.Command("snapctl", "get", "-d").Output()
	if err != nil {
		return config.Config{}, fmt.Errorf("reading snap config: %w", err)
	}
	return config.Parse(out)
}

func snapctlSet(key, value string) error {
	return exec.Command("snapctl", "set", key+"="+value).Run()
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: launcher <daemon|reconcile|update|backup|rollback|check-config|cli>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cmd string) error {
	cli := docker.New(dockerBin())
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
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
```

- [ ] **Step 2: Build, vet, and test everything**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: exit 0; all package tests PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/launcher/main.go
git commit -m "feat(launcher): daemon, reconcile, update, backup, rollback, cli"
```

---

## Task 8: Configure hook

**Files:** Create `snap/hooks/configure`.

- [ ] **Step 1: Write the hook**

```sh
#!/bin/sh
set -e
# Re-apply configuration by restarting the launcher daemon, which reconciles the
# Home Assistant container to the (possibly changed) snap config. Reconcile is
# idempotent: a spec-hash label means an unchanged config is a no-op.
if snapctl services "${SNAP_INSTANCE_NAME}.daemon" >/dev/null 2>&1; then
    snapctl restart "${SNAP_INSTANCE_NAME}.daemon"
fi
```

- [ ] **Step 2: Make executable + lint**

Run: `chmod +x snap/hooks/configure && shellcheck snap/hooks/configure`
Expected: executable; no shellcheck warnings. (If `shellcheck` is unavailable, `sh -n snap/hooks/configure` reports no syntax errors.)

- [ ] **Step 3: Commit**

```bash
git add snap/hooks/configure
git commit -m "feat(hook): configure hook restarts daemon to reconcile"
```

---

## Task 9: Snap packaging + local smoke test

**Files:** Create `snap/snapcraft.yaml`.

- [ ] **Step 1: Write `snapcraft.yaml`**

```yaml
name: home-assistant
title: Home Assistant
version: '0.1.0'
summary: Home Assistant (Container) managed via the Docker snap
description: |
  Runs the official Home Assistant container image
  (ghcr.io/home-assistant/home-assistant) using the Docker snap.

  Requires the docker snap (installed automatically on first install). After
  installing, connect the interfaces:

    snap connect home-assistant:docker docker:docker-daemon
    snap connect home-assistant:docker-executables docker:docker-executables

  Configure, e.g.:

    snap set home-assistant docker.timezone=Europe/Oslo
    snap set home-assistant docker.devices.zigbee=/dev/serial/by-id/...
    snap set home-assistant docker.config-dir=/home/<user>/home-assistant   # opt-in bind mount

base: core26
confinement: strict
grade: stable

platforms:
  amd64:
  arm64:

plugs:
  docker:
    interface: docker
  docker-executables:
    interface: content
    content: docker-executables
    target: $SNAP/docker-snap
    default-provider: docker

apps:
  daemon:
    command: bin/launcher daemon
    daemon: simple
    restart-condition: always
    plugs: [docker]
  reconcile:
    command: bin/launcher reconcile
    plugs: [docker]
  update:
    command: bin/launcher update
    plugs: [docker]
  backup:
    command: bin/launcher backup
    plugs: [docker]
  rollback:
    command: bin/launcher rollback
    plugs: [docker]
  check-config:
    command: bin/launcher check-config
    plugs: [docker]
  cli:
    command: bin/launcher cli
    plugs: [docker]

parts:
  launcher:
    plugin: go
    source: .
    build-snaps:
      - go
```

> Verify at build: the `go` plugin places the `cmd/launcher` binary at `$SNAP/bin/launcher` (core26 default). If the name differs, add an `organize:` mapping to `bin/launcher`. The `snap/hooks/configure` hook is included automatically.

- [ ] **Step 2: Build the snap**

Run: `snapcraft`
Expected: produces `home-assistant_0.1.0_amd64.snap` (or `_arm64`).

- [ ] **Step 3: Install + connect**

```bash
sudo snap install --dangerous ./home-assistant_0.1.0_*.snap
sudo snap install docker
sudo snap connect home-assistant:docker docker:docker-daemon
sudo snap connect home-assistant:docker-executables docker:docker-executables
```
Expected: both snaps installed; connections succeed.

- [ ] **Step 4: Smoke test (run, config, update, rollback)**

```bash
sudo snap set home-assistant docker.timezone=Europe/Oslo
sleep 90
docker ps --filter name=homeassistant --format '{{.Names}} {{.Status}}'
curl -sf http://localhost:8123 >/dev/null && echo "HA reachable"
sudo snap run home-assistant.backup            # creates a manual snapshot
sudo snap run home-assistant.update            # snapshot -> pull -> recreate
docker volume ls | grep homeassistant          # homeassistant-config + homeassistant-backups exist
sudo snap run home-assistant.rollback          # restores latest snapshot + pins digest
sudo snap get home-assistant image.digest      # shows the pinned digest after rollback
```
Expected: `HA reachable`; both volumes present; update/rollback report actions without error.

- [ ] **Step 5: Commit**

```bash
git add snap/snapcraft.yaml
git commit -m "feat(snap): package home-assistant launcher (core24, strict)"
```

---

## Task 10: Ubuntu Core validation (hard requirement)

**Files:** Create `docs/superpowers/notes/2026-06-22-ubuntu-core-validation.md` with results.

- [ ] **Step 1: Provision Ubuntu Core 24 or 26** (VM or device). Confirm it is Ubuntu Core (`snap version` → `series 16`). A core26-based app runs on either Core version (snapd pulls the core26 base).

- [ ] **Step 2: Install + connect**

```bash
snap install docker
snap install --dangerous ./home-assistant_0.1.0_arm64.snap   # or amd64
snap connect home-assistant:docker docker:docker-daemon
snap connect home-assistant:docker-executables docker:docker-executables
sleep 120
curl -sf http://localhost:8123 >/dev/null && echo "HA reachable on Core"
```
Expected: installs/connects (strict only) and HA is reachable.

- [ ] **Step 3: Verify serial passthrough** (USB Zigbee/Z-Wave attached)

```bash
ls -l /dev/serial/by-id/
snap set home-assistant docker.devices.zigbee=/dev/serial/by-id/<your-stick>
docker exec homeassistant ls -l /dev/serial/by-id/
```
Expected: the device node is visible inside the container.

- [ ] **Step 4: Validate Bluetooth (open caveat)** — record whether host BlueZ (the `bluez` snap) is reachable from the container over `/run/dbus`; mark Bluetooth supported/unsupported in the validation doc.

- [ ] **Step 5: Verify update/rollback on Core** — `snap run home-assistant.update` then `snap run home-assistant.rollback`; confirm config persists and the image digest pins.

- [ ] **Step 6: Commit the validation record**

```bash
git add docs/superpowers/notes/2026-06-22-ubuntu-core-validation.md
git commit -m "test: record Ubuntu Core validation results"
```

---

## Post-implementation (separate follow-ups, not v1 tasks)

- Snap Store **auto-connection** request for `docker`/`docker-executables` (one-command install).
- Investigate narrowing `--privileged` (specific caps + AppArmor) once the USB+BT minimum is known.
- v2: companion containers under `companions.*` (Z-Wave JS, Matter Server, Mosquitto); off-box backup export helper.

---

## Self-Review

**1. Spec coverage:**
- Architecture / launcher / docker dependency → Tasks 4–9. ✓
- Declarative `docker.*` config (managed/overridable/additive) → Tasks 1–3. ✓
- `/config` named-volume default + bind opt-in (`ConfigSource`) → Task 3; validated Task 10. ✓
- Hardware passthrough (devices, bluetooth/dbus, host net) → Task 3 args; Task 10 on-device. ✓
- Update model + **snapshot/rollback + digest pin** → Tasks 6–7 (`backup`, `update`, `rollback`), `image.digest` threaded through Tasks 1/3. ✓
- Install/connect UX → Task 9 (manual connect; auto-connection in follow-ups). ✓
- Companion containers deferred → noted v2. ✓
- Testing incl. real Ubuntu Core → Task 10. ✓
- Error handling (socket/pull/crash/invalid config/no-snapshot) → reconcile + Validate + daemon signals + rollback guard. ✓

**2. Placeholder scan:** No `TBD`/`TODO`/"add error handling"/"similar to Task N". One explicit verification note (go-plugin binary path) — a check, not missing content.

**3. Type consistency:** `config.Config` (incl. `ImageDigest`) used identically across `dockerargs`, `backup` callers, `reconcile`, `main`. `reconcile.Docker` is a strict subset of `*docker.Client` (`Exists`,`Running`,`SpecHash(name,label)`,`Remove`,`Run([]string)`,`Start`) — signatures match (note `Run` replaced the earlier `RunDetached`; reconcile and its fake both use `Run`). `dockerargs.{ConfigSource,ImageRef,SpecHash,BuildRunArgs,ContainerName,SpecHashLabel}` and `backup.{SnapshotArgs,RestoreArgs,ListArgs,ReadMetaArgs,ParseLatest,BackupsVolume}` referenced consistently in `main`. `main` uses `cli.ImageDigest`/`cli.Capture`/`cli.Run` which all exist on `*docker.Client`.
