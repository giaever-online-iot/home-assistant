# Phase 3 — Add-on Containers + Ingress Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run network-only add-on containers (Node-RED, Mosquitto, …) alongside HA Core, declared via `snap set addons.*`, reconciled by the launcher, and auto-surfaced in the HA sidebar via hass_ingress.

**Architecture:** The single-container reconcile ladder is generalized around a `ContainerSpec` value type; HA and add-ons flow through one code path, with orphan removal via a discovery label. Add-ons join a `ha-addons` docker bridge with loopback-published ports (model A default; model B = HA on the bridge too, opt-in). Ingress panels are derived from add-on specs, merged with explicit `ingress.*` entries, and auto-synced during reconcile (compare-first, restart HA only on change).

**Tech Stack:** Go (stdlib only — no new dependencies), existing fakes for docker (`Runner`, `fakeDocker`), snapd config via `snapctl`.

**Spec:** `docs/superpowers/specs/2026-07-09-addons-phase3-design.md` (approved). Read it before starting.

## Global Constraints

- Work on branch `feat/addons` off `main`. Conventional-commit messages (`feat(...)`, `test(...)`, `docs(...)`).
- Names verbatim: network `ha-addons`; containers `ha-addon-<name>`; data volumes `ha-addon-<name>-data`; discovery label key `io.giaever.home-assistant.addon`; HA container `homeassistant`.
- Default port bind IP is `127.0.0.1`; LAN exposure only via an explicit ip in the port value.
- **No behavior change for the HA container's reconcile** — existing ladder semantics (pull-before-remove, hash label, start-if-stopped) are preserved exactly.
- `AddonSpecHash` excludes `ingress.*` fields: panel changes must not recreate containers.
- Every task: write the failing test first, see it fail, implement, see it pass, `gofmt`, commit.
- `devices.*` under add-ons is reserved and must FAIL validation (message points at a later release).
- Add-on names must match `^[a-z0-9][a-z0-9-]*$`.
- Comment style: explain constraints/invariants, not narration. Match existing files.

---

### Task 1: Port-spec parsing (`config.PortSpec`, `config.ParsePortSpec`)

**Files:**
- Create: `internal/config/addons.go`
- Test: `internal/config/addons_test.go`

**Interfaces:**
- Consumes: nothing (pure).
- Produces: `type PortSpec struct { IP, Host, Container string }`; `func ParsePortSpec(v string) (PortSpec, error)`. Later tasks call these exact names.

- [ ] **Step 1: Write the failing test**

Create `internal/config/addons_test.go`:

```go
package config

import "testing"

func TestParsePortSpec(t *testing.T) {
	cases := []struct {
		in   string
		want PortSpec
	}{
		{"1880", PortSpec{"127.0.0.1", "1880", "1880"}},
		{"8080:1880", PortSpec{"127.0.0.1", "8080", "1880"}},
		{"0.0.0.0:1883:1883", PortSpec{"0.0.0.0", "1883", "1883"}},
	}
	for _, c := range cases {
		got, err := ParsePortSpec(c.in)
		if err != nil {
			t.Errorf("ParsePortSpec(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParsePortSpec(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParsePortSpecRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "abc", "0", "65536", "80:", ":80", "1.2.3.4:80:81:82", "1.2.3.4::80"} {
		if _, err := ParsePortSpec(in); err == nil {
			t.Errorf("ParsePortSpec(%q): expected error", in)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestParsePortSpec -v`
Expected: FAIL — `undefined: PortSpec` / `undefined: ParsePortSpec` (compile error counts as the failing state).

- [ ] **Step 3: Write minimal implementation**

Create `internal/config/addons.go`:

```go
// Add-on config: port specs, the AddonSpec schema, and its validation helpers.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// PortSpec is one published add-on port. IP defaults to loopback so add-on
// UIs are reachable by HA (host netns) but invisible to the LAN — HA's auth
// stays in front. LAN exposure requires an explicit ip in the config value.
type PortSpec struct {
	IP, Host, Container string
}

// ParsePortSpec parses "port", "host:container" or "ip:host:container".
// (IPv6 ips are not supported — three colon-separated parts maximum.)
func ParsePortSpec(v string) (PortSpec, error) {
	parts := strings.Split(v, ":")
	switch len(parts) {
	case 1:
		if !validPort(parts[0]) {
			return PortSpec{}, fmt.Errorf("invalid port %q", v)
		}
		return PortSpec{IP: "127.0.0.1", Host: parts[0], Container: parts[0]}, nil
	case 2:
		if !validPort(parts[0]) || !validPort(parts[1]) {
			return PortSpec{}, fmt.Errorf("invalid port mapping %q (want host:container)", v)
		}
		return PortSpec{IP: "127.0.0.1", Host: parts[0], Container: parts[1]}, nil
	case 3:
		if parts[0] == "" || !validPort(parts[1]) || !validPort(parts[2]) {
			return PortSpec{}, fmt.Errorf("invalid port mapping %q (want ip:host:container)", v)
		}
		return PortSpec{IP: parts[0], Host: parts[1], Container: parts[2]}, nil
	default:
		return PortSpec{}, fmt.Errorf("invalid port mapping %q", v)
	}
}

func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS (all tests, including pre-existing ones).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/config/ && git add internal/config/ && git commit -m "feat(config): add-on port-spec parsing (loopback-default publish)"
```

---

### Task 2: `AddonSpec` type + parsing the `addons` namespace

**Files:**
- Modify: `internal/config/addons.go` (types), `internal/config/config.go` (rawConfig + Parse)
- Test: `internal/config/addons_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces (later tasks use these exact names): on `Config`: `Addons map[string]AddonSpec`. Types:
  `type AddonSpec struct { Image string; Ports map[string]string; DataDir string; Volumes map[string]string; Environment map[string]string; Devices map[string]string; Ingress *AddonIngress }`
  `type AddonIngress struct { Title, Icon, Port string; RequireAdmin bool }`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/addons_test.go`:

```go
func TestParseAddons(t *testing.T) {
	in := `{"addons":{"nodered":{
		"image":"nodered/node-red:latest",
		"ports":{"ui":1880,"alt":"8080:1880"},
		"data-dir":"/data",
		"volumes":{"certs":"/etc/certs:/certs:ro"},
		"environment":{"foo":"bar"},
		"ingress":{"title":"Node-RED","icon":"mdi:nodejs","require-admin":true}
	}}}`
	c, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a, ok := c.Addons["nodered"]
	if !ok {
		t.Fatal("addon nodered not parsed")
	}
	if a.Image != "nodered/node-red:latest" || a.DataDir != "/data" {
		t.Errorf("image/data-dir wrong: %+v", a)
	}
	// JSON numbers (snapctl emits unquoted ints) normalize to strings.
	if a.Ports["ui"] != "1880" || a.Ports["alt"] != "8080:1880" {
		t.Errorf("ports wrong: %+v", a.Ports)
	}
	if a.Volumes["certs"] != "/etc/certs:/certs:ro" || a.Environment["foo"] != "bar" {
		t.Errorf("volumes/env wrong: %+v", a)
	}
	if a.Ingress == nil || a.Ingress.Title != "Node-RED" || a.Ingress.Icon != "mdi:nodejs" || !a.Ingress.RequireAdmin {
		t.Errorf("ingress wrong: %+v", a.Ingress)
	}
}

func TestParseAddonsAbsent(t *testing.T) {
	c, err := Parse([]byte(`{}`))
	if err != nil || c.Addons != nil {
		t.Fatalf("no addons should parse to nil map, got %+v err=%v", c.Addons, err)
	}
	c2, _ := Parse([]byte(`{"addons":{"x":{"image":"img"}}}`))
	if c2.Addons["x"].Ingress != nil {
		t.Error("no ingress keys should mean a nil Ingress (no panel wanted)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestParseAddons -v`
