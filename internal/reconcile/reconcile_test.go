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
