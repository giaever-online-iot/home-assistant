package config

import "testing"

func TestParseEmptyAppliesDefaults(t *testing.T) {
	c, err := Parse([]byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ImageRegistry != "ghcr.io/home-assistant/home-assistant" || c.ImageChannel != "stable" {
		t.Errorf("image defaults wrong: %q %q", c.ImageRegistry, c.ImageChannel)
	}
	if c.ImageDigest != "" {
		t.Errorf("digest should default empty, got %q", c.ImageDigest)
	}
	if c.Network != "host" || !c.Privileged || !c.Bluetooth {
		t.Errorf("defaults wrong: net=%q priv=%v bt=%v", c.Network, c.Privileged, c.Bluetooth)
	}
}

func TestParseOverrides(t *testing.T) {
	in := `{"image":{"channel":"beta","digest":"sha256:abc"},"docker":{"privileged":false,"timezone":"Europe/Oslo","devices":{"zigbee":"/dev/ttyUSB0"},"extra-args":"--shm-size=256m","config-dir":"/home/joachim/home-assistant"}}`
	c, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ImageChannel != "beta" || c.ImageDigest != "sha256:abc" {
		t.Errorf("image override wrong: %q %q", c.ImageChannel, c.ImageDigest)
	}
	if c.Privileged {
		t.Errorf("privileged should be false (explicit override)")
	}
	if c.Timezone != "Europe/Oslo" || c.Devices["zigbee"] != "/dev/ttyUSB0" || c.ExtraArgs != "--shm-size=256m" || c.ConfigDir != "/home/joachim/home-assistant" {
		t.Errorf("overrides wrong: %+v", c)
	}
}

func TestValidateWarnsOnNonHostNetwork(t *testing.T) {
	c, _ := Parse([]byte(`{"docker":{"network":"bridge"}}`))
	warnings, err := c.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for non-host network")
	}
}

func TestValidateRejectsBadInputs(t *testing.T) {
	bad := []string{
		`{"docker":{"devices":{"z":"ttyUSB0"}}}`,
		`{"docker":{"volumes":{"m":"/mnt/media"}}}`,
		`{"docker":{"config-dir":"relative/path"}}`,
	}
	for _, in := range bad {
		c, _ := Parse([]byte(in))
		if _, err := c.Validate(); err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
}