Expected: FAIL — `c.Addons undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/config/addons.go`:

```go
// AddonSpec is one launcher-managed add-on container (network-only; devices
// are reserved for a later release and rejected at validation).
type AddonSpec struct {
	Image       string
	Ports       map[string]string // label → "port" | "host:container" | "ip:host:container"
	DataDir     string            // absolute container path → named volume ha-addon-<name>-data
	Volumes     map[string]string
	Environment map[string]string
	Devices     map[string]string // parsed only to be rejected (reserved)
	Ingress     *AddonIngress     // nil = no sidebar panel wanted
}

// AddonIngress describes the add-on's sidebar panel. Port names a ports.*
// label; empty means auto (the "ui" label, else the only port).
type AddonIngress struct {
	Title        string
	Icon         string
	Port         string
	RequireAdmin bool
}

type rawAddon struct {
	Image       string            `json:"image"`
	Ports       map[string]any    `json:"ports"` // any: snapctl emits numbers unquoted
	DataDir     string            `json:"data-dir"`
	Volumes     map[string]string `json:"volumes"`
	Environment map[string]string `json:"environment"`
	Devices     map[string]string `json:"devices"`
	Ingress     *struct {
		Title        string `json:"title"`
		Icon         string `json:"icon"`
		Port         any    `json:"port"`
		RequireAdmin *bool  `json:"require-admin"`
	} `json:"ingress"`
}

func (r rawAddon) toSpec() AddonSpec {
	s := AddonSpec{
		Image:       r.Image,
		DataDir:     r.DataDir,
		Volumes:     r.Volumes,
		Environment: r.Environment,
		Devices:     r.Devices,
	}
	if len(r.Ports) > 0 {
		s.Ports = make(map[string]string, len(r.Ports))
		for k, v := range r.Ports {
			s.Ports[k] = anyToString(v)
		}
	}
	if r.Ingress != nil {
		s.Ingress = &AddonIngress{
			Title:        r.Ingress.Title,
			Icon:         r.Ingress.Icon,
			Port:         anyToString(r.Ingress.Port),
			RequireAdmin: boolOrDefault(r.Ingress.RequireAdmin, false),
		}
	}
	return s
}

// anyToString normalizes snapctl JSON scalars: integers arrive as float64 and
// %v prints them without a decimal point (1880.0 → "1880").
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
```

In `internal/config/config.go`, add to `rawConfig` (after the `Ingress` field):

```go
	Addons map[string]rawAddon `json:"addons"`
```

And in `Parse`, before the final `return`:

```go
	var addons map[string]AddonSpec
	if len(raw.Addons) > 0 {
		addons = make(map[string]AddonSpec, len(raw.Addons))
		for name, a := range raw.Addons {
			addons[name] = a.toSpec()
		}
	}
```

