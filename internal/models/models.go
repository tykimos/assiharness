package models

import "time"

// Event represents a normalized event from any source.
type Event struct {
	Source   string         `json:"source"`    // github_issue, github_pr, ci_result, schedule
	SourceID string        `json:"source_id"` // issue number, PR number, job id
	Labels  []string       `json:"labels"`
	Status  string         `json:"status"`
	Payload map[string]any `json:"payload"`
}

// AgentConfig defines an agent loaded from config/agents/*.yml.
type AgentConfig struct {
	ID               string           `yaml:"id"               json:"id"`
	Type             string           `yaml:"type"             json:"type"`   // claude_worker, script, webhook
	Enabled          bool             `yaml:"enabled"          json:"enabled"`
	PromptFile       string           `yaml:"prompt_file"      json:"prompt_file"`
	AllowedTools     []string         `yaml:"allowed_tools"    json:"allowed_tools"`
	AutoApproveTools []string         `yaml:"auto_approve_tools" json:"auto_approve_tools"`
	DisallowedTools  []string         `yaml:"disallowed_tools" json:"disallowed_tools"`
	ExtraFlags       []string         `yaml:"extra_flags"      json:"extra_flags"`
	Worktree         WorktreeConfig   `yaml:"worktree"         json:"worktree"`
	Concurrency      ConcurrencyConfig `yaml:"concurrency"     json:"concurrency"`
	Timeouts         TimeoutConfig    `yaml:"timeouts"         json:"timeouts"`
	Retries          RetryConfig      `yaml:"retries"          json:"retries"`
	CanCreateIssues  bool             `yaml:"can_create_issues" json:"can_create_issues"`
	IssueCreation    IssueCreationConfig `yaml:"issue_creation_rules" json:"issue_creation_rules"`
	Outputs          OutputConfig     `yaml:"outputs"          json:"outputs"`
}

// WorktreeConfig defines worktree isolation strategy.
type WorktreeConfig struct {
	Mode    string `yaml:"mode"    json:"mode"`    // per_task, shared_role
	Pattern string `yaml:"pattern" json:"pattern"` // e.g. "{agent_id}-{task_id}"
}

// ConcurrencyConfig defines parallel execution limits.
type ConcurrencyConfig struct {
	MaxParallel int `yaml:"max_parallel" json:"max_parallel"`
}

// TimeoutConfig defines execution time limits.
type TimeoutConfig struct {
	Execution string `yaml:"execution" json:"execution"` // e.g. "30m"
	Claim     string `yaml:"claim"     json:"claim"`     // e.g. "5m"
}

// RetryConfig defines retry behavior.
type RetryConfig struct {
	MaxAttempts int    `yaml:"max_attempts" json:"max_attempts"`
	Backoff     string `yaml:"backoff"      json:"backoff"`
}

// IssueCreationConfig defines rules for agent-created issues.
type IssueCreationConfig struct {
	CheckDuplicates   bool     `yaml:"check_duplicates"   json:"check_duplicates"`
	RequiredLabels    []string `yaml:"required_labels"    json:"required_labels"`
	SeverityThreshold string   `yaml:"severity_threshold" json:"severity_threshold"`
}

// OutputConfig defines label changes on success/failure.
type OutputConfig struct {
	OnSuccess LabelAction `yaml:"on_success" json:"on_success"`
	OnFailure LabelAction `yaml:"on_failure" json:"on_failure"`
}

// LabelAction defines labels to add/remove.
type LabelAction struct {
	AddLabels    []string `yaml:"add_labels"    json:"add_labels"`
	RemoveLabels []string `yaml:"remove_labels" json:"remove_labels"`
}

// RouteRule maps events to agents.
type RouteRule struct {
	ID       string        `yaml:"id"       json:"id"`
	When     RouteCondition `yaml:"when"    json:"when"`
	Dispatch RouteDispatch `yaml:"dispatch" json:"dispatch"`
	Priority int           `yaml:"priority" json:"priority"` // lower = higher priority, default 100
}

// RouteCondition defines when a route matches.
type RouteCondition struct {
	Source string   `yaml:"source" json:"source"` // github_issue, github_pr, ci_result, schedule
	Labels []string `yaml:"labels" json:"labels"` // AND condition
	Status string   `yaml:"status" json:"status"`
	Job    string   `yaml:"job"    json:"job"`
}

// RouteDispatch defines what to do when a route matches.
type RouteDispatch struct {
	Agent string         `yaml:"agent" json:"agent"`
	Input map[string]any `yaml:"input" json:"input"`
}

