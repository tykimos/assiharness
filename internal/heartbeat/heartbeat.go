package heartbeat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry holds the heartbeat data for a single worker.
type Entry struct {
	RunID     string    `json:"run_id"`
	AgentID   string    `json:"agent_id"`
	WorkerPID int       `json:"worker_pid"`
	LastBeat  time.Time `json:"last_beat"`
}

// Monitor tracks active workers by reading and writing heartbeat files.
type Monitor struct {
	dir     string
	timeout time.Duration
	mu      sync.Mutex
}

// NewMonitor creates a Monitor and ensures <stateDir>/heartbeats/ exists.
func NewMonitor(stateDir string, timeout time.Duration) (*Monitor, error) {
	dir := filepath.Join(stateDir, "heartbeats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("heartbeat: create directory: %w", err)
	}
	return &Monitor{dir: dir, timeout: timeout}, nil
}

// Beat writes or updates the heartbeat file for runID with the current time and PID.
func (m *Monitor) Beat(runID string, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := Entry{
		RunID:     runID,
		AgentID:   agentID,
		WorkerPID: os.Getpid(),
		LastBeat:  time.Now(),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("heartbeat: marshal entry %s: %w", runID, err)
	}
	dest := filepath.Join(m.dir, runID+".json")
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("heartbeat: write file %s: %w", runID, err)
	}
	return nil
}

// Remove deletes the heartbeat file for runID.
func (m *Monitor) Remove(runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.dir, runID+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("heartbeat: remove file %s: %w", runID, err)
	}
	return nil
}

// readAll reads all heartbeat files from the monitor directory.
func (m *Monitor) readAll() ([]Entry, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("heartbeat: read directory: %w", err)
	}

	var results []Entry
	for _, de := range entries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		path := filepath.Join(m.dir, de.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("heartbeat: read file %s: %w", de.Name(), err)
		}
		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, fmt.Errorf("heartbeat: unmarshal file %s: %w", de.Name(), err)
		}
		results = append(results, entry)
	}
	return results, nil
}

// StaleWorkers returns entries where LastBeat + timeout is before now.
func (m *Monitor) StaleWorkers() ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	all, err := m.readAll()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var stale []Entry
	for _, e := range all {
		if e.LastBeat.Add(m.timeout).Before(now) {
			stale = append(stale, e)
		}
	}
	return stale, nil
}

// ActiveWorkers returns entries where LastBeat + timeout is at or after now.
func (m *Monitor) ActiveWorkers() ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	all, err := m.readAll()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var active []Entry
	for _, e := range all {
		if !e.LastBeat.Add(m.timeout).Before(now) {
			active = append(active, e)
		}
	}
	return active, nil
}
