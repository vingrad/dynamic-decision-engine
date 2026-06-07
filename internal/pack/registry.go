package pack

import "sort"

// Registry resolves a domain key to its Descriptor. It is read-only after
// construction and therefore safe for concurrent use.
type Registry struct {
	packs    map[string]Descriptor
	fallback Descriptor
}

// NewRegistry builds the registry of built-in packs with "generic" as the default.
func NewRegistry() *Registry {
	r := &Registry{packs: map[string]Descriptor{}}
	for _, d := range []Descriptor{
		genericPack(),
		investingPack(),
		growthPack(),
		careerPack(),
	} {
		r.packs[d.ID] = d
	}
	r.fallback = r.packs[DefaultDomain]
	return r
}

// Get returns the descriptor for a domain key. An empty key resolves to the
// default with ok=true; an unknown non-empty key resolves to the default with
// ok=false so callers can reject it.
func (r *Registry) Get(domainKey string) (Descriptor, bool) {
	if domainKey == "" {
		return r.fallback, true
	}
	d, ok := r.packs[domainKey]
	if !ok {
		return r.fallback, false
	}
	return d, true
}

// Known reports whether a domain key is registered. The empty key (the default)
// is always considered known.
func (r *Registry) Known(domainKey string) bool {
	if domainKey == "" {
		return true
	}
	_, ok := r.packs[domainKey]
	return ok
}

// IDs returns the registered domain keys in sorted order (for help text and
// error messages).
func (r *Registry) IDs() []string {
	out := make([]string, 0, len(r.packs))
	for id := range r.packs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
