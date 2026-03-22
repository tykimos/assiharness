package config

import "github.com/tykimos/assiharness/internal/models"

// Config aggregates all configuration sections loaded from the config directory.
type Config struct {
	Agents    []models.AgentConfig
	Routes    models.RoutesConfig
	Schedules models.SchedulesConfig
	Policies  models.PolicyConfig
	Runtime   models.RuntimeConfig
}
