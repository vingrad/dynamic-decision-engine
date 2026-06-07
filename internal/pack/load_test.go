package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

const fitnessYAML = `domains:
  - id: fitness
    name: Fitness
    prompt_template: |
      DOMAIN: FITNESS
      Treat each move as a training experiment.
    eval:
      confidence_delta: 0.2
    vocab:
      asset_kinds: [time, equipment]
    validation:
      rules:
        - check: require_metric
          field: metric
          message: "no fitness metric set"
          severity: warning
`

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDescriptorsYAML(t *testing.T) {
	path := writeTemp(t, t.TempDir(), "domains.yaml", fitnessYAML)
	got, err := LoadDescriptors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(got))
	}
	d := got[0]
	if d.ID != "fitness" || d.Name != "Fitness" {
		t.Errorf("top-level fields not mapped: %+v", d)
	}
	// Nested values must land — this is the guard against missing yaml tags.
	if d.Eval.ConfidenceDelta != 0.2 {
		t.Errorf("nested eval.confidence_delta not parsed: %v", d.Eval.ConfidenceDelta)
	}
	if len(d.Validation.Rules) != 1 || d.Validation.Rules[0].Check != CheckRequireMetric {
		t.Errorf("nested validation rule not parsed: %+v", d.Validation)
	}
	if len(d.Vocab.AssetKinds) != 2 {
		t.Errorf("nested vocab not parsed: %+v", d.Vocab)
	}
	// Defaults applied.
	if d.Version != "1" || d.PromptVersion != "fitness-v1" {
		t.Errorf("defaults not applied: version=%q promptVersion=%q", d.Version, d.PromptVersion)
	}
}

func TestLoadDescriptorsJSON(t *testing.T) {
	const j = `{"domains":[{"id":"legal","name":"Legal","prompt_template":"DOMAIN: LEGAL",
		"eval":{"confidence_delta":0.15},
		"validation":{"rules":[{"check":"require_context","field":"context","message":"m","severity":"warning"}]}}]}`
	path := writeTemp(t, t.TempDir(), "domains.json", j)
	got, err := LoadDescriptors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "legal" {
		t.Fatalf("json embed promotion failed: %+v", got)
	}
	if got[0].Eval.ConfidenceDelta != 0.15 || len(got[0].Validation.Rules) != 1 {
		t.Errorf("json nested fields not parsed: %+v", got[0])
	}
}

func TestLoadDescriptorsEmptyPath(t *testing.T) {
	got, err := LoadDescriptors("")
	if err != nil || got != nil {
		t.Errorf("empty path should be (nil,nil), got (%v,%v)", got, err)
	}
}

func TestLoadDescriptorsRejections(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"empty id":          "domains:\n  - name: NoID\n",
		"builtin collision": "domains:\n  - id: investing\n    name: Dup\n",
		"duplicate id":      "domains:\n  - id: x\n    name: A\n  - id: x\n    name: B\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeTemp(t, dir, "d.yaml", body)
			if _, err := LoadDescriptors(path); err == nil {
				t.Errorf("expected error for %q", name)
			}
		})
	}
}

func TestLoadDescriptorsPromptFile(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "legal.txt", "DOMAIN: LEGAL\nfrom file")
	path := writeTemp(t, dir, "domains.yaml", "domains:\n  - id: legal\n    name: Legal\n    prompt_file: legal.txt\n")

	got, err := LoadDescriptors(path)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].PromptTemplate != "DOMAIN: LEGAL\nfrom file" {
		t.Errorf("prompt_file not loaded, got %q", got[0].PromptTemplate)
	}

	// Both prompt_file and prompt_template set -> error.
	both := writeTemp(t, dir, "both.yaml", "domains:\n  - id: legal\n    name: L\n    prompt_file: legal.txt\n    prompt_template: inline\n")
	if _, err := LoadDescriptors(both); err == nil {
		t.Error("expected error when both prompt_file and prompt_template are set")
	}
}

// TestLoadedDomainValidatesAndRegisters proves a config-loaded domain is fully
// functional: it registers via NewRegistryFrom and its data-driven Validate fires.
func TestLoadedDomainValidatesAndRegisters(t *testing.T) {
	path := writeTemp(t, t.TempDir(), "domains.yaml", fitnessYAML)
	loaded, err := LoadDescriptors(path)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistryFrom(loaded...)

	d, ok := reg.Get("fitness")
	if !ok {
		t.Fatal("loaded domain not registered")
	}
	iss := d.Validate(domain.Goal{Objective: "get fit"}) // no metric -> rule fires
	if len(iss) != 1 || iss[0].Message != "no fitness metric set" {
		t.Errorf("config-defined validation did not fire as expected: %+v", iss)
	}
}
