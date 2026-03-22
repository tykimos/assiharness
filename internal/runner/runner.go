package runner

import (
	"context"

	"github.com/tykimos/assiharness/internal/models"
)

// Runner executes a task on behalf of an agent.
type Runner interface {
	Run(ctx context.Context, agent models.AgentConfig, task models.Task) (models.RunResult, error)
}
