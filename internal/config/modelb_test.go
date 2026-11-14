package config

import (
	"strings"
	"testing"
)

// T12 (D11): only a host of exactly localhost, 127.0.0.1 or [::1] is rewritten;
// scheme, port and path are kept.
func TestLoopbackToHostGateway(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"http://localhost:8091", "http://host.docker.internal:8091", true},
		{"http://127.0.0.1:3000/x", "http://host.docker.internal:3000/x", true},
		{"http://[::1]:80", "http://host.docker.internal:80", true},
		{"https://localhost/panel?a=1", "https://host.docker.internal/panel?a=1", true},
		{"http://localhost.example:80", "http://localhost.example:80", false},
		{"http://192.168.1.5:80", "http://192.168.1.5:80", false},
		{"http://ha-addon-nodered:1880", "http://ha-addon-nodered:1880", false},
		{"not a url", "not a url", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := LoopbackToHostGateway(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("LoopbackToHostGateway(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// T18 (D12): under model B the warning says what breaks and names each
// rewritten panel; model A has no such warning.
func TestValidateModelBWarning(t *testing.T) {
	const zwave = `"ingress":{"zwave":{"url":"http://localhost:8091"},"lan":{"url":"http://192.168.1.5:80"}}`
	c, _ := Parse([]byte(`{"docker":{"network":"ha-addons"},` + zwave + `}`))
	warnings, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(warnings, "\n")
	for _, want := range []string{
		`docker.network="ha-addons": discovery and Bluetooth need host networking, and Home Assistant's "localhost" becomes its own container — integrations set up with localhost URLs (e.g. zwave_js ws://localhost:3000) lose their connection; point them at host.docker.internal; the host service must listen beyond 127.0.0.1`,
		"ingress.zwave: url rewritten to http://host.docker.internal:8091 under docker.network=ha-addons",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings lack %q\n got:\n%s", want, all)
		}
	}
	if strings.Contains(all, "ingress.lan:") {
		t.Errorf("a non-loopback panel was reported as rewritten:\n%s", all)
	}
	c, _ = Parse([]byte(`{` + zwave + `}`))
	warnings, _ = c.Validate()
	if all := strings.Join(warnings, "\n"); strings.Contains(all, HostGatewayName) {
		t.Errorf("model A must not warn about %s:\n%s", HostGatewayName, all)
	}
}
