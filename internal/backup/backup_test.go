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
