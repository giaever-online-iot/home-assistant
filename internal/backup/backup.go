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
