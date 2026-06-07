package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/config"
	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
	"github.com/vingrad/dynamic-decision-engine/internal/wire"
)

func TestBuildSourceDepsDisabledOrEmpty(t *testing.T) {
	deps, err := buildSourceDeps(config.Config{}) // no path
	if err != nil {
		t.Fatal(err)
	}
	if len(deps.Sources) != 0 {
		t.Errorf("expected no sources with empty path, got %d", len(deps.Sources))
	}
}

func TestBuildSourceDepsLoadsHTTP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.yaml")
	body := "" +
		"sources:\n" +
		"  - name: pricefeed\n" +
		"    kind: http\n" +
		"    domain: purchasing\n" +
		"    endpoint: http://example.test/feed\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	deps, err := buildSourceDeps(config.Config{SourcesConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := deps.Sources["pricefeed"]
	if !ok {
		t.Fatalf("expected a pricefeed source, got keys %v", keys(deps.Sources))
	}
	if s.Describe().Name != "pricefeed" {
		t.Errorf("unexpected source name %q", s.Describe().Name)
	}

	// End-to-end through the resolver: a config-defined domain declaring the source
	// by name resolves to a working enricher.
	reg := pack.NewRegistryFrom(pack.Descriptor{ID: "purchasing", Name: "Purchasing", Version: "1", SourceKinds: []string{"pricefeed"}})
	r := wire.NewSourceResolver(reg, policy.Policy{}, deps)
	if r.SourcesFor("purchasing") == nil {
		t.Error("expected an enricher for the purchasing domain")
	}
	// The source itself will degrade (unreachable endpoint) but never error out.
	_, contribs := r.SourcesFor("purchasing").Enrich(context.Background(), domain.Goal{Domain: "purchasing", ID: "g1"}, "", nil)
	if len(contribs) != 1 || !contribs[0].Stale {
		t.Errorf("expected one stale contribution from the unreachable endpoint, got %+v", contribs)
	}
}

func TestBuildSourceDepsRejectsUnknownKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.json")
	if err := os.WriteFile(path, []byte(`{"sources":[{"name":"x","kind":"telepathy"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildSourceDeps(config.Config{SourcesConfigPath: path}); err == nil {
		t.Error("expected an error for an unknown source kind")
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
