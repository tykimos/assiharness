package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tykimos/assiharness/internal/models"
	"gopkg.in/yaml.v3"
)

// LoadConfig reads all configuration files from configDir and returns a
// populated Config. Missing files are silently ignored and produce zero-value
// defaults. Returns an error only for parse failures or validation errors.
func LoadConfig(configDir string) (*Config, error) {
	cfg := &Config{}

	// Load agents from config/agents/*.yml
	agentGlob := filepath.Join(configDir, "agents", "*.yml")
	agentFiles, err := filepath.Glob(agentGlob)
	if err != nil {
		return nil, fmt.Errorf("glob agents: %w", err)
	}
	for _, f := range agentFiles {
		var agent models.AgentConfig
		if err := decodeYAMLFile(f, &agent); err != nil {
			return nil, fmt.Errorf("parse agent file %s: %w", f, err)
		}
		cfg.Agents = append(cfg.Agents, agent)
	}

	// Load routes.yml
	if err := decodeYAMLFile(filepath.Join(configDir, "routes.yml"), &cfg.Routes); err != nil {
		return nil, fmt.Errorf("parse routes.yml: %w", err)
	}

	// Load schedules.yml
	if err := decodeYAMLFile(filepath.Join(configDir, "schedules.yml"), &cfg.Schedules); err != nil {
		return nil, fmt.Errorf("parse schedules.yml: %w", err)
	}

	// Load policies.yml
	if err := decodeYAMLFile(filepath.Join(configDir, "policies.yml"), &cfg.Policies); err != nil {
		return nil, fmt.Errorf("parse policies.yml: %w", err)
	}

	// Load runtime.yml
	if err := decodeYAMLFile(filepath.Join(configDir, "runtime.yml"), &cfg.Runtime); err != nil {
		return nil, fmt.Errorf("parse runtime.yml: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// decodeYAMLFile reads path into dest. If the file does not exist, dest is
// left at its zero value and no error is returned.
func decodeYAMLFile(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return yaml.Unmarshal(data, dest)
}

// validate checks semantic constraints across the loaded config.
func validate(cfg *Config) error {
	// Build agent ID index and check uniqueness.
	agentIDs := make(map[string]struct{}, len(cfg.Agents))
	for _, a := range cfg.Agents {
		if _, exists := agentIDs[a.ID]; exists {
			return fmt.Errorf("duplicate agent id: %q", a.ID)
		}
		agentIDs[a.ID] = struct{}{}
	}

	// Validate that every route dispatch references a known agent.
	for _, route := range cfg.Routes.Routes {
		agentID := route.Dispatch.Agent
		if agentID == "" {
			continue
		}
		if _, ok := agentIDs[agentID]; !ok {
			return fmt.Errorf("route %q references unknown agent %q", route.ID, agentID)
		}
	}

	// Validate that every schedule dispatch references a known agent.
	for _, job := range cfg.Schedules.Jobs {
		agentID := job.Dispatch.Agent
		if agentID == "" {
			continue
		}
		if _, ok := agentIDs[agentID]; !ok {
			return fmt.Errorf("schedule job %q references unknown agent %q", job.ID, agentID)
		}
	}

	return nil
}
