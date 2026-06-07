package domain

import "time"

// PlayerKind enumerates the kinds of decision-makers the engine plans for.
type PlayerKind string

const (
	PlayerPerson  PlayerKind = "person"
	PlayerTeam    PlayerKind = "team"
	PlayerCompany PlayerKind = "company"
	PlayerSystem  PlayerKind = "system"
)

// Valid reports whether the kind is recognised.
func (k PlayerKind) Valid() bool {
	switch k {
	case PlayerPerson, PlayerTeam, PlayerCompany, PlayerSystem:
		return true
	default:
		return false
	}
}

// Player is the person, team, company or system making decisions. A player owns
// goals and provides the framing for every plan generated on its behalf.
type Player struct {
	ID        string            `json:"id"`
	Kind      PlayerKind        `json:"kind"`
	Name      string            `json:"name"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}
