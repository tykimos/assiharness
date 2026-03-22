package recovery

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/tykimos/assiharness/internal/adapter"
	"github.com/tykimos/assiharness/internal/state"
	"github.com/tykimos/assiharness/internal/worktree"
)

// Engine handles recovery of stale runs and cleanup of orphaned worktrees.
type Engine struct {
	store        *state.Store
	worktreeMgr  *worktree.Manager
	github       *adapter.GitHubAdapter
	staleTimeout time.Duration
}

// NewEngine creates a new recovery Engine.
func NewEngine(store *state.Store, wtMgr *worktree.Manager, gh *adapter.GitHubAdapter, staleTimeout time.Duration) *Engine {
	return &Engine{
		store:       store,
		worktreeMgr: wtMgr,
		github:      gh,
		staleTimeout: staleTimeout,
	}
}

// parseIssueNum extracts the issue number N from a TaskID of the form "agentid-N".
// Returns an error if the format is unexpected or N is not a valid integer.
func parseIssueNum(taskID string) (int, error) {
	idx := strings.LastIndex(taskID, "-")
	if idx < 0 || idx == len(taskID)-1 {
		return 0, fmt.Errorf("recovery: taskID %q has no trailing numeric segment", taskID)
	}
	n, err := strconv.Atoi(taskID[idx+1:])
	if err != nil {
		return 0, fmt.Errorf("recovery: taskID %q trailing segment is not a number: %w", taskID, err)
	}
	return n, nil
}

// RecoverStaleRuns finds all runs with Status="running" whose StartedAt is
// older than staleTimeout, marks them as failed, cleans up their worktrees,
// and updates GitHub labels and comments accordingly.
func (e *Engine) RecoverStaleRuns() error {
	runs, err := e.store.ListRuns()
	if err != nil {
		return fmt.Errorf("recovery: list runs: %w", err)
	}

	now := time.Now()
	for _, run := range runs {
		if run.Status != "running" {
			continue
		}
		if run.StartedAt.Add(e.staleTimeout).After(now) {
			continue
		}

		log.Printf("recovery: stale run detected run_id=%s agent=%s task=%s started_at=%s",
			run.ID, run.AgentID, run.TaskID, run.StartedAt.Format(time.RFC3339))

		// Mark run as failed.
		run.Status = "failed"
		run.FinishedAt = now
		if err := e.store.SaveRun(run); err != nil {
			log.Printf("recovery: save run %s: %v", run.ID, err)
		}

		// Cleanup worktree if present.
		if run.Worktree != "" {
			if err := e.worktreeMgr.Cleanup(run.Worktree); err != nil {
				log.Printf("recovery: cleanup worktree %s for run %s: %v", run.Worktree, run.ID, err)
			} else {
				log.Printf("recovery: cleaned worktree %s for run %s", run.Worktree, run.ID)
			}
		}

		// Parse issue number from TaskID.
		issueNum, err := parseIssueNum(run.TaskID)
		if err != nil {
			log.Printf("recovery: parse issue number from task %s: %v", run.TaskID, err)
			continue
		}

		// Update GitHub labels.
		if err := e.github.RemoveLabels(issueNum, []string{"cc:running"}); err != nil {
			log.Printf("recovery: remove labels issue=%d run=%s: %v", issueNum, run.ID, err)
		}
		if err := e.github.AddLabels(issueNum, []string{"cc:failed"}); err != nil {
			log.Printf("recovery: add labels issue=%d run=%s: %v", issueNum, run.ID, err)
		}
		if err := e.github.AddComment(issueNum, "recovered by AssiHarness: stale run timeout"); err != nil {
			log.Printf("recovery: add comment issue=%d run=%s: %v", issueNum, run.ID, err)
		}

		log.Printf("recovery: stale run recovered run_id=%s issue=%d", run.ID, issueNum)
	}

	return nil
}

// CleanOrphanedWorktrees finds runs that are not running but still have a
// non-empty Worktree field, and calls Cleanup on each.
func (e *Engine) CleanOrphanedWorktrees() error {
	runs, err := e.store.ListRuns()
	if err != nil {
		return fmt.Errorf("recovery: list runs for orphan check: %w", err)
	}

	for _, run := range runs {
		if run.Status == "running" {
			continue
		}
		if run.Worktree == "" {
			continue
		}

		log.Printf("recovery: orphaned worktree found run_id=%s worktree=%s status=%s",
			run.ID, run.Worktree, run.Status)

		if err := e.worktreeMgr.Cleanup(run.Worktree); err != nil {
			log.Printf("recovery: cleanup orphaned worktree %s run=%s: %v", run.Worktree, run.ID, err)
		} else {
			log.Printf("recovery: cleaned orphaned worktree %s run=%s", run.Worktree, run.ID)
		}
	}

	return nil
}