// ScheduleJob defines a periodic job.
type ScheduleJob struct {
	ID       string        `yaml:"id"       json:"id"`
	Enabled  bool          `yaml:"enabled"  json:"enabled"`
	Every    string        `yaml:"every"    json:"every"` // e.g. "5m", "1h"
	Dispatch RouteDispatch `yaml:"dispatch" json:"dispatch"`
}

// PolicyConfig defines operational policies.
type PolicyConfig struct {
	Lock          LockPolicy          `yaml:"lock"           json:"lock"`
	Worktree      WorktreePolicy      `yaml:"worktree"       json:"worktree"`
	Recovery      RecoveryPolicy      `yaml:"recovery"       json:"recovery"`
	IssueCreation IssueCreationPolicy `yaml:"issue_creation" json:"issue_creation"`
}

// LockPolicy defines issue claiming strategy.
type LockPolicy struct {
	Strategy string `yaml:"strategy" json:"strategy"` // label_only, assignee_only, label_and_assignee
	BotUser  string `yaml:"bot_user" json:"bot_user"`
}

// WorktreePolicy defines worktree management policies.
type WorktreePolicy struct {
	CleanupAfter string `yaml:"cleanup_after" json:"cleanup_after"`
	MaxTotal     int    `yaml:"max_total"     json:"max_total"`
}

// RecoveryPolicy defines recovery behavior.
type RecoveryPolicy struct {
	StaleRunningTimeout    string `yaml:"stale_running_timeout"    json:"stale_running_timeout"`
	MaxConsecutiveFailures int    `yaml:"max_consecutive_failures" json:"max_consecutive_failures"`
	OrphanCheckInterval    string `yaml:"orphan_check_interval"    json:"orphan_check_interval"`
}

// IssueCreationPolicy defines issue creation limits.
type IssueCreationPolicy struct {
	DuplicateCheck bool               `yaml:"duplicate_check" json:"duplicate_check"`
	LoopPrevention LoopPreventionConfig `yaml:"loop_prevention" json:"loop_prevention"`
}

// LoopPreventionConfig prevents runaway issue creation.
type LoopPreventionConfig struct {
	MaxIssuesPerHour int    `yaml:"max_issues_per_hour" json:"max_issues_per_hour"`
	CooldownOnLimit  string `yaml:"cooldown_on_limit"   json:"cooldown_on_limit"`
}

// RuntimeConfig defines runtime settings.
type RuntimeConfig struct {
	PollInterval string        `yaml:"poll_interval" json:"poll_interval"`
	LogLevel     string        `yaml:"log_level"     json:"log_level"`
	StateDir     string        `yaml:"state_dir"     json:"state_dir"`
	LogsDir      string        `yaml:"logs_dir"      json:"logs_dir"`
	GitHub       GitHubConfig  `yaml:"github"        json:"github"`
	Claude       ClaudeConfig  `yaml:"claude"        json:"claude"`
}

// GitHubConfig defines GitHub connection settings.
type GitHubConfig struct {
	APIURL    string `yaml:"api_url"    json:"api_url"`
	UploadURL string `yaml:"upload_url" json:"upload_url"`
	Owner     string `yaml:"owner"      json:"owner"`
	Repo      string `yaml:"repo"       json:"repo"`
}

// ClaudeConfig defines Claude CLI settings.
type ClaudeConfig struct {
	Binary              string `yaml:"binary"                json:"binary"`
	DefaultOutputFormat string `yaml:"default_output_format" json:"default_output_format"`
	DefaultVerbose      bool   `yaml:"default_verbose"       json:"default_verbose"`
}

// Task represents a unit of work dispatched to an agent.
type Task struct {
	ID        string         `json:"id"`
	AgentID   string         `json:"agent_id"`
	Event     Event          `json:"event"`
	Input     map[string]any `json:"input"`
	CreatedAt time.Time      `json:"created_at"`
}

// Run represents a single execution record.
type Run struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agent_id"`
	TaskID     string    `json:"task_id"`
	Status     string    `json:"status"` // running, success, failed
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Result     RunResult `json:"result"`
	Duration   string    `json:"duration,omitempty"`
	Worktree   string    `json:"worktree"`
}

// RunResult holds the outcome of a worker execution.
type RunResult struct {
	Success    bool           `json:"success"`
	Output     string         `json:"output"`
	ExitCode   int            `json:"exit_code"`
	JSONResult map[string]any `json:"json_result,omitempty"`
}

// RoutesConfig is the top-level structure for routes.yml.
type RoutesConfig struct {
	Routes []RouteRule `yaml:"routes" json:"routes"`
}

// SchedulesConfig is the top-level structure for schedules.yml.
type SchedulesConfig struct {
	Jobs []ScheduleJob `yaml:"jobs" json:"jobs"`
}
