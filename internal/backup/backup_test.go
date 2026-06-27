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
	// same-prefix ordering: pre-update timestamps must still sort correctly
	ls := "manual-20260601-090000.tar.gz\nmanual-20260601-090000.image\npre-update-20260622-100000.tar.gz\npre-update-20260622-100000.image\n"
	if got := ParseLatest(ls); got != "pre-update-20260622-100000" {
		t.Errorf("ParseLatest same-prefix = %q, want pre-update-20260622-100000", got)
	}
	if got := ParseLatest("nothing\n"); got != "" {
		t.Errorf("ParseLatest empty = %q", got)
	}
}

func TestParseLatestMixedPrefixes(t *testing.T) {
	// regression: pre-update-* sorts after manual-* lexically ('m' < 'p'),
	// so a naive full-name sort would wrongly pick the older pre-update
	// snapshot when a newer manual snapshot exists.
	ls := "pre-update-20260101-000000.tar.gz\npre-update-20260101-000000.image\nmanual-20260622-235959.tar.gz\nmanual-20260622-235959.image\n"
	if got := ParseLatest(ls); got != "manual-20260622-235959" {
		t.Errorf("ParseLatest mixed-prefix = %q, want manual-20260622-235959", got)
	}
}
