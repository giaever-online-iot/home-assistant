package main

import (
	"strconv"
	"strings"
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
