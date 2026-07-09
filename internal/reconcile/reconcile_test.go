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
