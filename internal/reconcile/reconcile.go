// internal/reconcile/reconcile.go
package reconcile

import (
	"github.com/giaever-online-iot/home-assistant/internal/config"
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
}

type Action string

const (
	ActionNone      Action = "none"
	ActionStarted   Action = "started"
	ActionCreated   Action = "created"
	ActionRecreated Action = "recreated"
)

// Reconcile ensures the Home Assistant container matches c. When force is true
// the container is always recreated (used after pulling a new image).
func Reconcile(d Docker, c config.Config, force bool) (Action, error) {
	name := dockerargs.ContainerName
	want := dockerargs.SpecHash(c)
	args := dockerargs.BuildRunArgs(c)

	exists, err := d.Exists(name)
	if err != nil {
		return ActionNone, err
	}
	if !exists {
		if err := d.Run(args); err != nil {
			return ActionNone, err
		}
		return ActionCreated, nil
	}

	have, err := d.SpecHash(name, dockerargs.SpecHashLabel)
	if err != nil {
		return ActionNone, err
	}
	if force || have != want {
		if err := d.Remove(name); err != nil {
			return ActionNone, err
		}
		if err := d.Run(args); err != nil {
			return ActionNone, err
		}
		return ActionRecreated, nil
	}

	running, err := d.Running(name)
	if err != nil {
		return ActionNone, err
	}
	if !running {
		if err := d.Start(name); err != nil {
			return ActionNone, err
		}
		return ActionStarted, nil
	}
	return ActionNone, nil
}
