package main

import "testing"

func TestParsePluginVersion(t *testing.T) {
	cases := []struct {
		raw string
		v   [3]int
		ok  bool
	}{
		{"1.4.0", [3]int{1, 4, 0}, true},
		{"1.3.2", [3]int{1, 3, 2}, true},
		{"1.10.0", [3]int{1, 10, 0}, true},
		{"1.4", [3]int{1, 4, 0}, true},
		{"1.4.2", [3]int{1, 4, 2}, true},
		{"1.5.0b1", [3]int{1, 5, 0}, true},
		{" 1.4.0\n", [3]int{1, 4, 0}, true}, // Review Focus 3
		{"", [3]int{}, false},
		{"v1.4.0", [3]int{}, false},
		{"1", [3]int{}, false},
		{"1.x.0", [3]int{}, false},
		{"1.4.0.1", [3]int{}, false},
		{".4.0", [3]int{}, false},
		{"1.4.b1", [3]int{}, false}, // the last field must start with a digit
	}
	for _, tc := range cases {
		v, ok := parsePluginVersion(tc.raw)
		if v != tc.v || ok != tc.ok {
			t.Errorf("parsePluginVersion(%q) = (%v, %v), want (%v, %v)", tc.raw, v, ok, tc.v, tc.ok)
		}
	}
}

func TestComparePluginVersion(t *testing.T) {
	pin := [3]int{1, 4, 0}
	cases := []struct {
		raw  string
		want int // vs 1.4.0
	}{
		{"1.4.0", 0},
		{"1.3.2", -1},
		{"1.10.0", 1}, // numeric, not string (M5)
		{"1.4", 0},
		{"1.4.2", 1},
		{"1.5.0b1", 1},
		{"0.9.9", -1},
		{"2.0.0", 1},
	}
	for _, tc := range cases {
		v, ok := parsePluginVersion(tc.raw)
		if !ok {
			t.Fatalf("parsePluginVersion(%q) not ok", tc.raw)
		}
		if got := comparePluginVersion(v, pin); got != tc.want {
			t.Errorf("comparePluginVersion(%v, %v) = %d, want %d", v, pin, got, tc.want)
		}
	}
}
