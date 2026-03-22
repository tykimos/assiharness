package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/tykimos/assiharness/internal/adapter"
	"github.com/tykimos/assiharness/internal/config"
	"github.com/tykimos/assiharness/internal/heartbeat"
	"github.com/tykimos/assiharness/internal/logger"
	"github.com/tykimos/assiharness/internal/models"
	"github.com/tykimos/assiharness/internal/recovery"
	"github.com/tykimos/assiharness/internal/registry"
	"github.com/tykimos/assiharness/internal/router"
	"github.com/tykimos/assiharness/internal/runner"
	"github.com/tykimos/assiharness/internal/scheduler"
	"github.com/tykimos/assiharness/internal/state"
	"github.com/tykimos/assiharness/internal/worktree"
)

// Orchestrator is the main execution loop that ties all components together.
type Orchestrator struct {
	cfg         *config.Config
	configDir   string
	registry    *registry.Registry
	router      *router.Router
	runner      runner.Runner
	worktreeMgr *worktree.Manager
	store       *state.Store
	github      *adapter.GitHubAdapter
	scheduler   *scheduler.Scheduler
	recovery    *recovery.Engine
	heartbeat   *heartbeat.Monitor
	watcher     *config.Watcher
	log         *slog.Logger

	once   bool
	dryRun bool

	mu         sync.Mutex
	activeRuns map[string]int
	wg         sync.WaitGroup
}

// New creates an Orchestrator from the loaded config.
func New(cfg *config.Config, configDir string, once, dryRun bool) (*Orchestrator, error) {
	stateDir := cfg.Runtime.StateDir
	if stateDir == "" {
		stateDir = "state"
	}
	logsDir := cfg.Runtime.LogsDir
	if logsDir == "" {
		logsDir = "logs"
	}

	// Setup structured logging.
	if err := logger.Setup(cfg.Runtime.LogLevel, logsDir); err != nil {
		return nil, fmt.Errorf("orchestrator: init logger: %w", err)
	}
	log := logger.WithComponent("orchestrator")

	reg := registry.NewRegistry(cfg.Agents)
	rt := router.NewRouter(cfg.Routes.Routes)
	cr := runner.NewClaudeRunner(cfg.Runtime.Claude)
	gh := adapter.NewGitHubAdapter(cfg.Runtime.GitHub)

	store, err := state.NewStore(stateDir)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: init state store: %w", err)
	}

	wtMgr, err := worktree.NewManager(stateDir)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: init worktree manager: %w", err)
	}

	sched, err := scheduler.NewScheduler(cfg.Schedules.Jobs, stateDir)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: init scheduler: %w", err)
	}

	staleTimeout := parseDuration(cfg.Policies.Recovery.StaleRunningTimeout, 1*time.Hour)
	recov := recovery.NewEngine(store, wtMgr, gh, staleTimeout)

	hbTimeout := parseDuration(cfg.Policies.Recovery.StaleRunningTimeout, 1*time.Hour)
	hbMon, err := heartbeat.NewMonitor(stateDir, hbTimeout)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: init heartbeat: %w", err)
	}

	return &Orchestrator{
		cfg:         cfg,
		configDir:   configDir,
		registry:    reg,
		router:      rt,
		runner:      cr,
		worktreeMgr: wtMgr,
		store:       store,
		github:      gh,
		scheduler:   sched,
		recovery:    recov,
		heartbeat:   hbMon,
		log:         log,
		once:        once,
		dryRun:      dryRun,
		activeRuns:  make(map[string]int),
	}, nil
}

// Run starts the main loop. It blocks until ctx is cancelled or --once completes.
func (o *Orchestrator) Run(ctx context.Context) error {
	pollInterval := parseDuration(o.cfg.Runtime.PollInterval, 30*time.Second)

	o.log.Info("starting",
		"poll_interval", pollInterval.String(),
		"once", o.once,
		"dry_run", o.dryRun,
	)

	// Start config hot-reload watcher (unless --once).
	if !o.once {
		o.watcher = config.NewWatcher(o.configDir, 10*time.Second, o.cfg, func(newCfg *config.Config) {
			o.log.Info("config reloaded, updating components")
			o.mu.Lock()
			o.cfg = newCfg
			o.registry = registry.NewRegistry(newCfg.Agents)
			o.router = router.NewRouter(newCfg.Routes.Routes)
			o.mu.Unlock()
		})
		o.watcher.Start(ctx)
	}

	_ = o.store.SaveRuntimeState(state.RuntimeState{
		StartedAt: time.Now(),
	})

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	if err := o.tick(ctx); err != nil {
		o.log.Error("tick error", "error", err)
	}
	if o.once {
		o.log.Info("--once mode, waiting for active workers to finish")
		o.wg.Wait()
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			o.log.Info("shutting down, waiting for active workers")
			o.wg.Wait()
			return nil
		case <-ticker.C:
			if err := o.tick(ctx); err != nil {
				o.log.Error("tick error", "error", err)
			}
		}
	}
}

