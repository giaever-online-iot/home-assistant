package config

import (
	"encoding/json"
	"fmt"
	"strings"
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
