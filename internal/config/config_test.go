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

func TestWebhookEnvOverrides(t *testing.T) {
	t.Setenv("DDE_WEBHOOK_URL", "https://hooks.example.com/dde")
	t.Setenv("DDE_WEBHOOK_SECRET", "s3cret")
	t.Setenv("DDE_WEBHOOK_TIMEOUT", "10s")
	t.Setenv("DDE_WEBHOOK_RETRIES", "1")
	t.Setenv("DDE_WEBHOOK_EVENTS", "goal.created, plan.created")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebhookURL != "https://hooks.example.com/dde" {
		t.Errorf("expected webhook_url override, got %q", cfg.WebhookURL)
	}
	if cfg.WebhookSecret != "s3cret" {
		t.Errorf("expected webhook_secret override, got %q", cfg.WebhookSecret)
	}
	if cfg.WebhookTimeout != 10*time.Second {
		t.Errorf("expected webhook_timeout 10s, got %v", cfg.WebhookTimeout)
	}
	if cfg.WebhookRetries != 1 {
		t.Errorf("expected webhook_retries 1, got %d", cfg.WebhookRetries)
	}
	want := []string{"goal.created", "plan.created"}
	if len(cfg.WebhookEvents) != len(want) || cfg.WebhookEvents[0] != want[0] || cfg.WebhookEvents[1] != want[1] {
		t.Errorf("expected webhook_events %v, got %v", want, cfg.WebhookEvents)
	}
}

func TestWebhookDefaults(t *testing.T) {
	c := Default()
	if c.WebhookURL != "" {
		t.Errorf("webhooks should be disabled by default, got url %q", c.WebhookURL)
	}
	if c.WebhookTimeout != 5*time.Second {
		t.Errorf("default webhook_timeout should be 5s, got %v", c.WebhookTimeout)
	}
	if c.WebhookRetries != 3 {
		t.Errorf("default webhook_retries should be 3, got %d", c.WebhookRetries)
	}
}

func TestValidateRejectsBadWebhookKnobs(t *testing.T) {
	c := Default()
	c.WebhookURL = "https://hooks.example.com/dde"
	c.WebhookTimeout = 0
	if err := c.Validate(); err == nil {
		t.Error("expected error for non-positive webhook_timeout")
	}

	c = Default()
	c.WebhookURL = "https://hooks.example.com/dde"
	c.WebhookRetries = -1
	if err := c.Validate(); err == nil {
		t.Error("expected error for negative webhook_retries")
	}

	c = Default()
	c.WebhookEvents = []string{"goal.created", "nope"}
	if err := c.Validate(); err == nil {
		t.Error("expected error for unknown webhook event")
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
