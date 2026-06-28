package docker

import (
	"errors"
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
	StreamIn(stdin string, args ...string) error
	Check(args ...string) (bool, error)
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

// StreamIn runs the command feeding `stdin` to its standard input (used to pipe
// file content into `docker exec -i … sh -c 'cat > …'`).
func (r execRunner) StreamIn(stdin string, args ...string) error {
	cmd := exec.Command(r.bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// Check runs the command for its exit status: (true,nil) on exit 0, (false,nil)
// when it ran but exited non-zero, and an error only when docker couldn't run.
func (r execRunner) Check(args ...string) (bool, error) {
	if err := exec.Command(r.bin, args...).Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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

// Restart streams `docker restart <name>` — used by `install` to reload Home
// Assistant so a freshly-installed integration is picked up.
func (c *Client) Restart(name string) error { return c.r.Stream("restart", name) }

// ExecCheck runs `docker exec <name> <cmd…>` for its exit status (e.g.
// `test -d <path>`): (true,nil) on exit 0, (false,nil) on a non-zero exit, and
// an error only when docker itself couldn't be run.
func (c *Client) ExecCheck(name string, cmd ...string) (bool, error) {
	return c.r.Check(append([]string{"exec", name}, cmd...)...)
}

// WriteFile writes content to path inside the running container by piping it
// through `docker exec -i … sh -c 'cat > path'` — used to land snap-managed
// config files (e.g. snap-ingress.yaml) into the /config volume.
func (c *Client) WriteFile(name, path, content string) error {
	return c.r.StreamIn(content, "exec", "-i", name, "sh", "-c", "cat > "+path)
}

// ReadFile returns the content of path inside the container, or "" with no error
// when the file is absent/unreadable.
func (c *Client) ReadFile(name, path string) (string, error) {
	out, err := c.r.Output("exec", name, "cat", path)
	if err != nil {
		return "", nil
	}
	return out, nil
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
