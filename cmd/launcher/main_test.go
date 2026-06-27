// cmd/launcher/main_test.go
package main

import (
	"strings"
	"testing"
)

func TestDockerSteps(t *testing.T) {
	tests := []struct {
		name                    string
		execConn, sockConn, bin bool
		want                    []string
	}{
		{"all ready", true, true, true, nil},
		{"nothing connected", false, false, false, []string{
			"sudo snap install docker",
			"sudo snap connect home-assistant:docker-executables docker:docker-executables",
			"sudo snap connect home-assistant:docker docker:docker-daemon",
		}},
		{"only daemon socket missing", true, false, true, []string{
			"sudo snap connect home-assistant:docker docker:docker-daemon",
		}},
		{"only content missing (binary absent)", false, true, false, []string{
			"sudo snap install docker",
			"sudo snap connect home-assistant:docker-executables docker:docker-executables",
		}},
		{"content connected but binary missing (broken provider)", true, true, false, []string{
			"sudo snap install docker",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dockerSteps(tt.execConn, tt.sockConn, tt.bin)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("dockerSteps(%v,%v,%v)\n got: %v\nwant: %v", tt.execConn, tt.sockConn, tt.bin, got, tt.want)
			}
		})
	}
}