// tick performs a single iteration of the main loop.
func (o *Orchestrator) tick(ctx context.Context) error {
	o.log.Debug("tick start")

	// 1. Recovery: stale runs and orphaned worktrees.
	if err := o.recovery.RecoverStaleRuns(); err != nil {
		o.log.Warn("recovery stale runs failed", "error", err)
	}
	if err := o.recovery.CleanOrphanedWorktrees(); err != nil {
		o.log.Warn("recovery orphaned worktrees failed", "error", err)
	}

	// 2. Collect events from GitHub.
	events, err := o.collectEvents()
	if err != nil {
		return fmt.Errorf("collect events: %w", err)
	}

	// 3. Collect due scheduled jobs and convert to events.
	now := time.Now()
	dueJobs := o.scheduler.DueJobs(now)
	for _, job := range dueJobs {
		events = append(events, o.scheduler.ToEvent(job))
	}

	if len(events) == 0 {
		o.log.Debug("no events to process")
		return nil
	}

	o.log.Info("processing events", "count", len(events))

	// 4. Route each event to an agent and execute.
	for _, event := range events {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		agentID, input, matched := o.router.Match(event)
		if !matched {
			o.log.Debug("no route for event", "source", event.Source, "source_id", event.SourceID)
			continue
		}

		agent, ok := o.registry.Get(agentID)
		if !ok {
			o.log.Warn("agent not found", "agent_id", agentID)
			continue
		}
		if !agent.Enabled {
			o.log.Debug("agent disabled", "agent_id", agentID)
			continue
		}

		if o.dryRun {
			o.log.Info("[DRY-RUN] matched",
				"source", event.Source,
				"source_id", event.SourceID,
				"agent", agentID,
			)
			continue
		}

		if !o.canRun(agent) {
			o.log.Info("agent at concurrency limit",
				"agent_id", agentID,
				"max_parallel", agent.Concurrency.MaxParallel,
			)
			continue
		}

		task := models.Task{
			ID:        fmt.Sprintf("%s-%s", agentID, event.SourceID),
			AgentID:   agentID,
			Event:     event,
			Input:     input,
			CreatedAt: time.Now(),
		}

		// For schedule events, mark as running.
		isSchedule := event.Source == "schedule"
		if isSchedule {
			o.scheduler.MarkRunning(event.SourceID)
		}

		// For GitHub events, claim the issue.
		issueNum := 0
		if !isSchedule {
			issueNum, _ = strconv.Atoi(event.SourceID)
			if err := o.claimIssue(issueNum); err != nil {
				o.log.Error("failed to claim issue", "issue", issueNum, "error", err)
				continue
			}
		}

		wtName := o.worktreeMgr.GenerateName(agent, task)
		if o.worktreeMgr.IsOccupied(wtName) {
			o.log.Warn("worktree occupied", "worktree", wtName)
			continue
		}

		run := models.Run{
			ID:        fmt.Sprintf("run-%s-%d", task.ID, time.Now().UnixMilli()),
			AgentID:   agentID,
			TaskID:    task.ID,
			Status:    "running",
			StartedAt: time.Now(),
			Worktree:  wtName,
		}

		if err := o.worktreeMgr.Claim(wtName, run.ID); err != nil {
			o.log.Error("failed to claim worktree", "worktree", wtName, "error", err)
			continue
		}

		_ = o.store.SaveRun(run)
		o.incrementActive(agentID)

		o.wg.Add(1)
		go func(evt models.Event, isSchedule bool) {
			defer o.wg.Done()
			o.executeWorker(ctx, agent, task, run, wtName, issueNum, isSchedule)
		}(event, isSchedule)
	}

	return nil
}

