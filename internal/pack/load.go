package pack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// descriptorFile is the load-time shape of a domain entry: a Descriptor plus the
// loader-only prompt_file path. The embed flattens Descriptor's fields — via
// yaml ",inline" for YAML and via Go's anonymous-embed field promotion for JSON
// (encoding/json has no ",inline" option) — so authors write one flat object.
type descriptorFile struct {
	Descriptor `yaml:",inline"`
	PromptFile string `json:"prompt_file" yaml:"prompt_file"`
}

// domainsFile is the top-level config shape: a list of domains.
type domainsFile struct {
	Domains []descriptorFile `json:"domains" yaml:"domains"`
}

// LoadDescriptors reads extra domain descriptors from a JSON or YAML file (chosen
// by extension), validates and normalises them, and returns them for registration
// via NewRegistryFrom. An empty path returns nil (built-ins only), so config
// domains are strictly opt-in.
//
// Each domain must have a non-empty id that is unique within the file and does not
// collide with a built-in (built-ins stay authoritative). Version and
// PromptVersion default to "1" and "<id>-v1". A prompt_file (resolved relative to
// the config file's directory) loads the prompt template from disk; setting both
// prompt_file and prompt_template is an error.
func LoadDescriptors(path string) ([]Descriptor, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pack: read domains %s: %w", path, err)
	}
	var df domainsFile
	if strings.HasSuffix(path, ".json") {
		err = json.Unmarshal(data, &df)
	} else {
		err = yaml.Unmarshal(data, &df)
	}
	if err != nil {
		return nil, fmt.Errorf("pack: parse domains %s: %w", path, err)
	}

	builtins := NewRegistry()
	seen := make(map[string]bool, len(df.Domains))
	out := make([]Descriptor, 0, len(df.Domains))
	for i := range df.Domains {
		d, err := normaliseLoaded(df.Domains[i], path, builtins, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// normaliseLoaded validates one loaded entry and resolves its prompt, returning the
// runtime Descriptor.
func normaliseLoaded(entry descriptorFile, path string, builtins *Registry, seen map[string]bool) (Descriptor, error) {
	d := entry.Descriptor
	if d.ID == "" {
		return Descriptor{}, fmt.Errorf("pack: domain in %s has an empty id", path)
	}
	if d.ID == DefaultDomain || builtins.Known(d.ID) {
		return Descriptor{}, fmt.Errorf("pack: domain %q collides with a built-in; built-ins cannot be overridden from config", d.ID)
	}
	if seen[d.ID] {
		return Descriptor{}, fmt.Errorf("pack: domain %q is defined more than once in %s", d.ID, path)
	}
	seen[d.ID] = true

	if entry.PromptFile != "" {
		if d.PromptTemplate != "" {
			return Descriptor{}, fmt.Errorf("pack: domain %q sets both prompt_file and prompt_template", d.ID)
		}
		tmpl, err := readPromptFile(entry.PromptFile, path)
		if err != nil {
			return Descriptor{}, fmt.Errorf("pack: domain %q: %w", d.ID, err)
		}
		d.PromptTemplate = tmpl
	}

	if d.Version == "" {
		d.Version = "1"
	}
	if d.PromptVersion == "" {
		d.PromptVersion = d.ID + "-v1"
	}
	return d, nil
}

// readPromptFile reads a prompt template, resolving relative paths against the
// directory of the domains config file (not the process CWD).
func readPromptFile(promptFile, configPath string) (string, error) {
	if !filepath.IsAbs(promptFile) {
		promptFile = filepath.Join(filepath.Dir(configPath), promptFile)
	}
	b, err := os.ReadFile(promptFile)
	if err != nil {
		return "", fmt.Errorf("read prompt_file %s: %w", promptFile, err)
	}
	return string(b), nil
}
