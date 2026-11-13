// internal/reconcile/reconcile_test.go
package reconcile

import (
	"errors"
	"strings"
	"testing"
)

type fakeDocker struct {
	exists, running            bool
	hash                       string
	ran, removed, strt, pulled bool

	labeled      []string         // ListByLabel result
	removedNames []string         // every Remove call, in order
	runErrs      map[string]error // fail Run when args contain this substring
}

func (f *fakeDocker) Exists(string) (bool, error)             { return f.exists, nil }
func (f *fakeDocker) Running(string) (bool, error)            { return f.running, nil }
func (f *fakeDocker) SpecHash(string, string) (string, error) { return f.hash, nil }
func (f *fakeDocker) Start(string) error                      { f.strt = true; return nil }
func (f *fakeDocker) Pull(string) error                       { f.pulled = true; return nil }

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
