package docker

import (
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	outputs    map[string]string
	errs       map[string]error
	checks     map[string]bool // Check exit-0 results by joined-args key
	calls      [][]string
	streamErrs []error // returned in order for successive Stream calls
	lastStdin  string  // stdin from the most recent StreamIn call
}

func (f *fakeRunner) Output(args ...string) (string, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, args)
	return f.outputs[key], f.errs[key]
}
func (f *fakeRunner) Stream(args ...string) error {
	f.calls = append(f.calls, args)
	if len(f.streamErrs) > 0 {
		err := f.streamErrs[0]
		f.streamErrs = f.streamErrs[1:]
		return err
	}
	return nil
}
func (f *fakeRunner) Check(args ...string) (bool, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, args)
	return f.checks[key], f.errs[key]
}
func (f *fakeRunner) StreamIn(stdin string, args ...string) error {
	f.calls = append(f.calls, args)
	f.lastStdin = stdin
	return nil
}

func TestExistsAndRunning(t *testing.T) {
	f := &fakeRunner{outputs: map[string]string{
		"ps -a --filter name=^/homeassistant$ --format {{.Names}}": "homeassistant",
		"inspect -f {{.State.Running}} homeassistant":              "true",
	}}
	c := NewWithRunner(f)
	if ok, _ := c.Exists("homeassistant"); !ok {
		t.Error("Exists should be true")
	}
	if ok, _ := c.Running("homeassistant"); !ok {
		t.Error("Running should be true")
	}
}

func TestImageDigest(t *testing.T) {
	f := &fakeRunner{outputs: map[string]string{
		"inspect -f {{.Image}} homeassistant":                                "sha256:imgid",
		"inspect -f {{range .RepoDigests}}{{println .}}{{end}} sha256:imgid": "ghcr.io/home-assistant/home-assistant@sha256:deadbeef\n",
	}}
	c := NewWithRunner(f)
	got, _ := c.ImageDigest("homeassistant")
	if got != "sha256:deadbeef" {
		t.Errorf("ImageDigest = %q, want sha256:deadbeef", got)
	}
}

func TestRunAndCapturePassThrough(t *testing.T) {
	f := &fakeRunner{outputs: map[string]string{"x y": "out"}}
	c := NewWithRunner(f)
	if err := c.Run([]string{"run", "-d"}); err != nil {
		t.Fatal(err)
	}
	if out, _ := c.Capture([]string{"x", "y"}); out != "out" {
		t.Errorf("Capture = %q", out)
	}
}

func TestPullRetrySucceedsAfterTransientFailures(t *testing.T) {
	f := &fakeRunner{streamErrs: []error{errors.New("boom"), errors.New("boom")}} // fail twice, then succeed
	c := NewWithRunner(f)
	if err := c.pull("img", 3, 0); err != nil {
		t.Fatalf("pull should succeed on the 3rd attempt: %v", err)
	}
	if len(f.calls) != 3 {
		t.Errorf("expected 3 pull attempts, got %d", len(f.calls))
	}
}

func TestPullRetryFailsAfterAllAttempts(t *testing.T) {
	f := &fakeRunner{streamErrs: []error{errors.New("a"), errors.New("b"), errors.New("c")}}
	c := NewWithRunner(f)
	if err := c.pull("img", 3, 0); err == nil {
		t.Fatal("pull should fail after exhausting all attempts")
	}
	if len(f.calls) != 3 {
		t.Errorf("expected 3 pull attempts, got %d", len(f.calls))
	}
}

func TestImageExists(t *testing.T) {
	f := &fakeRunner{
		outputs: map[string]string{"image inspect present:tag": "[{}]"},
		errs:    map[string]error{"image inspect absent:tag": errors.New("No such image")},
	}
	c := NewWithRunner(f)
	if ok, _ := c.ImageExists("present:tag"); !ok {
		t.Error("present image should report exists")
	}
	if ok, _ := c.ImageExists("absent:tag"); ok {
		t.Error("absent image should report not-exists")
	}
}

