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
	// On the add-ons bridge HA loses the host netns; without this HA's own
	// UI would be unreachable from the LAN.
	if c.Network == AddonNetwork {
		args = append(args, "-p", "8123:8123")
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
