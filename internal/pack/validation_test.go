package pack

import (
	"testing"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

func warnRule(check ValidationCheck, kinds []string, scopes []KindScope) Descriptor {
	return Descriptor{Validation: Validation{Rules: []ValidationRule{{
		Check: check, Kinds: kinds, Scopes: scopes,
		Field: "f", Message: "m", Severity: SeverityWarning,
	}}}}
}

func TestRequireMetric(t *testing.T) {
	d := warnRule(CheckRequireMetric, nil, nil)
	if got := d.Validate(domain.Goal{}); len(got) != 1 {
		t.Errorf("missing metric should warn, got %v", got)
	}
	if got := d.Validate(domain.Goal{Metric: "customers"}); len(got) != 0 {
		t.Errorf("present metric should be quiet, got %v", got)
	}
}

func TestRequireContext(t *testing.T) {
	d := warnRule(CheckRequireContext, nil, nil)
	if got := d.Validate(domain.Goal{}); len(got) != 1 {
		t.Errorf("empty context should warn, got %v", got)
	}
	g := domain.Goal{Context: domain.Context{Assets: []domain.Asset{{Name: "a"}}}}
	if got := d.Validate(g); len(got) != 0 {
		t.Errorf("non-empty context should be quiet, got %v", got)
	}
}

func TestRequireAnyKindCrossScope(t *testing.T) {
	d := warnRule(CheckRequireAnyKind, []string{"time_horizon"}, []KindScope{ScopeAsset, ScopeConstraint})

	if got := d.Validate(domain.Goal{}); len(got) != 1 {
		t.Errorf("absent kind should warn, got %v", got)
	}
	// Satisfied by an asset of that kind.
	asset := domain.Goal{Context: domain.Context{Assets: []domain.Asset{{Kind: "time_horizon"}}}}
	if got := d.Validate(asset); len(got) != 0 {
		t.Errorf("asset kind should satisfy, got %v", got)
	}
	// Satisfied by a constraint of that kind, case-insensitively.
	constraint := domain.Goal{Context: domain.Context{Constraints: []domain.Constraint{{Kind: "TIME_HORIZON"}}}}
	if got := d.Validate(constraint); len(got) != 0 {
		t.Errorf("constraint kind (any case) should satisfy, got %v", got)
	}
}

func TestRequireAnyKindScopeRestriction(t *testing.T) {
	// Scoped to constraints only: an asset of the kind must NOT satisfy it.
	d := warnRule(CheckRequireAnyKind, []string{"time"}, []KindScope{ScopeConstraint})
	asset := domain.Goal{Context: domain.Context{Assets: []domain.Asset{{Kind: "time"}}}}
	if got := d.Validate(asset); len(got) != 1 {
		t.Errorf("asset must not satisfy a constraint-scoped rule, got %v", got)
	}
	constraint := domain.Goal{Context: domain.Context{Constraints: []domain.Constraint{{Kind: "time"}}}}
	if got := d.Validate(constraint); len(got) != 0 {
		t.Errorf("constraint should satisfy, got %v", got)
	}
}

func TestWarnUnknownKinds(t *testing.T) {
	d := Descriptor{
		Vocab:      Vocabulary{AssetKinds: []string{"skill"}, ConstraintKinds: []string{"budget"}},
		Validation: Validation{WarnUnknownKinds: true},
	}
	// Off-vocab asset kind warns.
	if got := d.Validate(domain.Goal{Context: domain.Context{Assets: []domain.Asset{{Kind: "bogus"}}}}); len(got) != 1 {
		t.Errorf("off-vocab asset kind should warn, got %v", got)
	}
	// In-vocab kinds and an empty kind are silent.
	quiet := domain.Goal{Context: domain.Context{
		Assets:      []domain.Asset{{Kind: "skill"}, {Kind: ""}},
		Constraints: []domain.Constraint{{Kind: "budget"}},
	}}
	if got := d.Validate(quiet); len(got) != 0 {
		t.Errorf("in-vocab and empty kinds should be quiet, got %v", got)
	}
	// With the flag off, even off-vocab kinds are silent.
	off := Descriptor{Vocab: d.Vocab, Validation: Validation{WarnUnknownKinds: false}}
	if got := off.Validate(domain.Goal{Context: domain.Context{Assets: []domain.Asset{{Kind: "bogus"}}}}); len(got) != 0 {
		t.Errorf("WarnUnknownKinds off should be quiet, got %v", got)
	}
}

func TestBuiltinsDoNotWarnUnknownKinds(t *testing.T) {
	r := NewRegistry()
	for _, id := range r.IDs() {
		d, _ := r.Get(id)
		if d.Validation.WarnUnknownKinds {
			t.Errorf("%s enables WarnUnknownKinds by default; built-ins must keep vocab non-binding", id)
		}
	}
}
