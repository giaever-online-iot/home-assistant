package main

import (
	"reflect"
	"testing"
)

func TestResolveTarget(t *testing.T) {
	r, ok := resolveTarget("hacs")
	if !ok || r.Name != "hacs" || r.Dest != "/config/custom_components/hacs" {
		t.Errorf("resolveTarget(hacs) = %+v, %v", r, ok)
	}
	if _, ok := resolveTarget("nope"); ok {
		t.Error("resolveTarget(nope) should be !ok")
	}
}

func TestInstallTargets(t *testing.T) {
	if got := installTargets(); !reflect.DeepEqual(got, []string{"hacs", "hass-ingress"}) {
		t.Errorf("installTargets() = %v, want [hacs hass-ingress]", got)
	}
}

func TestParseInstallArgs(t *testing.T) {
	cases := []struct {
		args             []string
		target           string
		noRestart, force bool
		wantErr          bool
	}{
		{[]string{"hacs"}, "hacs", false, false, false},
		{[]string{"hacs", "--no-restart"}, "hacs", true, false, false},
		{[]string{"--force", "hacs"}, "hacs", false, true, false},
		{[]string{"hacs", "--no-restart", "--force"}, "hacs", true, true, false},
		{[]string{}, "", false, false, true},                  // no target
		{[]string{"--no-restart"}, "", false, false, true},    // flags only, no target
		{[]string{"hacs", "extra"}, "", false, false, true},   // two targets
		{[]string{"--bogus", "hacs"}, "", false, false, true}, // unknown flag
	}
	for _, tc := range cases {
		target, noRestart, force, err := parseInstallArgs(tc.args)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseInstallArgs(%v) err=%v wantErr=%v", tc.args, err, tc.wantErr)
			continue
		}
		if tc.wantErr {
			continue
		}
		if target != tc.target || noRestart != tc.noRestart || force != tc.force {
			t.Errorf("parseInstallArgs(%v) = (%q,%v,%v), want (%q,%v,%v)",
				tc.args, target, noRestart, force, tc.target, tc.noRestart, tc.force)
		}
	}
}
