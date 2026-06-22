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