func TestRestart(t *testing.T) {
	f := &fakeRunner{}
	c := NewWithRunner(f)
	if err := c.Restart("homeassistant"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0][0] != "restart" || f.calls[0][1] != "homeassistant" {
		t.Errorf("Restart called %v", f.calls)
	}
}

func TestExecCheck(t *testing.T) {
	f := &fakeRunner{
		checks: map[string]bool{"exec homeassistant test -d /config/custom_components/hacs": true},
		errs:   map[string]error{"exec homeassistant boom": errors.New("cannot run docker")},
	}
	c := NewWithRunner(f)
	if ok, err := c.ExecCheck("homeassistant", "test", "-d", "/config/custom_components/hacs"); !ok || err != nil {
		t.Errorf("present path: ok=%v err=%v, want true,nil", ok, err)
	}
	if ok, err := c.ExecCheck("homeassistant", "test", "-d", "/nope"); ok || err != nil {
		t.Errorf("missing path: ok=%v err=%v, want false,nil", ok, err)
	}
	if _, err := c.ExecCheck("homeassistant", "boom"); err == nil {
		t.Error("docker invocation failure should return an error")
	}
}

func TestWriteFile(t *testing.T) {
	f := &fakeRunner{}
	c := NewWithRunner(f)
	if err := c.WriteFile("homeassistant", "/config/snap-ingress.yaml", "body\n"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || strings.Join(f.calls[0], " ") != "exec -i homeassistant sh -c cat > /config/snap-ingress.yaml" {
		t.Errorf("WriteFile args = %v", f.calls)
	}
	if f.lastStdin != "body\n" {
		t.Errorf("WriteFile stdin = %q, want %q", f.lastStdin, "body\n")
	}
}

func TestReadFile(t *testing.T) {
	f := &fakeRunner{
		outputs: map[string]string{"exec homeassistant cat /config/configuration.yaml": "default_config:"},
		errs:    map[string]error{"exec homeassistant cat /config/missing": errors.New("No such file")},
	}
	c := NewWithRunner(f)
	if out, err := c.ReadFile("homeassistant", "/config/configuration.yaml"); out != "default_config:" || err != nil {
		t.Errorf("present: %q,%v", out, err)
	}
	if out, err := c.ReadFile("homeassistant", "/config/missing"); out != "" || err != nil {
		t.Errorf("absent: %q,%v want '',nil", out, err)
	}
}

func TestEnsureNetwork(t *testing.T) {
	// Present: inspect succeeds → no create call.
	f := &fakeRunner{outputs: map[string]string{"network inspect ha-addons": "[{}]"}}
	c := NewWithRunner(f)
	if err := c.EnsureNetwork("ha-addons"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Errorf("existing network must not be created again: %v", f.calls)
	}
	// Absent: inspect fails → network create streamed.
	f = &fakeRunner{errs: map[string]error{"network inspect ha-addons": errors.New("not found")}}
	c = NewWithRunner(f)
	if err := c.EnsureNetwork("ha-addons"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || strings.Join(f.calls[1], " ") != "network create ha-addons" {
		t.Errorf("expected network create, got %v", f.calls)
	}
}

func TestListByLabel(t *testing.T) {
	f := &fakeRunner{outputs: map[string]string{
		"ps -a --filter label=io.giaever.home-assistant.addon --format {{.Names}}": "ha-addon-a\nha-addon-b",
	}}
	c := NewWithRunner(f)
	got, err := c.ListByLabel("io.giaever.home-assistant.addon")
	if err != nil || len(got) != 2 || got[0] != "ha-addon-a" || got[1] != "ha-addon-b" {
		t.Errorf("ListByLabel = %v, %v", got, err)
	}
	// No matches → empty output → nil slice, no error.
	c = NewWithRunner(&fakeRunner{})
	if got, err := c.ListByLabel("io.giaever.home-assistant.addon"); err != nil || got != nil {
		t.Errorf("empty: %v, %v", got, err)
	}
}
