package config

import (
	"testing"
	"time"
)

func TestReplanEnvOverrides(t *testing.T) {
	t.Setenv("DDE_REPLAN_TIMEOUT", "90s")
	t.Setenv("DDE_REPLAN_MAX_RETRIES", "5")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReplanTimeout != 90*time.Second {
		t.Errorf("expected replan_timeout 90s, got %v", cfg.ReplanTimeout)
	}
	if cfg.ReplanMaxRetries != 5 {
		t.Errorf("expected replan_max_retries 5, got %d", cfg.ReplanMaxRetries)
	}
}

func TestReplanDefaults(t *testing.T) {
	c := Default()
	if c.ReplanTimeout != 60*time.Second {
		t.Errorf("default replan_timeout should be 60s, got %v", c.ReplanTimeout)
	}
	if c.ReplanMaxRetries != 2 {
		t.Errorf("default replan_max_retries should be 2, got %d", c.ReplanMaxRetries)
	}
}

func TestValidateRejectsBadReplanKnobs(t *testing.T) {
	c := Default()
	c.ReplanTimeout = 0
	if err := c.Validate(); err == nil {
		t.Error("expected error for non-positive replan_timeout")
	}

	c = Default()
	c.ReplanMaxRetries = -1
	if err := c.Validate(); err == nil {
		t.Error("expected error for negative replan_max_retries")
	}
}
