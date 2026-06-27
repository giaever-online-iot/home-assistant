package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner executes docker commands; abstracted for testing.
type Runner interface {
	Output(args ...string) (string, error)
	Stream(args ...string) error
}

type execRunner struct{ bin string }

func (r execRunner) Output(args ...string) (string, error) {
	out, err := exec.Command(r.bin, args...).Output()
	return strings.TrimSpace(string(out)), err
}

func (r execRunner) Stream(args ...string) error {
	cmd := exec.Command(r.bin, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run()
}

// Client wraps the docker operations the launcher needs.
type Client struct{ r Runner }

func New(bin string) *Client         { return &Client{r: execRunner{bin: bin}} }
func NewWithRunner(r Runner) *Client { return &Client{r: r} }

// Run streams `docker <args...>` (used for `run …`, `rm`, `pull`, etc.).
func (c *Client) Run(args []string) error { return c.r.Stream(args...) }

// Capture runs `docker <args...>` and returns trimmed stdout.
func (c *Client) Capture(args []string) (string, error) { return c.r.Output(args...) }

func (c *Client) Exists(name string) (bool, error) {
	out, err := c.r.Output("ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return false, err
	}
	return out == name, nil
}

func (c *Client) Running(name string) (bool, error) {
	out, err := c.r.Output("inspect", "-f", "{{.State.Running}}", name)
	if err != nil {
		return false, nil
	}
	return out == "true", nil
}

func (c *Client) SpecHash(name, label string) (string, error) {
	return c.r.Output("inspect", "-f", fmt.Sprintf("{{ index .Config.Labels %q }}", label), name)
}

// ImageDigest returns the repo digest ("sha256:…") of the named container's
// image, or "" if no repo digest is available (e.g. a locally-built image).
func (c *Client) ImageDigest(name string) (string, error) {
	imageID, err := c.r.Output("inspect", "-f", "{{.Image}}", name)
	if err != nil {
		return "", err
	}
	repo, err := c.r.Output("inspect", "-f", "{{range .RepoDigests}}{{println .}}{{end}}", imageID)
	if err != nil {
		return "", nil
	}
	first := strings.TrimSpace(repo)
	if first == "" {
		return "", nil
	}
	first = strings.SplitN(first, "\n", 2)[0]
	if at := strings.Index(first, "@"); at >= 0 {
		return first[at+1:], nil
	}
	return "", nil
}

func (c *Client) Pull(image string) error  { return c.pull(image, pullAttempts, pullBackoff) }
func (c *Client) Remove(name string) error { return c.r.Stream("rm", "-f", name) }
func (c *Client) Start(name string) error  { return c.r.Stream("start", name) }
func (c *Client) Stop(name string) error   { return c.r.Stream("stop", name) }
func (c *Client) FollowLogs(name string) error {
	return c.r.Stream("logs", "-f", "--tail", "100", name)
}
func (c *Client) Exec(name string, cmd ...string) error {
	return c.r.Stream(append([]string{"exec", name}, cmd...)...)
}

const (
	pullAttempts = 3
	pullBackoff  = 3 * time.Second
)

// pull is Pull's testable core: it streams `docker pull` (per-layer progress)
// and retries up to attempts times with backoff between tries, so a transient
// registry/containerd hiccup self-heals instead of leaving the create wedged.
// Already-downloaded layers are cached, so a retry resumes rather than restarts.
func (c *Client) pull(image string, attempts int, backoff time.Duration) error {
	var err error
	for i := 1; i <= attempts; i++ {
		if err = c.r.Stream("pull", image); err == nil {
			return nil
		}
		if i < attempts {
			fmt.Fprintf(os.Stderr, "docker pull %s failed (attempt %d/%d): %v — retrying in %s\n", image, i, attempts, err, backoff)
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("docker pull %s failed after %d attempts: %w", image, attempts, err)
}

// ImageExists reports whether the given image reference is present locally
// (used by the preflight ladder to tell "still downloading" from "not started").
func (c *Client) ImageExists(image string) (bool, error) {
	if _, err := c.r.Output("image", "inspect", image); err != nil {
		return false, nil // `image inspect` fails when the image is absent
	}
	return true, nil
}
