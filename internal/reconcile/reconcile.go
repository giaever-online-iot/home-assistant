// internal/reconcile/reconcile.go
package reconcile

import (
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
)

// Docker is the subset of docker operations Reconcile needs (*docker.Client satisfies it).
type Docker interface {
	Exists(name string) (bool, error)
	Running(name string) (bool, error)
	SpecHash(name, label string) (string, error)
	Remove(name string) error
	Run(args []string) error
	Start(name string) error
	Pull(image string) error
	ListByLabel(label string) ([]string, error)
}

type Action string

const (
	ActionNone      Action = "none"
	ActionStarted   Action = "started"
	ActionCreated   Action = "created"
	ActionRecreated Action = "recreated"
)

// ContainerSpec is the desired state of one launcher-managed container —
// HA Core and add-ons are the same thing at this level.
type ContainerSpec struct {
	Name     string
	Image    string
	WantHash string
	RunArgs  []string
}

// Reconcile ensures the container matches s. When force is true the
// container is always recreated (used after pulling a new image).
func Reconcile(d Docker, s ContainerSpec, force bool) (Action, error) {
	exists, err := d.Exists(s.Name)
	if err != nil {
		return ActionNone, err
	}
	if !exists {
		// Pull explicitly (visible progress + bounded retry) BEFORE `docker run`,
		// rather than relying on run's silent implicit pull, which can wedge for
		// many minutes with no feedback and no retry.
		if err := d.Pull(s.Image); err != nil {
			return ActionNone, err
		}
		if err := d.Run(s.RunArgs); err != nil {
			return ActionNone, err
		}
		return ActionCreated, nil
	}

	have, err := d.SpecHash(s.Name, dockerargs.SpecHashLabel)
	if err != nil {
		return ActionNone, err
	}
	if force || have != s.WantHash {
		// Pull the (possibly new) image before tearing down the running container,
		// so the swap is quick and the old container stays up while it downloads.
		if err := d.Pull(s.Image); err != nil {
			return ActionNone, err
		}
		if err := d.Remove(s.Name); err != nil {
			return ActionNone, err
		}
		if err := d.Run(s.RunArgs); err != nil {
			return ActionNone, err
		}
		return ActionRecreated, nil
	}

	running, err := d.Running(s.Name)
	if err != nil {
		return ActionNone, err
	}
	if !running {
		if err := d.Start(s.Name); err != nil {
			return ActionNone, err
		}
		return ActionStarted, nil
	}
	return ActionNone, nil
}