…and add `Addons: addons,` to the returned `Config` literal, plus `Addons map[string]AddonSpec` to the `Config` struct (after `Ingress`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/config/ && git add internal/config/ && git commit -m "feat(config): parse addons.* into AddonSpec"
```

---

### Task 3: Ingress-port resolution (`AddonSpec.IngressPortSpec`)

**Files:**
- Modify: `internal/config/addons.go`
- Test: `internal/config/addons_test.go`

**Interfaces:**
- Consumes: `ParsePortSpec` (Task 1).
- Produces: `func (s AddonSpec) IngressPortSpec() (PortSpec, error)` — used by validation (Task 4) and ingress derivation (Task 10).

- [ ] **Step 1: Write the failing test**

Append to `internal/config/addons_test.go`:

```go
func TestIngressPortSpec(t *testing.T) {
	ing := &AddonIngress{Title: "X"}
	// "ui" label wins even with several ports.
	s := AddonSpec{Ports: map[string]string{"ui": "1880", "metrics": "9100"}, Ingress: ing}
	if ps, err := s.IngressPortSpec(); err != nil || ps.Host != "1880" {
		t.Errorf("ui label: %+v, %v", ps, err)
	}
	// A single port is unambiguous.
	s = AddonSpec{Ports: map[string]string{"web": "3000"}, Ingress: ing}
	if ps, err := s.IngressPortSpec(); err != nil || ps.Host != "3000" {
		t.Errorf("single port: %+v, %v", ps, err)
	}
	// Explicit ingress.port selects a label.
	s = AddonSpec{Ports: map[string]string{"a": "1000", "b": "2000"}, Ingress: &AddonIngress{Port: "b"}}
	if ps, err := s.IngressPortSpec(); err != nil || ps.Host != "2000" {
		t.Errorf("explicit label: %+v, %v", ps, err)
	}
}

func TestIngressPortSpecErrors(t *testing.T) {
	ing := &AddonIngress{}
	cases := []AddonSpec{
		{Ingress: ing},                                                            // no ports at all
		{Ports: map[string]string{"a": "1000", "b": "2000"}, Ingress: ing},        // ambiguous
		{Ports: map[string]string{"a": "1000"}, Ingress: &AddonIngress{Port: "z"}}, // unknown label
		{Ingress: nil}, // no panel wanted
	}
	for i, s := range cases {
		if _, err := s.IngressPortSpec(); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestIngressPortSpec -v`
Expected: FAIL — `s.IngressPortSpec undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/config/addons.go`:

```go
// IngressPortSpec resolves which published port the add-on's sidebar panel
// proxies to: the explicit ingress.port label, else the "ui" label, else the
// only port. Anything else is ambiguous and is a validation error.
func (s AddonSpec) IngressPortSpec() (PortSpec, error) {
	if s.Ingress == nil {
		return PortSpec{}, fmt.Errorf("no ingress panel configured")
	}
	label := s.Ingress.Port
	if label == "" {
		switch {
		case s.Ports["ui"] != "":
			label = "ui"
		case len(s.Ports) == 1:
			for l := range s.Ports {
				label = l
			}
		case len(s.Ports) == 0:
			return PortSpec{}, fmt.Errorf("a panel needs at least one ports.* entry")
		default:
			return PortSpec{}, fmt.Errorf("several ports and none labeled \"ui\" — set ingress.port to one of the labels")
		}
	}
	v, ok := s.Ports[label]
	if !ok {
		return PortSpec{}, fmt.Errorf("ingress.port=%q matches no ports.* label", label)
	}
	return ParsePortSpec(v)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/config/ && git add internal/config/ && git commit -m "feat(config): resolve an add-on panel's target port"
```

---

### Task 4: Add-on validation rules

**Files:**
- Modify: `internal/config/addons.go` (a `validateAddons` helper), `internal/config/config.go` (call it from `Validate`)
- Test: `internal/config/addons_test.go`

**Interfaces:**
- Consumes: `ParsePortSpec`, `IngressPortSpec`.
- Produces: extended `Config.Validate()` — same signature as today.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/addons_test.go`:

```go
func TestValidateAddonsRejectsBadInputs(t *testing.T) {
	bad := []string{
		`{"addons":{"x":{}}}`,                                                        // missing image
		`{"addons":{"x":{"image":"img","ports":{"ui":"abc"}}}}`,                      // bad port
		`{"addons":{"x":{"image":"img","data-dir":"relative"}}}`,                     // relative data-dir
		`{"addons":{"x":{"image":"img","volumes":{"v":"/just-a-path"}}}}`,            // volume without :
		`{"addons":{"x":{"image":"img","devices":{"z":"/dev/ttyACM0"}}}}`,            // devices reserved
		`{"addons":{"Bad_Name":{"image":"img"}}}`,                                    // invalid name
		`{"addons":{"x":{"image":"img","ingress":{"title":"X"}}}}`,                   // panel with no ports
		`{"addons":{"x":{"image":"img","ports":{"a":"1","b":"2"},"ingress":{}}}}`,    // ambiguous panel port
		`{"addons":{"x":{"image":"img","ports":{"ui":"80"},"ingress":{}}},"ingress":{"x":{"url":"http://y"}}}`, // name collision
	}
	for _, in := range bad {
		c, err := Parse([]byte(in))
		if err != nil {
			t.Fatalf("parse should not fail for %s: %v", in, err)
		}
		if _, err := c.Validate(); err == nil {
			t.Errorf("expected validate error for %s", in)
		}
	}
}

func TestValidateAddonsAcceptsGood(t *testing.T) {
	in := `{"addons":{"nodered":{"image":"nodered/node-red:latest","ports":{"ui":1880},"data-dir":"/data","ingress":{"title":"Node-RED"}}}}`
	c, _ := Parse([]byte(in))
	if _, err := c.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestValidateAddons -v`
Expected: FAIL — the bad inputs pass validation (no error returned yet).

- [ ] **Step 3: Write minimal implementation**

Append to `internal/config/addons.go` (add `"regexp"` to its imports):

```go
// Add-on names become container/volume names (ha-addon-<name>); keep them
// DNS- and docker-safe.
var addonNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (c Config) validateAddons() error {
	for name, a := range c.Addons {
		if !addonNameRE.MatchString(name) {
			return fmt.Errorf("addons.%s: name must match %s", name, addonNameRE)
		}
		if a.Image == "" {
			return fmt.Errorf("addons.%s.image is required", name)
		}
		for label, v := range a.Ports {
			if _, err := ParsePortSpec(v); err != nil {
				return fmt.Errorf("addons.%s.ports.%s: %v", name, label, err)
			}
		}
		if a.DataDir != "" && !strings.HasPrefix(a.DataDir, "/") {
			return fmt.Errorf("addons.%s.data-dir=%q: must be an absolute container path", name, a.DataDir)
		}
		for label, v := range a.Volumes {
			if !strings.Contains(v, ":") {
				return fmt.Errorf("addons.%s.volumes.%s=%q: volume must be host:container", name, label, v)
			}
		}
		if len(a.Devices) > 0 {
			return fmt.Errorf("addons.%s.devices.*: device passthrough lands in a later release — unset it for now", name)
		}
		if a.Ingress != nil {
			if _, err := a.IngressPortSpec(); err != nil {
				return fmt.Errorf("addons.%s.ingress: %v", name, err)
			}
			if _, taken := c.Ingress[name]; taken {
				return fmt.Errorf("addons.%s: panel name collides with ingress.%s — rename one of them", name, name)
			}
		}
	}
	return nil
}
```

In `internal/config/config.go`, at the end of `Validate` (before `return warnings, nil`):

```go
	if err := c.validateAddons(); err != nil {
		return warnings, err
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/config/ && git add internal/config/ && git commit -m "feat(config): validate addons.* (fatal on misuse, devices reserved)"
```

---

### Task 5: Add-on docker args + spec hash (`internal/dockerargs`)

**Files:**
- Create: `internal/dockerargs/addonargs.go`
- Test: `internal/dockerargs/addonargs_test.go`

**Interfaces:**
- Consumes: `config.AddonSpec`, `config.ParsePortSpec`, package-local `sortedKeys`, `SpecHashLabel`.
- Produces (exact names used by Tasks 9–13):
  `const AddonNetwork = "ha-addons"`, `const AddonLabelKey = "io.giaever.home-assistant.addon"`,
  `func AddonContainerName(name string) string`, `func AddonDataVolume(name string) string`,
  `func BuildAddonRunArgs(name string, s config.AddonSpec) []string`, `func AddonSpecHash(name string, s config.AddonSpec) string`.

- [ ] **Step 1: Write the failing test**

Create `internal/dockerargs/addonargs_test.go`:

```go
package dockerargs

import (
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

func addonCfg(json string) config.AddonSpec {
	c, _ := config.Parse([]byte(json))
	for _, a := range c.Addons {
		return a
	}
	return config.AddonSpec{}
}

func TestBuildAddonRunArgs(t *testing.T) {
	a := addonCfg(`{"addons":{"nodered":{
		"image":"nodered/node-red:latest",
		"ports":{"ui":1880,"mqtt":"0.0.0.0:1883:1883"},
		"data-dir":"/data",
		"volumes":{"certs":"/etc/certs:/certs:ro"},
		"environment":{"foo":"bar"},
		"ingress":{"title":"Node-RED"}
	}}}`)
	got := strings.Join(BuildAddonRunArgs("nodered", a), " ")
	for _, want := range []string{
		"run -d", "--name ha-addon-nodered", "--restart=unless-stopped",
		"--network=ha-addons",
		"--label io.giaever.home-assistant.addon=nodered",
		"-p 0.0.0.0:1883:1883", // explicit ip preserved
		"-p 127.0.0.1:1880:1880", // loopback default
		"-v ha-addon-nodered-data:/data",
		"-v /etc/certs:/certs:ro",
		"-e foo=bar",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\n got: %s", want, got)
		}
	}
	args := BuildAddonRunArgs("nodered", a)
	if args[len(args)-1] != "nodered/node-red:latest" {
		t.Errorf("image must be the last arg, got %q", args[len(args)-1])
	}
}

func TestAddonSpecHash(t *testing.T) {
	base := `{"addons":{"x":{"image":"img:1","ports":{"ui":80}}}}`
	if AddonSpecHash("x", addonCfg(base)) != AddonSpecHash("x", addonCfg(base)) {
		t.Error("hash must be deterministic")
	}
	if AddonSpecHash("x", addonCfg(base)) == AddonSpecHash("x", addonCfg(`{"addons":{"x":{"image":"img:2","ports":{"ui":80}}}}`)) {
		t.Error("hash must change with the image")
	}
	if AddonSpecHash("x", addonCfg(base)) == AddonSpecHash("y", addonCfg(base)) {
		t.Error("hash must change with the add-on name")
	}
	// Panel changes must NOT recreate the container.
	withPanel := `{"addons":{"x":{"image":"img:1","ports":{"ui":80},"ingress":{"title":"X"}}}}`
	if AddonSpecHash("x", addonCfg(base)) != AddonSpecHash("x", addonCfg(withPanel)) {
		t.Error("ingress fields must not affect the spec hash")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dockerargs/ -run 'TestBuildAddonRunArgs|TestAddonSpecHash' -v`
Expected: FAIL — `undefined: BuildAddonRunArgs` etc.

- [ ] **Step 3: Write minimal implementation**

Create `internal/dockerargs/addonargs.go`:

```go
package dockerargs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

const (
	// AddonNetwork is the docker bridge all add-on containers join (and HA
	// too, under the opt-in model B via docker.network=ha-addons).
	AddonNetwork = "ha-addons"
	// AddonLabelKey marks launcher-managed add-on containers; orphan cleanup
	// only ever removes containers carrying it.
	AddonLabelKey = "io.giaever.home-assistant.addon"

	addonPrefix = "ha-addon-"
)

func AddonContainerName(name string) string { return addonPrefix + name }

// AddonDataVolume is the named volume behind data-dir. It deliberately
// survives add-on removal — deleting data is a manual `docker volume rm`.
func AddonDataVolume(name string) string { return addonPrefix + name + "-data" }

// BuildAddonRunArgs returns the full `docker run` argument list for one
// add-on (including "run"). Ports were validated; unparseable entries are
// skipped rather than crashing the args builder.
func BuildAddonRunArgs(name string, s config.AddonSpec) []string {
	args := []string{
		"run", "-d",
		"--name", AddonContainerName(name),
		"--restart=unless-stopped",
		"--label", SpecHashLabel + "=" + AddonSpecHash(name, s),
		"--label", AddonLabelKey + "=" + name,
		"--network=" + AddonNetwork,
	}
	for _, l := range sortedKeys(s.Ports) {
		ps, err := config.ParsePortSpec(s.Ports[l])
		if err != nil {
			continue
		}
		args = append(args, "-p", ps.IP+":"+ps.Host+":"+ps.Container)
	}
	if s.DataDir != "" {
		args = append(args, "-v", AddonDataVolume(name)+":"+s.DataDir)
	}
	for _, k := range sortedKeys(s.Volumes) {
		args = append(args, "-v", s.Volumes[k])
	}
	for _, k := range sortedKeys(s.Environment) {
		args = append(args, "-e", k+"="+s.Environment[k])
	}
	return append(args, s.Image)
}

// AddonSpecHash fingerprints everything that affects the container. Ingress
// fields are deliberately excluded: a panel tweak must not recreate the
// container (the panel lives in HA's config, not in docker).
func AddonSpecHash(name string, s config.AddonSpec) string {
	h := sha256.New()
	fmt.Fprintf(h, "name=%s\nimage=%s\ndata=%s\n", name, s.Image, s.DataDir)
	for _, k := range sortedKeys(s.Ports) {
		fmt.Fprintf(h, "port:%s=%s\n", k, s.Ports[k])
	}
	for _, k := range sortedKeys(s.Volumes) {
		fmt.Fprintf(h, "vol:%s=%s\n", k, s.Volumes[k])
	}
	for _, k := range sortedKeys(s.Environment) {
		fmt.Fprintf(h, "env:%s=%s\n", k, s.Environment[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/dockerargs/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/dockerargs/ && git add internal/dockerargs/ && git commit -m "feat(dockerargs): add-on run args + spec hash (ingress-independent)"
```

---

### Task 6: Model B — publish HA's `:8123` when it leaves the host network

**Files:**
- Modify: `internal/dockerargs/dockerargs.go:38-69` (`BuildRunArgs`)
- Test: `internal/dockerargs/dockerargs_test.go`

**Interfaces:**
- Consumes: `AddonNetwork` (Task 5).
- Produces: no new symbols — `BuildRunArgs` behavior only.

- [ ] **Step 1: Write the failing test**

Append to `internal/dockerargs/dockerargs_test.go`:

```go
func TestBuildRunArgsPublishesHAOnBridge(t *testing.T) {
	// Model B: HA on the ha-addons bridge loses the host netns, so :8123
	// must be explicitly published or HA becomes unreachable from the LAN.
	got := join(BuildRunArgs(cfg(`{"docker":{"network":"ha-addons"}}`)))
	if !strings.Contains(got, "-p 8123:8123") {
		t.Errorf("missing -p 8123:8123 on the %s network\n got: %s", AddonNetwork, got)
	}
	if def := join(BuildRunArgs(cfg(`{}`))); strings.Contains(def, "8123") {
		t.Errorf("host network must not publish ports: %s", def)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dockerargs/ -run TestBuildRunArgsPublishesHAOnBridge -v`
Expected: FAIL — missing `-p 8123:8123`.

- [ ] **Step 3: Write minimal implementation**

In `BuildRunArgs` (`internal/dockerargs/dockerargs.go`), right after the `"--network=" + c.Network` element is added:

```go
	// On the add-ons bridge HA loses the host netns; without this HA's own
	// UI would be unreachable from the LAN.
	if c.Network == AddonNetwork {
		args = append(args, "-p", "8123:8123")
	}
```

(No `SpecHash` change needed: `Network` is already hashed, so flipping models recreates the container.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/dockerargs/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/dockerargs/ && git add internal/dockerargs/ && git commit -m "feat(dockerargs): publish :8123 when HA joins the ha-addons bridge"
```

---

### Task 7: docker client — `EnsureNetwork` + `ListByLabel`

**Files:**
- Modify: `internal/docker/docker.go`
- Test: `internal/docker/docker_test.go`

**Interfaces:**
- Consumes: existing `Runner`.
- Produces: `func (c *Client) EnsureNetwork(name string) error`; `func (c *Client) ListByLabel(label string) ([]string, error)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/docker/docker_test.go`:

```go
func TestEnsureNetwork(t *testing.T) {
	// Present: inspect succeeds → no create call.
	f := &fakeRunner{outputs: map[string]string{"network inspect ha-addons": "[{}]"}}
	c := NewWithRunner(f)
	if err := c.EnsureNetwork("ha-addons"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Errorf("existing network must not be created again: %v", f.calls)
	}
	// Absent: inspect fails → network create streamed.
	f = &fakeRunner{errs: map[string]error{"network inspect ha-addons": errors.New("not found")}}
	c = NewWithRunner(f)
	if err := c.EnsureNetwork("ha-addons"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || strings.Join(f.calls[1], " ") != "network create ha-addons" {
		t.Errorf("expected network create, got %v", f.calls)
	}
}

func TestListByLabel(t *testing.T) {
	f := &fakeRunner{outputs: map[string]string{
		"ps -a --filter label=io.giaever.home-assistant.addon --format {{.Names}}": "ha-addon-a\nha-addon-b",
	}}
	c := NewWithRunner(f)
	got, err := c.ListByLabel("io.giaever.home-assistant.addon")
	if err != nil || len(got) != 2 || got[0] != "ha-addon-a" || got[1] != "ha-addon-b" {
		t.Errorf("ListByLabel = %v, %v", got, err)
	}
	// No matches → empty output → nil slice, no error.
	c = NewWithRunner(&fakeRunner{})
	if got, err := c.ListByLabel("io.giaever.home-assistant.addon"); err != nil || got != nil {
		t.Errorf("empty: %v, %v", got, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/docker/ -run 'TestEnsureNetwork|TestListByLabel' -v`
Expected: FAIL — `c.EnsureNetwork undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/docker/docker.go`:

```go
// EnsureNetwork creates the named user-defined bridge if it does not exist.
// Idempotent: an existing network is left untouched.
func (c *Client) EnsureNetwork(name string) error {
	if _, err := c.r.Output("network", "inspect", name); err == nil {
		return nil
	}
	return c.r.Stream("network", "create", name)
}

// ListByLabel returns the names of all containers (running or not) carrying
// the given label key — used to find add-on containers whose config is gone.
func (c *Client) ListByLabel(label string) ([]string, error) {
	out, err := c.r.Output("ps", "-a", "--filter", "label="+label, "--format", "{{.Names}}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/docker/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/docker/ && git add internal/docker/ && git commit -m "feat(docker): EnsureNetwork + ListByLabel for add-on lifecycle"
```

---

### Task 8: Reconcile refactor — `ContainerSpec` (no behavior change)

**Files:**
- Modify: `internal/reconcile/reconcile.go`
- Test: `internal/reconcile/reconcile_test.go` (adapt existing tests)

**Interfaces:**
- Consumes: `dockerargs.SpecHashLabel` (unchanged).
- Produces: `type ContainerSpec struct { Name, Image, WantHash string; RunArgs []string }`; `func Reconcile(d Docker, s ContainerSpec, force bool) (Action, error)`. **The old `Reconcile(d, config.Config, force)` signature is gone** — Task 12 fixes the `cmd/launcher` call site; until then only this package must compile/pass.

- [ ] **Step 1: Adapt the tests to the new signature (this is the failing test)**

Rewrite `internal/reconcile/reconcile_test.go` — the fake stays, config/dockerargs imports drop, and each test builds a `ContainerSpec` directly:

```go
// internal/reconcile/reconcile_test.go
package reconcile

import "testing"

type fakeDocker struct {
	exists, running            bool
	hash                       string
	ran, removed, strt, pulled bool
}

func (f *fakeDocker) Exists(string) (bool, error)             { return f.exists, nil }
func (f *fakeDocker) Running(string) (bool, error)            { return f.running, nil }
func (f *fakeDocker) SpecHash(string, string) (string, error) { return f.hash, nil }
func (f *fakeDocker) Remove(string) error                     { f.removed = true; return nil }
func (f *fakeDocker) Run([]string) error                      { f.ran = true; return nil }
func (f *fakeDocker) Start(string) error                      { f.strt = true; return nil }
func (f *fakeDocker) Pull(string) error                       { f.pulled = true; return nil }
func (f *fakeDocker) ListByLabel(string) ([]string, error)    { return nil, nil }

func spec() ContainerSpec {
	return ContainerSpec{Name: "homeassistant", Image: "img:tag", WantHash: "want", RunArgs: []string{"run"}}
}

func TestCreatesWhenMissing(t *testing.T) {
	f := &fakeDocker{exists: false}
	if act, _ := Reconcile(f, spec(), false); act != ActionCreated || !f.ran || !f.pulled {
		t.Fatalf("act=%v ran=%v pulled=%v", act, f.ran, f.pulled)
	}
}

func TestRecreatesOnHashMismatch(t *testing.T) {
	f := &fakeDocker{exists: true, hash: "stale"}
	if act, _ := Reconcile(f, spec(), false); act != ActionRecreated || !f.removed || !f.ran || !f.pulled {
		t.Fatalf("act=%v removed=%v ran=%v pulled=%v", act, f.removed, f.ran, f.pulled)
	}
}

func TestNoOpWhenMatchingAndRunning(t *testing.T) {
	f := &fakeDocker{exists: true, running: true, hash: "want"}
	if act, _ := Reconcile(f, spec(), false); act != ActionNone || f.pulled {
		t.Fatalf("act=%v pulled=%v", act, f.pulled)
	}
}

func TestStartsWhenStopped(t *testing.T) {
	f := &fakeDocker{exists: true, running: false, hash: "want"}
	if act, _ := Reconcile(f, spec(), false); act != ActionStarted || !f.strt || f.pulled {
		t.Fatalf("act=%v strt=%v pulled=%v", act, f.strt, f.pulled)
	}
}

func TestForceRecreates(t *testing.T) {
	f := &fakeDocker{exists: true, running: true, hash: "want"}
	if act, _ := Reconcile(f, spec(), true); act != ActionRecreated {
		t.Fatalf("act=%v", act)
	}
}
```

(The `ListByLabel` fake method is added now so Task 9 only extends behavior, not the fake's shape.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/reconcile/ -v`
Expected: FAIL — `Reconcile` still takes `config.Config`; compile error.

- [ ] **Step 3: Refactor the implementation**

Rewrite `internal/reconcile/reconcile.go`:

```go
// internal/reconcile/reconcile.go
package reconcile

import (
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
	Pull(image string) error
	ListByLabel(label string) ([]string, error)
}

type Action string

const (
	ActionNone      Action = "none"
	ActionStarted   Action = "started"
	ActionCreated   Action = "created"
	ActionRecreated Action = "recreated"
)

// ContainerSpec is the desired state of one launcher-managed container —
// HA Core and add-ons are the same thing at this level.
type ContainerSpec struct {
	Name     string
	Image    string
	WantHash string
	RunArgs  []string
}

// Reconcile ensures the container matches s. When force is true the
// container is always recreated (used after pulling a new image).
func Reconcile(d Docker, s ContainerSpec, force bool) (Action, error) {
	exists, err := d.Exists(s.Name)
	if err != nil {
		return ActionNone, err
	}
	if !exists {
		// Pull explicitly (visible progress + bounded retry) BEFORE `docker run`,
		// rather than relying on run's silent implicit pull, which can wedge for
		// many minutes with no feedback and no retry.
		if err := d.Pull(s.Image); err != nil {
			return ActionNone, err
		}
		if err := d.Run(s.RunArgs); err != nil {
			return ActionNone, err
		}
		return ActionCreated, nil
	}

	have, err := d.SpecHash(s.Name, dockerargs.SpecHashLabel)
	if err != nil {
		return ActionNone, err
	}
	if force || have != s.WantHash {
		// Pull the (possibly new) image before tearing down the running container,
		// so the swap is quick and the old container stays up while it downloads.
		if err := d.Pull(s.Image); err != nil {
			return ActionNone, err
		}
		if err := d.Remove(s.Name); err != nil {
			return ActionNone, err
		}
		if err := d.Run(s.RunArgs); err != nil {
			return ActionNone, err
		}
		return ActionRecreated, nil
	}

	running, err := d.Running(s.Name)
	if err != nil {
		return ActionNone, err
	}
	if !running {
		if err := d.Start(s.Name); err != nil {
			return ActionNone, err
		}
		return ActionStarted, nil
	}
	return ActionNone, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/reconcile/ -v`
Expected: PASS. (`go build ./...` will fail at `cmd/launcher` — expected until Task 12; do NOT try to fix main.go in this task.)

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/reconcile/ && git add internal/reconcile/ && git commit -m "refactor(reconcile): generalize the ladder around ContainerSpec"
```

---

### Task 9: `reconcile.Set` — the container set + orphan removal

**Files:**
- Modify: `internal/reconcile/reconcile.go`
- Test: `internal/reconcile/reconcile_test.go`

**Interfaces:**
- Consumes: `Reconcile`, `Docker.ListByLabel`, `dockerargs.AddonLabelKey`.
- Produces: `type Result struct { Name string; Action Action; Err error }`; `const ActionRemoved Action = "removed"`; `func Set(d Docker, specs []ContainerSpec, force bool) []Result`.

- [ ] **Step 1: Write the failing test**

Append to `internal/reconcile/reconcile_test.go` (extend the fake — replace its struct and the `Remove`/`Run`/`ListByLabel` methods, keeping all other methods as-is):

```go
// Extend fakeDocker for Set tests: record per-name calls and allow failures.
// (Replace the existing struct + Remove/Run/ListByLabel methods with these.)
```

```go
type fakeDocker struct {
	exists, running            bool
	hash                       string
	ran, removed, strt, pulled bool

	labeled      []string          // ListByLabel result
	removedNames []string          // every Remove call, in order
	runErrs      map[string]error  // fail Run when args contain this substring
}

func (f *fakeDocker) Remove(name string) error {
	f.removed = true
	f.removedNames = append(f.removedNames, name)
	return nil
}

func (f *fakeDocker) Run(args []string) error {
	f.ran = true
	for k, err := range f.runErrs {
		for _, a := range args {
			if strings.Contains(a, k) {
				return err
			}
		}
	}
	return nil
}

func (f *fakeDocker) ListByLabel(string) ([]string, error) { return f.labeled, nil }
```

(`import "strings"` and `"errors"` join `"testing"`.) Then the tests:

```go
func TestSetReconcilesAllAndRemovesOrphans(t *testing.T) {
	f := &fakeDocker{exists: false, labeled: []string{"ha-addon-old", "ha-addon-keep"}}
	specs := []ContainerSpec{
		{Name: "homeassistant", Image: "ha", WantHash: "h", RunArgs: []string{"run", "ha"}},
		{Name: "ha-addon-keep", Image: "k", WantHash: "h", RunArgs: []string{"run", "keep"}},
	}
	results := Set(f, specs, false)
	if len(results) != 3 { // 2 reconciled + 1 orphan removed
		t.Fatalf("results = %+v", results)
	}
	if results[0].Name != "homeassistant" || results[0].Action != ActionCreated {
		t.Errorf("HA first: %+v", results[0])
	}
	if results[2].Name != "ha-addon-old" || results[2].Action != ActionRemoved || results[2].Err != nil {
		t.Errorf("orphan: %+v", results[2])
	}
	if len(f.removedNames) != 1 || f.removedNames[0] != "ha-addon-old" {
		t.Errorf("removed %v, want only the orphan", f.removedNames)
	}
}

func TestSetIsolatesFailures(t *testing.T) {
	f := &fakeDocker{exists: false, runErrs: map[string]error{"bad": errors.New("boom")}}
	specs := []ContainerSpec{
		{Name: "ha-addon-bad", Image: "bad", WantHash: "h", RunArgs: []string{"run", "bad"}},
		{Name: "ha-addon-good", Image: "good", WantHash: "h", RunArgs: []string{"run", "good"}},
	}
	results := Set(f, specs, false)
	if results[0].Err == nil {
		t.Error("bad add-on should error")
	}
	if results[1].Err != nil || results[1].Action != ActionCreated {
		t.Errorf("good add-on must still converge: %+v", results[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/reconcile/ -v`
Expected: FAIL — `undefined: Set`, `undefined: ActionRemoved`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/reconcile/reconcile.go`:

```go
// ActionRemoved reports an orphan (an add-on container whose config is gone).
const ActionRemoved Action = "removed"

// Result is one container's reconcile outcome within a Set.
type Result struct {
	Name   string
	Action Action
	Err    error
}

// Set converges every spec in order (callers put HA first), then removes
// labeled add-on containers that are no longer desired. Every spec is
// attempted — one broken add-on cannot block HA or its siblings. Data
// volumes are never touched.
func Set(d Docker, specs []ContainerSpec, force bool) []Result {
	results := make([]Result, 0, len(specs))
	desired := make(map[string]bool, len(specs))
	for _, s := range specs {
		desired[s.Name] = true
		act, err := Reconcile(d, s, force)
		results = append(results, Result{Name: s.Name, Action: act, Err: err})
	}
	existing, err := d.ListByLabel(dockerargs.AddonLabelKey)
	if err != nil {
		return append(results, Result{Name: "addon-cleanup", Action: ActionNone, Err: err})
	}
	for _, name := range existing {
		if desired[name] {
			continue
		}
		if err := d.Remove(name); err != nil {
			results = append(results, Result{Name: name, Action: ActionNone, Err: err})
			continue
		}
		results = append(results, Result{Name: name, Action: ActionRemoved})
	}
	return results
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/reconcile/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/reconcile/ && git add internal/reconcile/ && git commit -m "feat(reconcile): Set — converge the container set, remove orphans, isolate failures"
```

---

### Task 10: Ingress derivation + sync decision (`internal/ingress`)

**Files:**
- Create: `internal/ingress/derive.go`
- Test: `internal/ingress/derive_test.go`

**Interfaces:**
- Consumes: `config.AddonSpec.IngressPortSpec`, `dockerargs.AddonNetwork`, `dockerargs.AddonContainerName`, `config.IngressSpec`.
- Produces: `func DeriveEntries(addons map[string]config.AddonSpec, network string) map[string]config.IngressSpec`; `func MergedEntries(c config.Config) map[string]config.IngressSpec`; `func ShouldSync(current, rendered string, panels int) bool`.

- [ ] **Step 1: Write the failing test**

Create `internal/ingress/derive_test.go`:

```go
package ingress

import (
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

func parse(t *testing.T, in string) config.Config {
	t.Helper()
	c, err := config.Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDeriveEntriesModelA(t *testing.T) {
	c := parse(t, `{"addons":{
		"nodered":{"image":"i","ports":{"ui":"8080:1880"},"ingress":{"icon":"mdi:nodejs"}},
		"quiet":{"image":"i","ports":{"ui":80}}
	}}`)
	got := DeriveEntries(c.Addons, c.Network) // default network = host → model A
	if len(got) != 1 {
		t.Fatalf("only panel-wanting add-ons derive entries: %+v", got)
	}
	e := got["nodered"]
	if e.URL != "http://127.0.0.1:8080" { // host port, loopback
		t.Errorf("URL = %q", e.URL)
	}
	if e.Title != "nodered" { // title defaults to the add-on name
		t.Errorf("Title = %q", e.Title)
	}
	if e.Icon != "mdi:nodejs" || e.WorkMode != "ingress" {
		t.Errorf("entry = %+v", e)
	}
}

func TestDeriveEntriesModelB(t *testing.T) {
	c := parse(t, `{"docker":{"network":"ha-addons"},"addons":{
		"nodered":{"image":"i","ports":{"ui":"8080:1880"},"ingress":{"title":"Node-RED"}}
	}}`)
	e := DeriveEntries(c.Addons, c.Network)["nodered"]
	if e.URL != "http://ha-addon-nodered:1880" { // container name + container port
		t.Errorf("URL = %q", e.URL)
	}
	if e.Title != "Node-RED" {
		t.Errorf("Title = %q", e.Title)
	}
}

func TestMergedEntries(t *testing.T) {
	c := parse(t, `{
		"ingress":{"zwave":{"url":"http://localhost:8091","title":"Z-Wave"}},
		"addons":{"nodered":{"image":"i","ports":{"ui":1880},"ingress":{}}}
	}`)
	got := MergedEntries(c)
	if len(got) != 2 || got["zwave"].URL != "http://localhost:8091" || got["nodered"].URL == "" {
		t.Errorf("merged = %+v", got)
	}
}

func TestShouldSync(t *testing.T) {
	cases := []struct {
		current, rendered string
		panels            int
		want              bool
	}{
		{"", "{}", 0, false},        // nothing wanted, nothing there: fresh install stays untouched
		{"", "nodered:\n", 1, true}, // first panel
		{"nodered:", "nodered:\n", 1, false}, // ReadFile trims; trimmed-equal = no restart
		{"old:", "new:\n", 1, true},          // changed
		{"old:", "{}\n", 0, true},            // last panel removed → write the empty map
	}
	for i, c := range cases {
		if got := ShouldSync(c.current, c.rendered, c.panels); got != c.want {
			t.Errorf("case %d: ShouldSync = %v, want %v", i, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ingress/ -run 'TestDerive|TestMerged|TestShouldSync' -v`
Expected: FAIL — `undefined: DeriveEntries`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/ingress/derive.go`:

```go
package ingress

import (
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
)

// DeriveEntries turns panel-wanting add-ons into ingress entries. The URL
// depends on the networking model: on the ha-addons bridge (model B) HA
// reaches add-ons by container name; on host networking (model A) via the
// loopback-published host port.
func DeriveEntries(addons map[string]config.AddonSpec, network string) map[string]config.IngressSpec {
	entries := map[string]config.IngressSpec{}
	for name, a := range addons {
		if a.Ingress == nil {
			continue
		}
		ps, err := a.IngressPortSpec()
		if err != nil {
			continue // fatal at validate; unreachable after a successful Validate
		}
		url := "http://127.0.0.1:" + ps.Host
		if network == dockerargs.AddonNetwork {
			url = "http://" + dockerargs.AddonContainerName(name) + ":" + ps.Container
		}
		title := a.Ingress.Title
		if title == "" {
			title = name
		}
		entries[name] = config.IngressSpec{
			URL:          url,
			Title:        title,
			Icon:         a.Ingress.Icon,
			WorkMode:     "ingress",
			RequireAdmin: a.Ingress.RequireAdmin,
		}
	}
	return entries
}

// MergedEntries is the full panel map: derived add-on panels plus explicit
// ingress.* entries. Collisions are rejected at validate; if one slips
// through anyway the explicit entry wins (deterministic).
func MergedEntries(c config.Config) map[string]config.IngressSpec {
	m := DeriveEntries(c.Addons, c.Network)
	for k, v := range c.Ingress {
		m[k] = v
	}
	return m
}

// ShouldSync decides whether reconcile's auto-sync must (re)write the ingress
// file and restart HA. current is the file's present content ("" when absent
// — and note docker's ReadFile trims whitespace, hence trimmed comparison).
// A fresh install with no panels must stay untouched: no write, no include
// line, no restart.
func ShouldSync(current, rendered string, panels int) bool {
	if panels == 0 && strings.TrimSpace(current) == "" {
		return false
	}
	return strings.TrimSpace(current) != strings.TrimSpace(rendered)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ingress/ -v`
Expected: PASS (existing Render/EnsureInclude tests included).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ingress/ && git add internal/ingress/ && git commit -m "feat(ingress): derive add-on panels + change-driven sync decision"
```

---

### Task 11: `buildContainerSpecs` (cmd/launcher)

**Files:**
- Create: `cmd/launcher/specs.go`
- Test: `cmd/launcher/specs_test.go`

**Interfaces:**
- Consumes: `dockerargs.{ContainerName, ImageRef, SpecHash, BuildRunArgs, AddonContainerName, AddonSpecHash, BuildAddonRunArgs}`, `reconcile.ContainerSpec`.
- Produces: `func buildContainerSpecs(cfg config.Config) []reconcile.ContainerSpec` — used by Task 12.

- [ ] **Step 1: Write the failing test**

Create `cmd/launcher/specs_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

func TestBuildContainerSpecs(t *testing.T) {
	c, err := config.Parse([]byte(`{"addons":{
		"zeta":{"image":"z:1","ports":{"ui":80}},
		"alpha":{"image":"a:1","ports":{"ui":81}}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	specs := buildContainerSpecs(c)
	if len(specs) != 3 {
		t.Fatalf("specs = %d, want 3", len(specs))
	}
	if specs[0].Name != "homeassistant" {
		t.Errorf("HA must come first, got %q", specs[0].Name)
	}
	if specs[1].Name != "ha-addon-alpha" || specs[2].Name != "ha-addon-zeta" {
		t.Errorf("add-ons must be name-sorted: %q, %q", specs[1].Name, specs[2].Name)
	}
	if specs[1].Image != "a:1" || specs[1].WantHash == "" {
		t.Errorf("addon spec incomplete: %+v", specs[1])
	}
	if !strings.Contains(strings.Join(specs[1].RunArgs, " "), "--name ha-addon-alpha") {
		t.Errorf("addon args wrong: %v", specs[1].RunArgs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/launcher/ -run TestBuildContainerSpecs -v`
Expected: FAIL — `undefined: buildContainerSpecs` (plus the pre-existing Task-8 breakage of main.go — that is fixed next task; if the package does not compile, that IS the failing state).

- [ ] **Step 3: Write minimal implementation**

Create `cmd/launcher/specs.go`:

```go
package main

import (
	"sort"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
	"github.com/giaever-online-iot/home-assistant/internal/reconcile"
)

// buildContainerSpecs is the desired container set: HA first (the core is
// never hostage to an add-on), then add-ons sorted by name for deterministic
// reconcile order and output.
func buildContainerSpecs(cfg config.Config) []reconcile.ContainerSpec {
	specs := []reconcile.ContainerSpec{{
		Name:     dockerargs.ContainerName,
		Image:    dockerargs.ImageRef(cfg),
		WantHash: dockerargs.SpecHash(cfg),
		RunArgs:  dockerargs.BuildRunArgs(cfg),
	}}
	names := make([]string, 0, len(cfg.Addons))
	for n := range cfg.Addons {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		a := cfg.Addons[n]
		specs = append(specs, reconcile.ContainerSpec{
			Name:     dockerargs.AddonContainerName(n),
			Image:    a.Image,
			WantHash: dockerargs.AddonSpecHash(n, a),
			RunArgs:  dockerargs.BuildAddonRunArgs(n, a),
		})
	}
	return specs
}
```

Note: `cmd/launcher` still won't fully compile until Task 12 rewrites `applyReconcile`. If needed to run this task's test in isolation, apply Task 12's `applyReconcile` change first — otherwise fold this task's commit into Task 12's (single commit is acceptable here).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/launcher/ -run TestBuildContainerSpecs -v` (after Task 12 if the package didn't compile).
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd/launcher/ && git add cmd/launcher/specs.go cmd/launcher/specs_test.go && git commit -m "feat(launcher): desired container set (HA first, add-ons sorted)"
```

---

### Task 12: Wire main — `addons` namespace, `EnsureNetwork`, `reconcile.Set`

**Files:**
- Modify: `cmd/launcher/main.go` (`loadConfig:37`, `applyReconcile:252-266`)

**Interfaces:**
- Consumes: `buildContainerSpecs` (Task 11), `reconcile.Set` (Task 9), `cli.EnsureNetwork` (Task 7), `dockerargs.AddonNetwork` (Task 5), `applyIngress` (Task 13 — see note).
- Produces: a compiling `cmd/launcher` with per-container reconcile output.

- [ ] **Step 1: Update `loadConfig`**

In `cmd/launcher/main.go:37`, extend the namespace list:

```go
	for _, ns := range []string{"image", "docker", "ingress", "addons"} {
```

- [ ] **Step 2: Rewrite `applyReconcile`**

Replace the whole function (`cmd/launcher/main.go:252-266`); add `"errors"` to main.go's imports:

```go
func applyReconcile(cli *docker.Client, cfg config.Config, force bool) error {
	warnings, err := cfg.Validate()
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		return err
	}
	// The bridge must exist before anything that joins it — add-ons always,
	// HA too under model B (docker.network=ha-addons).
	if len(cfg.Addons) > 0 || cfg.Network == dockerargs.AddonNetwork {
		if err := cli.EnsureNetwork(dockerargs.AddonNetwork); err != nil {
			return fmt.Errorf("ensuring the %s network: %w", dockerargs.AddonNetwork, err)
		}
	}
	var errs []error
	for _, r := range reconcile.Set(cli, buildContainerSpecs(cfg), force) {
		if r.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.Name, r.Err))
			fmt.Fprintf(os.Stderr, "reconcile %s: error: %v\n", r.Name, r.Err)
			continue
		}
		fmt.Printf("reconcile %s: %s\n", r.Name, r.Action)
	}
	// Panels are cosmetic, containers are the substance: ingress problems
	// must never fail (or flap) the reconcile.
	if err := applyIngress(cli, cfg, false); err != nil {
		fmt.Fprintln(os.Stderr, "warning: ingress sync:", err)
	}
	return errors.Join(errs...)
}
```

**Note on ordering with Task 13:** `applyIngress` is defined there. If executing this task before Task 13, add a temporary stub in `cmd/launcher/ingress.go` so the package compiles — `func applyIngress(cli *docker.Client, cfg config.Config, force bool) error { return nil }` — Task 13 replaces it. (Do not skip the stub: `go build ./...` must be green at every commit from here on.)

- [ ] **Step 3: Build and run all tests**

Run: `go build ./... && go test ./...`
Expected: everything compiles; all package tests PASS (including Task 11's).

- [ ] **Step 4: Commit**

```bash
gofmt -w cmd/launcher/ && git add cmd/launcher/ && git commit -m "feat(launcher): reconcile the full container set (addons namespace + bridge)"
```

---

### Task 13: `applyIngress` — auto-sync, manual sync, missing-integration hint

**Files:**
- Modify: `cmd/launcher/ingress.go`

**Interfaces:**
- Consumes: `ingress.{MergedEntries, Render, ShouldSync, EnsureInclude, IncludeFile}`, `cli.{ReadFile, WriteFile, ExecCheck, Restart}`, `preflightContainer`.
- Produces: `func applyIngress(cli *docker.Client, cfg config.Config, force bool) error` (replaces Task 12's stub); reworked `runIngress`.

- [ ] **Step 1: Rewrite `cmd/launcher/ingress.go`**

Replace the file's body below the consts (keep package, imports — the const block stays):

```go
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
```

- [ ] **Step 2: Build and run all tests**

Run: `go build ./... && go test ./...`
Expected: all PASS. The decision logic itself (`ShouldSync`, `MergedEntries`, `DeriveEntries`, `EnsureInclude`, `Render`) is already unit-tested in `internal/ingress`; this file is thin I/O glue over tested parts.

- [ ] **Step 3: Commit**

```bash
gofmt -w cmd/launcher/ && git add cmd/launcher/ingress.go && git commit -m "feat(launcher): change-driven ingress auto-sync + merged manual sync"
```

---

### Task 14: Docs — snapcraft description + README

**Files:**
- Modify: `snap/snapcraft.yaml:5-30` (description), `README.md`

- [ ] **Step 1: Extend the snapcraft description**

In `snap/snapcraft.yaml`, append to the `description:` block (after the existing ingress example):

```yaml
  Run add-on containers (network-only) beside Home Assistant, with an
  optional sidebar panel (requires `home-assistant.install hass-ingress`):

    snap set home-assistant \
      addons.nodered.image=nodered/node-red:latest \
      addons.nodered.ports.ui=1880 \
      addons.nodered.data-dir=/data \
      addons.nodered.ingress.title="Node-RED"

  Ports bind to 127.0.0.1 by default (reachable through the sidebar only);
  publish LAN-wide with an explicit ip, e.g.
  addons.mqtt.ports.broker=0.0.0.0:1883:1883. Unsetting an add-on removes
  its container but keeps its data volume (ha-addon-<name>-data).
```

- [ ] **Step 2: Add a README section**

Add an "Add-ons" section to `README.md` mirroring the description above, plus these notes: uppercase environment names need the JSON-document form (`sudo snap set home-assistant addons.x.environment='{"TZ":"Europe/Oslo"}'`); `docker.network=ha-addons` opts HA onto the bridge (panels switch to container-name URLs, `:8123` is auto-published, discovery/mDNS degrade); `devices.*` is reserved for a later release; add-on volumes are not included in `backup`/`rollback` snapshots.

- [ ] **Step 3: Validate + commit**

Run: `go build ./... && go test ./...` (unchanged code — sanity only). If `snapcraft` is available, `snapcraft lint` is a bonus, not required.

```bash
git add snap/snapcraft.yaml README.md && git commit -m "docs: add-on containers usage (ports, persistence, model B)"
```

---

### Task 15: Full verification + PR

- [ ] **Step 1: Full local gate**

Run each; all must be clean:

```bash
gofmt -l .            # expected: no output
go vet ./...          # expected: no output
go test ./...         # expected: ok for every package
go build ./...        # expected: silence
```

- [ ] **Step 2: Push branch + open PR**

```bash
git push -u origin feat/addons
gh pr create --title "feat(addons): launcher-managed add-on containers + ingress (phase 3)" --body "Implements docs/superpowers/specs/2026-07-09-addons-phase3-design.md — addons.* schema, ha-addons bridge (model A default/model B opt-in), shared reconcile ladder with orphan removal, data-dir persistence, ingress auto-sync. On-device validation checklist in the PR description."
```

Include in the PR body the on-device checklist below (it is completed on the Pi after the PR build, not by the implementing agent).

- [ ] **Step 3: On-device validation checklist (manual — requires the Pi; do not attempt in CI)**

1. **Node-RED end-to-end:** `sudo snap set home-assistant addons.nodered.image=nodered/node-red:latest addons.nodered.ports.ui=1880 addons.nodered.data-dir=/data addons.nodered.ingress.title=Node-RED` → container `ha-addon-nodered` up on the `ha-addons` bridge → Node-RED appears in the HA sidebar with no further command.
2. **LAN opt-in:** Mosquitto with `addons.mqtt.ports.broker=0.0.0.0:1883:1883` reachable from another LAN host; the Node-RED port (loopback) is NOT.
3. **Removal:** `sudo snap unset home-assistant addons.nodered` → container removed, `docker volume ls` still shows `ha-addon-nodered-data`; re-adding the add-on restores flows.
4. **Model B smoke test:** `sudo snap set home-assistant docker.network=ha-addons` → HA recreated on the bridge, still reachable at `:8123`; panel URL flips to the container-name form; revert with `sudo snap set home-assistant docker.network=host`.

---

## Plan Self-Review (done at write time)

- **Spec coverage:** schema+validation → Tasks 1–4; networking/bridge/model B → Tasks 5–7, 12; shared ladder + Set + orphans + isolation → Tasks 8–9; derivation/auto-sync/hint → Tasks 10, 13; command semantics (`update` force = `applyReconcile(force=true)` now force-reconciles the whole set automatically; `backup`/`rollback` untouched) → no extra code needed, covered by Task 12; docs → Task 14; on-device → Task 15.
- **Type consistency check:** `ContainerSpec{Name, Image, WantHash, RunArgs}` used identically in Tasks 8, 9, 11; `PortSpec{IP, Host, Container}` in 1, 3, 5, 10; `Result{Name, Action, Err}` in 9, 12; `applyIngress(cli, cfg, force)` in 12, 13.
- **Known coupling:** Tasks 8→12 leave `cmd/launcher` temporarily uncompilable (flagged in both tasks; Task 12's stub note covers executing 12 before 13).