// collectEvents gathers GitHub issues and PRs that have the cc:ready label.
func (o *Orchestrator) collectEvents() ([]models.Event, error) {
	var allEvents []models.Event

	issues, err := o.github.ListIssues([]string{"cc:ready"})
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	allEvents = append(allEvents, issues...)

	prs, err := o.github.ListPRs([]string{"cc:ready"})
	if err != nil {
		return nil, fmt.Errorf("list PRs: %w", err)
	}
	allEvents = append(allEvents, prs...)

	return allEvents, nil
}

// claimIssue implements the lock strategy: remove cc:ready, add cc:running, assignee, comment.
func (o *Orchestrator) claimIssue(issueNum int) error {
	if err := o.github.RemoveLabels(issueNum, []string{"cc:ready"}); err != nil {
		return err
	}
	if err := o.github.AddLabels(issueNum, []string{"cc:running"}); err != nil {
		return err
	}
	botUser := o.cfg.Policies.Lock.BotUser
	if botUser != "" {
		_ = o.github.SetAssignee(issueNum, botUser)
	}
	_ = o.github.AddComment(issueNum, fmt.Sprintf("claimed by AssiHarness at %s", time.Now().Format(time.RFC3339)))
	return nil
}

// executeWorker runs a single worker and handles the result.
func (o *Orchestrator) executeWorker(ctx context.Context, agent models.AgentConfig, task models.Task, run models.Run, wtName string, issueNum int, isSchedule bool) {
	defer func() {
		o.decrementActive(agent.ID)
		_ = o.heartbeat.Remove(run.ID)
		if err := o.worktreeMgr.Cleanup(wtName); err != nil {
			o.log.Warn("worktree cleanup failed", "worktree", wtName, "error", err)
		}
	}()

	// Initial heartbeat.
	_ = o.heartbeat.Beat(run.ID, agent.ID)

	execCtx := ctx
	if agent.Timeouts.Execution != "" {
		timeout := parseDuration(agent.Timeouts.Execution, 30*time.Minute)
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	o.log.Info("running worker",
		"agent", agent.ID,
		"task", task.ID,
		"worktree", wtName,
	)

	// Heartbeat goroutine during execution.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				_ = o.heartbeat.Beat(run.ID, agent.ID)
			}
		}
	}()

	result, err := o.runner.Run(execCtx, agent, task)
	hbCancel()

	run.FinishedAt = time.Now()
	run.Duration = run.FinishedAt.Sub(run.StartedAt).String()
	run.Result = result

	if err != nil {
		o.log.Error("worker failed", "agent", agent.ID, "task", task.ID, "error", err)
		run.Status = "failed"
	} else if result.Success {
		o.log.Info("worker succeeded", "agent", agent.ID, "task", task.ID)
		run.Status = "success"
	} else {
		o.log.Warn("worker returned failure", "agent", agent.ID, "task", task.ID)
		run.Status = "failed"
	}

	_ = o.store.SaveRun(run)

	// Mark schedule job completed.
	if isSchedule {
		o.scheduler.MarkCompleted(task.Event.SourceID, run.Status == "success")
	}

	// Apply GitHub labels for non-schedule events.
	if !isSchedule && issueNum > 0 {
		o.applyOutputLabels(issueNum, agent, run.Status)
	}
}

// applyOutputLabels adds/removes labels based on the run result.
func (o *Orchestrator) applyOutputLabels(issueNum int, agent models.AgentConfig, status string) {
	var action models.LabelAction
	if status == "success" {
		action = agent.Outputs.OnSuccess
	} else {
		action = agent.Outputs.OnFailure
	}

	if len(action.AddLabels) > 0 {
		_ = o.github.AddLabels(issueNum, action.AddLabels)
	}
	if len(action.RemoveLabels) > 0 {
		_ = o.github.RemoveLabels(issueNum, action.RemoveLabels)
	}

	comment := fmt.Sprintf("AssiHarness: agent `%s` finished with status **%s**", agent.ID, status)
	_ = o.github.AddComment(issueNum, comment)
}

func (o *Orchestrator) canRun(agent models.AgentConfig) bool {
	if agent.Concurrency.MaxParallel <= 0 {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.activeRuns[agent.ID] < agent.Concurrency.MaxParallel
}

func (o *Orchestrator) incrementActive(agentID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.activeRuns[agentID]++
}

func (o *Orchestrator) decrementActive(agentID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.activeRuns[agentID] > 0 {
		o.activeRuns[agentID]--
	}
}

func parseDuration(s string, defaultVal time.Duration) time.Duration {
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}
