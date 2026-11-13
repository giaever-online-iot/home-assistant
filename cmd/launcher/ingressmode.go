package main

import (
	"fmt"
	"io"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
	"github.com/giaever-online-iot/home-assistant/internal/ingress"
)

// warnIngressMode warns when hass_ingress is not in YAML mode, where it would
// silently ignore the snap's panels (D10). It only reads, and never fails the
// caller: panels are cosmetic (D11).
func warnIngressMode(cli *docker.Client, w io.Writer) {
	storage, _ := cli.ReadFile(dockerargs.ContainerName, ingress.StoragePath)
	fmt.Fprint(w, ingress.ModeWarning(ingress.EntryMode(storage)))
}
