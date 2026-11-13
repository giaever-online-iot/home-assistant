package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/giaever-online-iot/home-assistant/internal/docker"
	"github.com/giaever-online-iot/home-assistant/internal/dockerargs"
)

// parseHAVersion reads the year and month from a Home Assistant version, as
// written to /config/.HA_VERSION ("2026.11.1", "2026.12.0b1"). Fields 0 and 1
// must be non-empty ASCII digits; anything after field 1 is ignored.
func parseHAVersion(raw string) (year, month int, ok bool) {
	fields := strings.Split(strings.TrimSpace(raw), ".")
	if len(fields) < 2 || !allDigits(fields[0]) || !allDigits(fields[1]) {
		return 0, 0, false
	}
	year, errY := strconv.Atoi(fields[0])
	month, errM := strconv.Atoi(fields[1])
	if errY != nil || errM != nil { // overflow
		return 0, 0, false
	}
	return year, month, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// HACS moved into Home Assistant core as the Marketplace in 2026.11; on that
// version and later get.hacs.xyz refuses and the loader blocks the hacs domain.
const (
	marketplaceYear  = 2026
	marketplaceMonth = 11
)

func marketplaceBuiltIn(year, month int) bool {
	return year > marketplaceYear || (year == marketplaceYear && month >= marketplaceMonth)
}

const haVersionPath = "/config/.HA_VERSION"

const hacsFrozenNotice = "Note: HACS is frozen: it moved into Home Assistant core (the Marketplace) in 2026.11 and gets no further releases. Upgrading Home Assistant migrates it automatically."

const hacsLegacyByHand = `On an older Home Assistant, run the legacy installer by hand: docker exec homeassistant bash -c "wget -O - https://get.hacs.xyz | bash"`

// marketplaceGuidance tells the user where HACS went. found is the running
// version; "" (unknown) leaves out the "(found …)" clause.
func marketplaceGuidance(found string) string {
	clause := ""
	if found != "" {
		clause = " (found " + found + ")"
	}
	return "HACS is built into Home Assistant 2026.11 and later as the Marketplace" + clause + "; nothing to install.\n" +
		"  1. Home Assistant → Settings → Marketplace (administrator accounts only).\n" +
		"  2. Accept the one-time risk notice. Until you do, nothing can be installed or updated.\n" +
		"An existing HACS install is migrated to the Marketplace automatically on the first start of 2026.11 or later."
}

// hacsGate reads the running HA's version from the config volume (the same
// file get.hacs.xyz checks). 2026.11+ or unknown: guidance, do not proceed
// (D1, D3). Older: proceed with the frozen notice.
func hacsGate(c *docker.Client) (bool, string, error) {
	raw, err := c.ReadFile(dockerargs.ContainerName, haVersionPath)
	if err != nil {
		return false, "", err
	}
	v := strings.TrimSpace(raw)
	year, month, ok := parseHAVersion(v)
	if !ok {
		return false, fmt.Sprintf("Could not read Home Assistant's version from %s (got %q); assuming 2026.11 or later.\n", haVersionPath, v) +
			marketplaceGuidance("") + "\n" + hacsLegacyByHand, nil
	}
	if marketplaceBuiltIn(year, month) {
		return false, marketplaceGuidance(v), nil
	}
	return true, hacsFrozenNotice, nil
}
