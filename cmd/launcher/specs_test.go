package main

import (
	"strings"
	"testing"

	"github.com/giaever-online-iot/home-assistant/internal/config"
)

func TestBuildContainerSpecs(t *testing.T) {
	c, err := config.Parse([]byte(`{"addons":{
		"zeta":{"image":"z:1","ports":{"ui":80}},
		"alpha":{"image":"a:1","ports":{"ui":81}}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	specs := buildContainerSpecs(c)
	if len(specs) != 3 {
		t.Fatalf("specs = %d, want 3", len(specs))
	}
	if specs[0].Name != "homeassistant" {
		t.Errorf("HA must come first, got %q", specs[0].Name)
	}
	if specs[1].Name != "ha-addon-alpha" || specs[2].Name != "ha-addon-zeta" {
		t.Errorf("add-ons must be name-sorted: %q, %q", specs[1].Name, specs[2].Name)
	}
	if specs[1].Image != "a:1" || specs[1].WantHash == "" {
		t.Errorf("addon spec incomplete: %+v", specs[1])
	}
	if !strings.Contains(strings.Join(specs[1].RunArgs, " "), "--name ha-addon-alpha") {
		t.Errorf("addon args wrong: %v", specs[1].RunArgs)
	}
}
