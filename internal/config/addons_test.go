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
	for _, in := range []string{"", "abc", "0", "65536", "80:", ":80", "1.2.3.4:80:81:82", "1.2.3.4::80", "junk:80:81", "999.1.1.1:80:80"} {
		if _, err := ParsePortSpec(in); err == nil {
			t.Errorf("ParsePortSpec(%q): expected error", in)
		}
	}
}

func TestParseAddons(t *testing.T) {
	in := `{"addons":{"nodered":{
		"image":"nodered/node-red:latest",
		"ports":{"ui":1880,"alt":"8080:1880"},
		"data-dir":"/data",
		"volumes":{"certs":"/etc/certs:/certs:ro"},
		"environment":{"foo":"bar"},
		"ingress":{"title":"Node-RED","icon":"mdi:nodejs","require-admin":true}
	}}}`
	c, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a, ok := c.Addons["nodered"]
	if !ok {
		t.Fatal("addon nodered not parsed")
	}
	if a.Image != "nodered/node-red:latest" || a.DataDir != "/data" {
		t.Errorf("image/data-dir wrong: %+v", a)
	}
	// JSON numbers (snapctl emits unquoted ints) normalize to strings.
	if a.Ports["ui"] != "1880" || a.Ports["alt"] != "8080:1880" {
		t.Errorf("ports wrong: %+v", a.Ports)
	}
	if a.Volumes["certs"] != "/etc/certs:/certs:ro" || a.Environment["foo"] != "bar" {
		t.Errorf("volumes/env wrong: %+v", a)
	}
	if a.Ingress == nil || a.Ingress.Title != "Node-RED" || a.Ingress.Icon != "mdi:nodejs" || !a.Ingress.RequireAdmin {
		t.Errorf("ingress wrong: %+v", a.Ingress)
	}
}

func TestParseAddonsAbsent(t *testing.T) {
	c, err := Parse([]byte(`{}`))
	if err != nil || c.Addons != nil {
		t.Fatalf("no addons should parse to nil map, got %+v err=%v", c.Addons, err)
	}
	c2, _ := Parse([]byte(`{"addons":{"x":{"image":"img"}}}`))
	if c2.Addons["x"].Ingress != nil {
		t.Error("no ingress keys should mean a nil Ingress (no panel wanted)")
	}
}

func TestIngressPortSpec(t *testing.T) {
	ing := &AddonIngress{Title: "X"}
	// "ui" label wins even with several ports.
	s := AddonSpec{Ports: map[string]string{"ui": "1880", "metrics": "9100"}, Ingress: ing}
	if ps, err := s.IngressPortSpec(); err != nil || ps.Host != "1880" {
		t.Errorf("ui label: %+v, %v", ps, err)
	}
	// A single port is unambiguous.
	s = AddonSpec{Ports: map[string]string{"web": "3000"}, Ingress: ing}
	if ps, err := s.IngressPortSpec(); err != nil || ps.Host != "3000" {
		t.Errorf("single port: %+v, %v", ps, err)
	}
	// Explicit ingress.port selects a label.
	s = AddonSpec{Ports: map[string]string{"a": "1000", "b": "2000"}, Ingress: &AddonIngress{Port: "b"}}
	if ps, err := s.IngressPortSpec(); err != nil || ps.Host != "2000" {
		t.Errorf("explicit label: %+v, %v", ps, err)
	}
}

func TestIngressPortSpecErrors(t *testing.T) {
	ing := &AddonIngress{}
	cases := []AddonSpec{
		{Ingress: ing}, // no ports at all
		{Ports: map[string]string{"a": "1000", "b": "2000"}, Ingress: ing},         // ambiguous
		{Ports: map[string]string{"a": "1000"}, Ingress: &AddonIngress{Port: "z"}}, // unknown label
		{Ingress: nil}, // no panel wanted
	}
	for i, s := range cases {
		if _, err := s.IngressPortSpec(); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestValidateAddonsRejectsBadInputs(t *testing.T) {
	bad := []string{
		`{"addons":{"x":{}}}`, // missing image
		`{"addons":{"x":{"image":"img","ports":{"ui":"abc"}}}}`,                                                // bad port
		`{"addons":{"x":{"image":"img","data-dir":"relative"}}}`,                                               // relative data-dir
		`{"addons":{"x":{"image":"img","volumes":{"v":"/just-a-path"}}}}`,                                      // volume without :
		`{"addons":{"x":{"image":"img","devices":{"z":"/dev/ttyACM0"}}}}`,                                      // devices reserved
		`{"addons":{"Bad_Name":{"image":"img"}}}`,                                                              // invalid name
		`{"addons":{"x":{"image":"img","ingress":{"title":"X"}}}}`,                                             // panel with no ports
		`{"addons":{"x":{"image":"img","ports":{"a":"1","b":"2"},"ingress":{}}}}`,                              // ambiguous panel port
		`{"addons":{"x":{"image":"img","ports":{"ui":"80"},"ingress":{}}},"ingress":{"x":{"url":"http://y"}}}`, // name collision
	}
	for _, in := range bad {
		c, err := Parse([]byte(in))
		if err != nil {
			t.Fatalf("parse should not fail for %s: %v", in, err)
		}
		if _, err := c.Validate(); err == nil {
			t.Errorf("expected validate error for %s", in)
		}
	}
}

func TestValidateAddonsRejectsPortCollisions(t *testing.T) {
	// Two add-ons publishing the same ip:host pair.
	c, _ := Parse([]byte(`{"addons":{
		"a":{"image":"img","ports":{"ui":"80"}},
		"b":{"image":"img","ports":{"ui":"80"}}
	}}`))
	if _, err := c.Validate(); err == nil {
		t.Error("duplicate published host port across add-ons must be rejected")
	}

	// Same container port, different host ports: no collision.
	c, _ = Parse([]byte(`{"addons":{
		"a":{"image":"img","ports":{"ui":"8080:80"}},
		"b":{"image":"img","ports":{"ui":"8081:80"}}
	}}`))
	if _, err := c.Validate(); err != nil {
		t.Errorf("distinct host ports must be accepted: %v", err)
	}

	// :8123 is only reserved once HA itself publishes it (model B).
	c, _ = Parse([]byte(`{"addons":{"x":{"image":"img","ports":{"ui":"8123"}}}}`))
	if _, err := c.Validate(); err != nil {
		t.Errorf(":8123 must be accepted outside model B: %v", err)
	}
	c, _ = Parse([]byte(`{"docker":{"network":"ha-addons"},"addons":{"x":{"image":"img","ports":{"ui":"8123"}}}}`))
	if _, err := c.Validate(); err == nil {
		t.Error(":8123 must be rejected under model B (docker.network=ha-addons)")
	}
}

func TestValidateAddonsAcceptsGood(t *testing.T) {
	in := `{"addons":{"nodered":{"image":"nodered/node-red:latest","ports":{"ui":1880},"data-dir":"/data","ingress":{"title":"Node-RED"}}}}`
	c, _ := Parse([]byte(in))
	if _, err := c.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
