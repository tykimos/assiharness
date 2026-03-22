package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/tykimos/assiharness/internal/models"
)

// ClaudeRunner executes tasks by invoking the Claude CLI.
type ClaudeRunner struct {
	binary       string
	outputFormat string
}

// NewClaudeRunner creates a ClaudeRunner from the given ClaudeConfig.
func NewClaudeRunner(cfg models.ClaudeConfig) *ClaudeRunner {
	binary := cfg.Binary
	if binary == "" {
		binary = "claude"
	}
	outputFormat := cfg.DefaultOutputFormat
	if outputFormat == "" {
		outputFormat = "json"
	}
	return &ClaudeRunner{
		binary:       binary,
		outputFormat: outputFormat,
	}
}

// Run executes the task using the Claude CLI and returns the result.
func (r *ClaudeRunner) Run(ctx context.Context, agent models.AgentConfig, task models.Task) (models.RunResult, error) {
	args := []string{"-p", "--output-format", r.outputFormat}

	// Worktree identifier from task ID.
	args = append(args, "--worktree", task.ID)

	// Optional system prompt file.
	if agent.PromptFile != "" {
		args = append(args, "--append-system-prompt-file", agent.PromptFile)
	}

	// Allowed tools.
	if len(agent.AllowedTools) > 0 {
		args = append(args, "--tools", strings.Join(agent.AllowedTools, ","))
	}

	// Auto-approve tools.
	if len(agent.AutoApproveTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(agent.AutoApproveTools, ","))
	}

	// Disallowed tools.
	if len(agent.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(agent.DisallowedTools, ","))
	}

	// Extra flags from agent config.
	args = append(args, agent.ExtraFlags...)

	// Build the instruction text from the task event payload.
	instruction := buildInstruction(task)

	log.Printf("claude runner: executing: %s %s", r.binary, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdin = strings.NewReader(instruction)
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	output, err := cmd.Output()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			log.Printf("claude runner: exit code %d, stderr: %s", exitCode, stderrBuf.String())
		} else {
			return models.RunResult{
				Success:  false,
				Output:   fmt.Sprintf("exec error: %v, stderr: %s", err, stderrBuf.String()),
				ExitCode: -1,
			}, fmt.Errorf("failed to execute claude: %w", err)
		}
	}

	rawOutput := string(output)

	// Parse JSON output to extract the result field.
	var parsed map[string]any
	jsonResult := map[string]any{}
	resultStr := ""

	if jsonErr := json.Unmarshal(output, &parsed); jsonErr == nil {
		jsonResult = parsed
		if v, ok := parsed["result"]; ok {
			resultStr = fmt.Sprintf("%v", v)
		} else {
			resultStr = rawOutput
		}
	} else {
		resultStr = rawOutput
	}

	// Dual judgment: both exit code and JSON result field must indicate success.
	success := exitCode == 0 && isJSONSuccess(parsed)

	return models.RunResult{
		Success:    success,
		Output:     resultStr,
		ExitCode:   exitCode,
		JSONResult: jsonResult,
	}, nil
}

// buildInstruction constructs the prompt text from the task event payload.
// It uses the "title" and "body" fields when present (GitHub issue/PR pattern).
func buildInstruction(task models.Task) string {
	payload := task.Event.Payload
	if payload == nil {
		return ""
	}

	var parts []string
	if title, ok := payload["title"]; ok && title != nil {
		parts = append(parts, fmt.Sprintf("%v", title))
	}
	if body, ok := payload["body"]; ok && body != nil {
		if s := fmt.Sprintf("%v", body); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	return ""
}

// isJSONSuccess checks whether the parsed JSON output indicates success.
// Returns true when the "result" field is absent (non-error output) or is not
// an error indicator. Returns false when "is_error" is true.
func isJSONSuccess(parsed map[string]any) bool {
	if parsed == nil {
		return true
	}
	if v, ok := parsed["is_error"]; ok {
		switch val := v.(type) {
		case bool:
			return !val
		case string:
			return val != "true"
		}
	}
	return true
}
