package config

import "testing"

func TestParsePortSpec(t *testing.T) {
	cases := []struct {
		in   string
		want PortSpec
	}{
		{"1880", PortSpec{"127.0.0.1", "1880", "1880"}},
		{"8080:1880", PortSpec{"127.0.0.1", "8080", "1880"}},
		{"0.0.0.0:1883:1883", PortSpec{"0.0.0.0", "1883", "1883"}},
	}
	for _, c := range cases {
		got, err := ParsePortSpec(c.in)
		if err != nil {
			t.Errorf("ParsePortSpec(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParsePortSpec(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParsePortSpecRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "abc", "0", "65536", "80:", ":80", "1.2.3.4:80:81:82", "1.2.3.4::80"} {
		if _, err := ParsePortSpec(in); err == nil {
			t.Errorf("ParsePortSpec(%q): expected error", in)
		}
	}
}
