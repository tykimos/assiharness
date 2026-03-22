package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tykimos/assiharness/internal/models"
)

// ScheduleState tracks the runtime state of a single scheduled job.
type ScheduleState struct {
	LastRunAt           time.Time `json:"last_run_at"`
	NextRunAt           time.Time `json:"next_run_at"`
	Running             bool      `json:"running"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
}

// Scheduler manages periodic jobs defined in schedules.yml.
type Scheduler struct {
	jobs      []models.ScheduleJob
	states    map[string]*ScheduleState
	stateFile string
	mu        sync.Mutex
}

// NewScheduler creates a Scheduler, loading persisted state from
// <stateDir>/schedules.json. Jobs with no existing state have their
// NextRunAt initialised to now so they fire on the first tick.
func NewScheduler(jobs []models.ScheduleJob, stateDir string) (*Scheduler, error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("scheduler: create state dir: %w", err)
	}

	s := &Scheduler{
		jobs:      jobs,
		states:    make(map[string]*ScheduleState),
		stateFile: filepath.Join(stateDir, "schedules.json"),
	}

	if err := s.load(); err != nil {
		return nil, err
	}

	now := time.Now()
	for _, job := range jobs {
		if _, exists := s.states[job.ID]; !exists {
			s.states[job.ID] = &ScheduleState{
				NextRunAt: now,
			}
		}
	}

	return s, nil
}

// DueJobs returns all enabled, non-running jobs whose NextRunAt is at or
// before now.
func (s *Scheduler) DueJobs(now time.Time) []models.ScheduleJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	var due []models.ScheduleJob
	for _, job := range s.jobs {
		if !job.Enabled {
			continue
		}
		st, ok := s.states[job.ID]
		if !ok || st.Running {
			continue
		}
		if !now.Before(st.NextRunAt) {
			due = append(due, job)
		}
	}
	return due
}

// MarkRunning marks a job as currently running and persists state.
func (s *Scheduler) MarkRunning(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.stateFor(jobID)
	st.Running = true
	_ = s.save()
}

// MarkCompleted marks a job as finished, updates timing fields and failure
// counters, then persists state.
func (s *Scheduler) MarkCompleted(jobID string, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.stateFor(jobID)
	st.Running = false
	now := time.Now()
	st.LastRunAt = now

	// Compute interval from the job definition.
	interval := s.intervalFor(jobID)
	st.NextRunAt = now.Add(interval)

	if success {
		st.ConsecutiveFailures = 0
	} else {
		st.ConsecutiveFailures++
	}

	_ = s.save()
}

// GetState returns a copy pointer of the state for jobID, or nil if unknown.
func (s *Scheduler) GetState(jobID string) *ScheduleState {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.states[jobID]
	if !ok {
		return nil
	}
	copy := *st
	return &copy
}

// ToEvent converts a ScheduleJob into a normalised Event ready for routing.
func (s *Scheduler) ToEvent(job models.ScheduleJob) models.Event {
	payload := make(map[string]any)
	for k, v := range job.Dispatch.Input {
		payload[k] = v
	}
	payload["job"] = job.ID

	return models.Event{
		Source:   "schedule",
		SourceID: job.ID,
		Payload:  payload,
	}
}

// --- internal helpers ---

// stateFor returns the ScheduleState for jobID, creating a zero value if none
// exists. Caller must hold s.mu.
func (s *Scheduler) stateFor(jobID string) *ScheduleState {
	if st, ok := s.states[jobID]; ok {
		return st
	}
	st := &ScheduleState{}
	s.states[jobID] = st
	return st
}

// intervalFor returns the parsed duration for jobID, defaulting to 1 minute
// on parse errors or missing jobs. Caller must hold s.mu.
func (s *Scheduler) intervalFor(jobID string) time.Duration {
	for _, job := range s.jobs {
		if job.ID == jobID {
			d, err := time.ParseDuration(job.Every)
			if err != nil || d <= 0 {
				return time.Minute
			}
			return d
		}
	}
	return time.Minute
}

// save writes the current states map to stateFile as JSON.
// Caller must hold s.mu.
func (s *Scheduler) save() error {
	data, err := json.MarshalIndent(s.states, "", "  ")
	if err != nil {
		return fmt.Errorf("scheduler: marshal state: %w", err)
	}

	dir := filepath.Dir(s.stateFile)
	tmp, err := os.CreateTemp(dir, ".tmp-schedules-")
	if err != nil {
		return fmt.Errorf("scheduler: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("scheduler: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("scheduler: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.stateFile); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("scheduler: rename temp to dest: %w", err)
	}
	return nil
}

// load reads state from stateFile. A missing file is treated as empty state.
func (s *Scheduler) load() error {
	data, err := os.ReadFile(s.stateFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("scheduler: read state file: %w", err)
	}
	if err := json.Unmarshal(data, &s.states); err != nil {
		return fmt.Errorf("scheduler: unmarshal state file: %w", err)
	}
	return nil
}
