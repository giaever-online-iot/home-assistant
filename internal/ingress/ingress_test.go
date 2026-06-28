package ingress

import (
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

func TestRenderEmpty(t *testing.T) {
	out := Render(nil)
	if !strings.Contains(out, "{}") {
		t.Errorf("empty render should be an empty mapping, got:\n%s", out)
	}
	if strings.Contains(out, "ingress:") {
		t.Error("rendered body must NOT repeat the `ingress:` key (it is !included as the value)")
	}
}

func TestRenderEntriesSortedAndComplete(t *testing.T) {
	out := Render(map[string]config.IngressSpec{
		"zwave":  {URL: "http://localhost:8091", Title: "Z-Wave JS UI", Icon: "mdi:z-wave", WorkMode: "ingress"},
		"nodered": {URL: "http://localhost:1880", Title: "Node-RED", WorkMode: "iframe", RequireAdmin: true},
	})
	// sorted: nodered before zwave
	if i, j := strings.Index(out, "nodered:"), strings.Index(out, "zwave:"); i < 0 || j < 0 || i > j {
		t.Errorf("entries should be sorted (nodered before zwave):\n%s", out)
	}
	for _, want := range []string{
		`url: "http://localhost:8091"`,
		`title: "Z-Wave JS UI"`,
		`icon: "mdi:z-wave"`,
		`url: "http://localhost:1880"`,
		`work_mode: "iframe"`,
		`require_admin: true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q in:\n%s", want, out)
		}
	}
	// default work_mode "ingress" is omitted; require_admin omitted when false
	if strings.Contains(out, `work_mode: "ingress"`) {
		t.Error("default work_mode should be omitted")
	}
}

func TestEnsureIncludeAppendsWhenMissing(t *testing.T) {
	out, changed, conflict := EnsureInclude("default_config:\n")
	if !changed || conflict {
		t.Fatalf("missing → changed=true conflict=false, got changed=%v conflict=%v", changed, conflict)
	}
	if !strings.Contains(out, "ingress: !include snap-ingress.yaml") {
		t.Errorf("include not appended:\n%s", out)
	}
	if !strings.HasPrefix(out, "default_config:") {
		t.Error("existing content must be preserved")
	}
}

func TestEnsureIncludeNoopWhenPresent(t *testing.T) {
	in := "default_config:\ningress: !include snap-ingress.yaml\n"
	out, changed, conflict := EnsureInclude(in)
	if changed || conflict || out != in {
		t.Errorf("already present → unchanged; got changed=%v conflict=%v", changed, conflict)
	}
}

func TestEnsureIncludeConflictOnForeignIngress(t *testing.T) {
	in := "ingress:\n  foo:\n    url: http://x\n"
	out, changed, conflict := EnsureInclude(in)
	if changed || !conflict || out != in {
		t.Errorf("foreign ingress: → conflict=true, untouched; got changed=%v conflict=%v", changed, conflict)
	}
}
