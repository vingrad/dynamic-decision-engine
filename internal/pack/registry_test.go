package pack

import (
	"reflect"
	"testing"
)

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
