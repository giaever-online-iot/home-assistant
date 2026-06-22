package docker

import (
	"strings"
	"testing"
)

type fakeRunner struct {
	outputs map[string]string
	calls   [][]string
}

func (f *fakeRunner) Output(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	return f.outputs[strings.Join(args, " ")], nil
}
func (f *fakeRunner) Stream(args ...string) error {
	f.calls = append(f.calls, args)
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
		"inspect -f {{.Image}} homeassistant":                                       "sha256:imgid",
		"inspect -f {{range .RepoDigests}}{{println .}}{{end}} sha256:imgid":         "ghcr.io/home-assistant/home-assistant@sha256:deadbeef\n",
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
