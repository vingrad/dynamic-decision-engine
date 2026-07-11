package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDemoCommandWalksTheFullLoop(t *testing.T) {
	for _, scenario := range []string{"growth", "investing"} {
		t.Run(scenario, func(t *testing.T) {
			cmd := newDemoCommand()
			cmd.SetArgs([]string{scenario})
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("demo %s: %v", scenario, err)
			}

			out := buf.String()
			for _, want := range []string{
				"[1/5] GOAL",
				"[2/5] PLAN v1",
				"[3/5] SIGNAL",
				"material: true",
				"[4/5] PLAN v2",
				"[5/5] OUTCOME",
				"failure",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("demo %s output missing %q", scenario, want)
				}
			}
		})
	}
}

func TestDemoCommandRejectsUnknownScenario(t *testing.T) {
	cmd := newDemoCommand()
	cmd.SetArgs([]string{"poker"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an unknown scenario")
	}
}
