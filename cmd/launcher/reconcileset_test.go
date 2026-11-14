package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/ingress"
)

const (
	keyExistsHA  = "ps -a --filter name=^/homeassistant$ --format {{.Names}}"
	keyHashHA    = `inspect -f {{ index .Config.Labels "io.giaever.home-assistant.spec-hash" }} homeassistant`
	keyRemoveHA  = "rm -f -v homeassistant"
	prefixRunHA  = "run -d --name homeassistant "
	keyStartHA   = "start homeassistant"
	keyCatIngres = "exec homeassistant cat " + ingressFilePath
)

// modelBNodeRED moves HA onto the bridge with one add-on panel, so both HA's
// hash and the panel URL change against the model A state the fake holds.
const modelBNodeRED = `{"docker":{"network":"ha-addons"},"addons":{"nodered":{"image":"nodered/node-red:4","ports":{"ui":1880},"ingress":{"title":"Node-RED"}}}}`

func mustParse(t *testing.T, in string) config.Config {
	t.Helper()
	c, err := config.Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// T6 (D4): a network change recreates HA with the ingress files already
// written through the old container, so HA starts once and is never
// restarted.
func TestReconcileSetWritesIngressBeforeSwap(t *testing.T) {
	modelA := mustParse(t, `{"addons":{"nodered":{"image":"nodered/node-red:4","ports":{"ui":1880},"ingress":{"title":"Node-RED"}}}}`)
	f := syncFake(ingress.Render(ingress.MergedEntries(modelA)), "default_config:\n"+includeYAMLLine)
	f.outputs[keyExistsHA] = "homeassistant"
	f.outputs[keyHashHA] = "stale"
	if _, err := reconcileSet(docker.NewWithRunner(f), mustParse(t, modelBNodeRED), false); err != nil {
		t.Fatalf("reconcileSet: %v", err)
	}
	w, rm := indexOfCall(f.calls, keyWriteIngress), indexOfCall(f.calls, keyRemoveHA)
	if w < 0 || rm < 0 || w > rm {
		t.Errorf("snap-ingress.yaml write at %d, rm at %d: the write must come first; calls %q", w, rm, f.calls)
	}
	if n := f.count(keyWriteIngress); n != 1 {
		t.Errorf("snap-ingress.yaml written %d times, want 1 (the hook's)", n)
	}
	if !strings.Contains(f.stdin[keyWriteIngress], `"http://ha-addon-nodered:1880"`) {
		t.Errorf("the hook wrote %q, want the model B panel URL", f.stdin[keyWriteIngress])
	}
	if n := f.count(keyRestart); n != 0 {
		t.Errorf("restart called %d times, want 0", n)
	}
	if n, s := f.count(prefixRunHA), f.count(keyStartHA); n != 1 || s != 0 {
		t.Errorf("HA run %d times and started %d times, want one run and no start", n, s)
	}
}

// T7 (D5): when HA does not exist yet, the files are written after Set and
// HA restarts once, as before.
func TestReconcileSetCreateFallsBackToRestart(t *testing.T) {
	f := syncFake("", "default_config:\n"+includeYAMLLine)
	f.errs[keyCatIngres] = errors.New("exit status 1")
	if _, err := reconcileSet(docker.NewWithRunner(f), mustParse(t, modelBNodeRED), false); err != nil {
		t.Fatalf("reconcileSet: %v", err)
	}
	run, w := indexOfCall(f.calls, prefixRunHA), indexOfCall(f.calls, keyWriteIngress)
	if run < 0 || w < run {
		t.Errorf("HA run at %d, snap-ingress.yaml write at %d: the write must follow the run; calls %q", run, w, f.calls)
	}
	if n := f.count(keyWriteIngress); n != 1 {
		t.Errorf("snap-ingress.yaml written %d times, want 1", n)
	}
	if n := f.count(keyRestart); n != 1 {
		t.Errorf("restart called %d times, want 1 (the D5 fallback)", n)
	}
}

// D4/D5: a stopped HA cannot be written through; the hook says so in one
// note line and leaves the files to the post-Set sync.
func TestPreSwapIngressStoppedHA(t *testing.T) {
	f := syncFake("", "default_config:")
	f.outputs[keyRunning] = "false"
	var buf bytes.Buffer
	preSwapIngress(docker.NewWithRunner(f), mustParse(t, modelBNodeRED), &buf)
	if n := f.count(keyWriteIngress) + f.count(keyWriteConfigYAML); n != 0 {
		t.Errorf("%d writes to a stopped HA, want 0", n)
	}
	if lines := strings.Count(buf.String(), "\n"); lines != 1 || !strings.HasPrefix(buf.String(), "note: ") {
		t.Errorf("output %q, want exactly one note: line", buf.String())
	}
}
