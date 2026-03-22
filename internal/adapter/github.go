package adapter

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/tykimos/assiharness/internal/models"
)

// GitHubAdapter wraps the gh CLI tool to interact with GitHub.
type GitHubAdapter struct {
	owner string
	repo  string
}

// NewGitHubAdapter constructs a GitHubAdapter from the given config.
func NewGitHubAdapter(cfg models.GitHubConfig) *GitHubAdapter {
	return &GitHubAdapter{
		owner: cfg.Owner,
		repo:  cfg.Repo,
	}
}

// runGH executes gh with the provided arguments and returns stdout.
// On non-zero exit it returns an error containing stderr.
func (a *GitHubAdapter) runGH(args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return out, nil
}

// ghIssue is the raw JSON shape returned by gh issue/pr list.
type ghIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// repoFlag returns the "owner/repo" string used for --repo.
func (a *GitHubAdapter) repoFlag() string {
	return a.owner + "/" + a.repo
}

// buildLabelArgs appends --label <L> flags for each label.
func buildLabelArgs(base []string, labels []string) []string {
	for _, l := range labels {
		base = append(base, "--label", l)
	}
	return base
}

// parseEvents converts a slice of ghIssue into []models.Event.
func parseEvents(raw []ghIssue, source string) []models.Event {
	events := make([]models.Event, 0, len(raw))
	for _, item := range raw {
		labelNames := make([]string, 0, len(item.Labels))
		for _, lbl := range item.Labels {
			labelNames = append(labelNames, lbl.Name)
		}
		events = append(events, models.Event{
			Source:   source,
			SourceID: strconv.Itoa(item.Number),
			Labels:   labelNames,
			Payload: map[string]any{
				"title": item.Title,
				"body":  item.Body,
			},
		})
	}
	return events
}

// ListIssues fetches open GitHub issues matching all given labels.
func (a *GitHubAdapter) ListIssues(labels []string) ([]models.Event, error) {
	args := buildLabelArgs(
		[]string{"issue", "list", "--repo", a.repoFlag(),
			"--json", "number,title,body,labels",
			"--limit", "100"},
		labels,
	)
	out, err := a.runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("ListIssues: %w", err)
	}
	var raw []ghIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("ListIssues: parse JSON: %w", err)
	}
	return parseEvents(raw, "github_issue"), nil
}

// ListPRs fetches open pull requests matching all given labels.
func (a *GitHubAdapter) ListPRs(labels []string) ([]models.Event, error) {
	args := buildLabelArgs(
		[]string{"pr", "list", "--repo", a.repoFlag(),
			"--json", "number,title,body,labels",
			"--limit", "100"},
		labels,
	)
	out, err := a.runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("ListPRs: %w", err)
	}
	var raw []ghIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("ListPRs: parse JSON: %w", err)
	}
	return parseEvents(raw, "github_pr"), nil
}

// AddLabels adds the given labels to an issue.
func (a *GitHubAdapter) AddLabels(issueNum int, labels []string) error {
	_, err := a.runGH(
		"issue", "edit", strconv.Itoa(issueNum),
		"--repo", a.repoFlag(),
		"--add-label", strings.Join(labels, ","),
	)
	if err != nil {
		return fmt.Errorf("AddLabels: %w", err)
	}
	return nil
}

// RemoveLabels removes the given labels from an issue.
func (a *GitHubAdapter) RemoveLabels(issueNum int, labels []string) error {
	_, err := a.runGH(
		"issue", "edit", strconv.Itoa(issueNum),
		"--repo", a.repoFlag(),
		"--remove-label", strings.Join(labels, ","),
	)
	if err != nil {
		return fmt.Errorf("RemoveLabels: %w", err)
	}
	return nil
}

// AddComment posts a comment on an issue.
func (a *GitHubAdapter) AddComment(issueNum int, body string) error {
	_, err := a.runGH(
		"issue", "comment", strconv.Itoa(issueNum),
		"--repo", a.repoFlag(),
		"--body", body,
	)
	if err != nil {
		return fmt.Errorf("AddComment: %w", err)
	}
	return nil
}

// SetAssignee assigns a user to an issue.
func (a *GitHubAdapter) SetAssignee(issueNum int, user string) error {
	_, err := a.runGH(
		"issue", "edit", strconv.Itoa(issueNum),
		"--repo", a.repoFlag(),
		"--add-assignee", user,
	)
	if err != nil {
		return fmt.Errorf("SetAssignee: %w", err)
	}
	return nil
}
