package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vingrad/dynamic-decision-engine/internal/config"
	"github.com/vingrad/dynamic-decision-engine/internal/source"
	"github.com/vingrad/dynamic-decision-engine/internal/wire"
)

// sourceDef is one entry in the sources config file (DDE_SOURCES). A domain's
// pack SourceKinds reference these by Name.
type sourceDef struct {
	Name      string `json:"name" yaml:"name"`
	Kind      string `json:"kind" yaml:"kind"`     // currently "http" (the default)
	Domain    string `json:"domain" yaml:"domain"` // optional; informational
	Endpoint  string `json:"endpoint" yaml:"endpoint"`
	APIKeyEnv string `json:"api_key_env" yaml:"api_key_env"` // env var holding the bearer token
}

type sourcesFile struct {
	Sources []sourceDef `json:"sources" yaml:"sources"`
}

// buildSourceDeps assembles the wired source registry from the sources config file.
// It is only called when cfg.SourcesEnabled is true. An empty/absent file yields an
// empty registry (so every domain resolves to the no-op enricher).
func buildSourceDeps(cfg config.Config) (wire.SourceDeps, error) {
	deps := wire.SourceDeps{Timeout: cfg.SourceTimeout}
	if cfg.SourcesConfigPath == "" {
		return deps, nil
	}
	data, err := os.ReadFile(cfg.SourcesConfigPath)
	if err != nil {
		return deps, fmt.Errorf("sources: read %s: %w", cfg.SourcesConfigPath, err)
	}
	var sf sourcesFile
	if strings.HasSuffix(cfg.SourcesConfigPath, ".json") {
		err = json.Unmarshal(data, &sf)
	} else {
		err = yaml.Unmarshal(data, &sf)
	}
	if err != nil {
		return deps, fmt.Errorf("sources: parse %s: %w", cfg.SourcesConfigPath, err)
	}

	reg := make(map[string]source.Source, len(sf.Sources))
	for _, d := range sf.Sources {
		if d.Name == "" {
			return deps, fmt.Errorf("sources: a source entry is missing a name")
		}
		if _, dup := reg[d.Name]; dup {
			return deps, fmt.Errorf("sources: duplicate source name %q", d.Name)
		}
		switch d.Kind {
		case "http", "":
			apiKey := ""
			if d.APIKeyEnv != "" {
				apiKey = os.Getenv(d.APIKeyEnv)
			}
			reg[d.Name] = source.NewHTTPSource(source.HTTPConfig{
				Name:     d.Name,
				Domain:   d.Domain,
				Endpoint: d.Endpoint,
				APIKey:   apiKey,
				Timeout:  cfg.SourceTimeout,
			})
		default:
			return deps, fmt.Errorf("sources: unknown kind %q for source %q", d.Kind, d.Name)
		}
	}
	deps.Sources = reg
	return deps, nil
}
