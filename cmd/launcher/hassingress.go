package main

import (
	"strconv"
	"strings"
)

// parsePluginVersion reads a custom integration's manifest version
// ("1.4.0", "1.4", "1.5.0b1"). There must be 2 or 3 dot-separated fields;
// every field but the last is all ASCII digits, the last starts with at least
// one digit and any non-digit suffix is dropped. A missing third field is 0.
func parsePluginVersion(raw string) (v [3]int, ok bool) {
	fields := strings.Split(strings.TrimSpace(raw), ".")
	if len(fields) < 2 || len(fields) > 3 {
		return [3]int{}, false
	}
	last := len(fields) - 1
	for i, f := range fields {
		if i == last {
			n := 0
			for n < len(f) && f[n] >= '0' && f[n] <= '9' {
				n++
			}
			f = f[:n]
		}
		if !allDigits(f) {
			return [3]int{}, false
		}
		x, err := strconv.Atoi(f)
		if err != nil { // overflow
			return [3]int{}, false
		}
		v[i] = x
	}
	return v, true
}

// comparePluginVersion compares numerically, field by field: -1, 0 or 1.
func comparePluginVersion(a, b [3]int) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}
