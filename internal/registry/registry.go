package registry

import "github.com/tykimos/assiharness/internal/models"

// Registry holds all agent configurations indexed by ID.
type Registry struct {
	agents map[string]models.AgentConfig
}

// NewRegistry builds a Registry from a slice of AgentConfig.
func NewRegistry(agents []models.AgentConfig) *Registry {
	m := make(map[string]models.AgentConfig, len(agents))
	for _, a := range agents {
		m[a.ID] = a
	}
	return &Registry{agents: m}
}

// Get returns the AgentConfig for the given ID.
func (r *Registry) Get(id string) (models.AgentConfig, bool) {
	a, ok := r.agents[id]
	return a, ok
}

// List returns all registered agents.
func (r *Registry) List() []models.AgentConfig {
	out := make([]models.AgentConfig, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	return out
}

// ListEnabled returns only agents with Enabled set to true.
func (r *Registry) ListEnabled() []models.AgentConfig {
	var out []models.AgentConfig
	for _, a := range r.agents {
		if a.Enabled {
			out = append(out, a)
		}
	}
	return out
}
