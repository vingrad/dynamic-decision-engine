package pack

import (
	"reflect"
	"testing"
)

func TestNewRegistryFrom(t *testing.T) {
	extra := Descriptor{ID: "sports", Name: "Sports", Version: "1", PlannerKind: "finance"}
	r := NewRegistryFrom(extra)

	got, ok := r.Get("sports")
	if !ok || got.ID != "sports" || got.PlannerKind != "finance" {
		t.Fatalf("extra descriptor not resolvable: ok=%v got=%+v", ok, got)
	}
	// Built-ins are still present, and the default is unchanged.
	if !r.Known("investing") || !r.Known("growth") {
		t.Error("built-in packs should remain registered alongside extras")
	}
	def, _ := r.Get("")
	if def.ID != DefaultDomain {
		t.Errorf("default should remain %q, got %q", DefaultDomain, def.ID)
	}
}

func TestRegistryGet(t *testing.T) {
	r := NewRegistry()
	tests := []struct {
		key    string
		wantID string
		wantOK bool
	}{
		{"", DefaultDomain, true},
		{"generic", "generic", true},
		{"investing", "investing", true},
		{"growth", "growth", true},
		{"career", "career", true},
		{"nope", DefaultDomain, false}, // unknown falls back to generic, ok=false
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			d, ok := r.Get(tt.key)
			if ok != tt.wantOK {
				t.Errorf("Get(%q) ok = %v, want %v", tt.key, ok, tt.wantOK)
			}
			if d.ID != tt.wantID {
				t.Errorf("Get(%q) ID = %q, want %q", tt.key, d.ID, tt.wantID)
			}
		})
	}
}

func TestRegistryKnown(t *testing.T) {
	r := NewRegistry()
	if !r.Known("") || !r.Known("investing") {
		t.Error("empty and investing should be known")
	}
	if r.Known("bogus") {
		t.Error("bogus should not be known")
	}
}

func TestRegistryIDs(t *testing.T) {
	r := NewRegistry()
	got := r.IDs()
	want := []string{"career", "generic", "growth", "investing"} // sorted
	if !reflect.DeepEqual(got, want) {
		t.Errorf("IDs() = %v, want %v", got, want)
	}
}
