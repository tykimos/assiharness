package worktree

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tykimos/assiharness/internal/models"
)

// worktreeEntry holds the run ID that claimed a worktree.
type worktreeEntry struct {
	RunID string `json:"run_id"`
}

// Manager tracks which worktrees are occupied and persists state to disk.
type Manager struct {
	mu        sync.Mutex
	occupied  map[string]worktreeEntry
	stateFile string
}

// NewManager creates a Manager and loads existing state from <stateDir>/worktrees.json.
func NewManager(stateDir string) (*Manager, error) {
	m := &Manager{
		occupied:  make(map[string]worktreeEntry),
		stateFile: filepath.Join(stateDir, "worktrees.json"),
	}
	if err := m.load(); err != nil {
		return nil, fmt.Errorf("worktree manager: load state: %w", err)
	}
	return m, nil
}

// GenerateName produces a worktree name by substituting {agent_id} and {task_id}
// in the agent's worktree pattern. Falls back to "{agent_id}-{task_id}".
func (m *Manager) GenerateName(agent models.AgentConfig, task models.Task) string {
	pattern := agent.Worktree.Pattern
	if pattern == "" {
		pattern = "{agent_id}-{task_id}"
	}
	name := strings.ReplaceAll(pattern, "{agent_id}", agent.ID)
	name = strings.ReplaceAll(name, "{task_id}", task.ID)
	return name
}

// IsOccupied reports whether the named worktree is currently claimed.
func (m *Manager) IsOccupied(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.occupied[name]
	return ok
}

// Claim marks a worktree as occupied by the given run and persists state.
func (m *Manager) Claim(name string, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.occupied[name] = worktreeEntry{RunID: runID}
	return m.save()
}

// Release removes a worktree from the occupied set and persists state.
func (m *Manager) Release(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.occupied, name)
	return m.save()
}

// Cleanup removes the git worktree and its branch, then calls Release.
// Errors from git commands are ignored (the worktree or branch may not exist).
func (m *Manager) Cleanup(name string) error {
	exec.Command("git", "worktree", "remove", name).Run() //nolint:errcheck
	exec.Command("git", "branch", "-D", "worktree-"+name).Run() //nolint:errcheck
	return m.Release(name)
}

// save writes the current occupied map to stateFile. Caller must hold m.mu.
func (m *Manager) save() error {
	data, err := json.MarshalIndent(m.occupied, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal worktree state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.stateFile), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := os.WriteFile(m.stateFile, data, 0o644); err != nil {
		return fmt.Errorf("write worktree state: %w", err)
	}
	return nil
}

// load reads state from stateFile. Missing file is not an error.
func (m *Manager) load() error {
	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read worktree state: %w", err)
	}
	if err := json.Unmarshal(data, &m.occupied); err != nil {
		return fmt.Errorf("unmarshal worktree state: %w", err)
	}
	return nil
}
