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

// ActionRemoved reports an orphan (an add-on container whose config is gone).
const ActionRemoved Action = "removed"

// Result is one container's reconcile outcome within a Set.
type Result struct {
	Name   string
	Action Action
	Err    error
}

// Set converges every spec in order (callers put HA first), then removes
// labeled add-on containers that are no longer desired. Every spec is
// attempted — one broken add-on cannot block HA or its siblings. Data
// volumes are never touched.
func Set(d Docker, specs []ContainerSpec, force bool) []Result {
	results := make([]Result, 0, len(specs))
	desired := make(map[string]bool, len(specs))
	for _, s := range specs {
		desired[s.Name] = true
		act, err := Reconcile(d, s, force)
		results = append(results, Result{Name: s.Name, Action: act, Err: err})
	}
	existing, err := d.ListByLabel(dockerargs.AddonLabelKey)
	if err != nil {
		return append(results, Result{Name: "addon-cleanup", Action: ActionNone, Err: err})
	}
	for _, name := range existing {
		if desired[name] {
			continue
		}
		if err := d.Remove(name); err != nil {
			results = append(results, Result{Name: name, Action: ActionNone, Err: err})
			continue
		}
		results = append(results, Result{Name: name, Action: ActionRemoved})
	}
	return results
}
