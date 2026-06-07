package domain

// Asset is a resource, skill, advantage, dataset, relationship or product the
// player can bring to bear on a goal.
type Asset struct {
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"` // skill, data, network, product, capital, ...
	Description string `json:"description,omitempty"`
}

// Constraint is a limit, risk, rule or boundary the plan must respect.
type Constraint struct {
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"` // budget, time, geography, policy, risk, ...
	Description string `json:"description,omitempty"`
}

// Context is the current situation and the relevant facts, assets and
// constraints that frame planning for a goal. It is the primary input the
// planner reasons over.
type Context struct {
	Situation   string       `json:"situation,omitempty"`
	Facts       []string     `json:"facts,omitempty"`
	Assets      []Asset      `json:"assets,omitempty"`
	Constraints []Constraint `json:"constraints,omitempty"`
}
