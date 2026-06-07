package domain

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// idEncoding produces compact, lowercase, unpadded identifiers.
var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewID returns a globally-unique identifier with a human-readable prefix,
// e.g. NewID("plan") -> "plan_k3f9a2...". The random suffix carries 80 bits of
// entropy, which is ample for collision resistance in this domain.
func NewID(prefix string) string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure means the platform RNG is broken; there is no
		// sensible way to continue.
		panic("domain: cannot read random bytes: " + err.Error())
	}
	return prefix + "_" + strings.ToLower(idEncoding.EncodeToString(b))
}
