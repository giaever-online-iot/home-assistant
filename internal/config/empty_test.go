package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// T14 (D15): the maximal empty-object paths, sorted; values are never listed.
func TestEmptyPaths(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"addons":{}}`, "addons"},
		{`{"docker":{},"ingress":{"a":{"url":"u"}}}`, "docker"},
		{`{"addons":{"x":{"ingress":{}},"y":{"image":"i"}}}`, "addons.x"},
		{`{"addons":{"x":{"ingress":{}}}}`, "addons"},
		{`{"addons":{"y":{"image":"i","ports":{},"ingress":{"title":"T"}}},"docker":{"devices":{}}}`, "addons.y.ports,docker"},
		{`{"docker":{"privileged":false,"timezone":"","extra-args":""},"image":{"channel":"stable"}}`, ""},
		{`{}`, ""},
	}
	for _, tc := range cases {
		var doc map[string]any
		if err := json.Unmarshal([]byte(tc.in), &doc); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(EmptyPaths(doc), ","); got != tc.want {
			t.Errorf("EmptyPaths(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
