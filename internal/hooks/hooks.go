package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/t-rex-ai/t-rex/internal/config"
)

const (
	SessionStart       = "SessionStart"
	UserPromptSubmit   = "UserPromptSubmit"
	PreToolUse         = "PreToolUse"
	PostToolUse        = "PostToolUse"
	PostToolUseFailure = "PostToolUseFailure"
	Stop               = "Stop"
	SessionEnd         = "SessionEnd"
)

// Payload is serialized to hook stdin. The common fields are added by Runner.
type Payload map[string]interface{}

// Result combines the decisions from every matching hook.
type Result struct {
	Blocked           bool
	Reason            string
	AdditionalContext []string
	SystemMessages    []string
	Executed          int
}

type hookOutput struct {
	Continue           *bool  `json:"continue,omitempty"`
	Decision           string `json:"decision,omitempty"`
	Reason             string `json:"reason,omitempty"`
	AdditionalContext  string `json:"additionalContext,omitempty"`
	SystemMessage      string `json:"systemMessage,omitempty"`
	HookSpecificOutput struct {
		AdditionalContext        string `json:"additionalContext,omitempty"`
		PermissionDecision       string `json:"permissionDecision,omitempty"`
		PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	} `json:"hookSpecificOutput,omitempty"`
}

// Runner executes Claude Code style command hooks from T-REX configuration.
// Hook commands receive JSON on stdin and can return a JSON decision on stdout.
type Runner struct {
	config    *config.HooksConfig
	sessionID string
	project   string
}

func NewRunner(cfg *config.HooksConfig, sessionID, project string) *Runner {
	return &Runner{config: cfg, sessionID: sessionID, project: project}
}

func (r *Runner) Run(ctx context.Context, event string, payload Payload) (Result, error) {
	var combined Result
	if r == nil || r.config == nil {
		return combined, nil
	}

	input := make(Payload, len(payload)+4)
	for key, value := range payload {
		input[key] = value
	}
	input["session_id"] = r.sessionID
	input["cwd"] = r.project
	input["hook_event_name"] = event

	matcherValue := matcherValue(event, input)
	var errs []error
	for _, hook := range r.config.Hooks {
		if !hook.Enabled || !strings.EqualFold(strings.TrimSpace(hook.Event), event) || !matches(hook.Matcher, matcherValue) {
			continue
		}

		if hook.AsyncRewake {
			go func(h config.HookConfig) {
				_, _ = r.runOne(context.Background(), h, input)
			}(hook)
			combined.Executed++
			continue
		}

		output, err := r.runOne(ctx, hook, input)
		combined.Executed++
		if err != nil {
			var blocked *blockedError
			if errors.As(err, &blocked) {
				combined.Blocked = true
				combined.Reason = blocked.Error()
				break
			}
			errs = append(errs, err)
			continue
		}

		if output.AdditionalContext != "" {
			combined.AdditionalContext = append(combined.AdditionalContext, output.AdditionalContext)
		}
		if output.HookSpecificOutput.AdditionalContext != "" {
			combined.AdditionalContext = append(combined.AdditionalContext, output.HookSpecificOutput.AdditionalContext)
		}
		if output.SystemMessage != "" {
			combined.SystemMessages = append(combined.SystemMessages, output.SystemMessage)
		}

		decision := strings.ToLower(strings.TrimSpace(output.Decision))
		permissionDecision := strings.ToLower(strings.TrimSpace(output.HookSpecificOutput.PermissionDecision))
		if (output.Continue != nil && !*output.Continue) || decision == "deny" || decision == "block" || permissionDecision == "deny" {
			combined.Blocked = true
			combined.Reason = firstNonEmpty(output.Reason, output.HookSpecificOutput.PermissionDecisionReason, "blocked by hook")
			break
		}
	}

	return combined, errors.Join(errs...)
}

func (r *Runner) runOne(parent context.Context, hook config.HookConfig, input Payload) (hookOutput, error) {
	var output hookOutput
	timeout := time.Duration(hook.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	data, err := json.Marshal(input)
	if err != nil {
		return output, fmt.Errorf("encode hook input: %w", err)
	}

	name, args := shellCommand(hook.Command)
	cmd := exec.CommandContext(ctx, name, args...)
	if r.project != "" {
		cmd.Dir = r.project
	}
	cmd.Env = append(os.Environ(),
		"T_REX_SESSION_ID="+r.sessionID,
		"T_REX_PROJECT_DIR="+r.project,
		"T_REX_HOOK_EVENT="+fmt.Sprint(input["hook_event_name"]),
	)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("hook %s timed out after %s", hook.Event, timeout)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			return output, &blockedError{reason: firstNonEmpty(strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String()), "hook exited with status 2")}
		}
		return output, fmt.Errorf("hook %s failed: %w%s", hook.Event, err, formatStderr(stderr.String()))
	}

	text := strings.TrimSpace(stdout.String())
	if text == "" {
		return output, nil
	}
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		// Plain stdout is useful context and remains compatible with simple hooks.
		output.AdditionalContext = text
	}
	return output, nil
}

type blockedError struct{ reason string }

func (e *blockedError) Error() string { return e.reason }

func matcherValue(event string, input Payload) string {
	if event == PreToolUse || event == PostToolUse || event == PostToolUseFailure {
		return fmt.Sprint(input["tool_name"])
	}
	return ""
}

func matches(patterns, value string) bool {
	patterns = strings.TrimSpace(patterns)
	if patterns == "" || patterns == "*" {
		return true
	}
	for _, pattern := range strings.Split(patterns, ",") {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		candidate := strings.ToLower(value)
		if pattern == candidate {
			return true
		}
		if ok, err := filepath.Match(pattern, candidate); err == nil && ok {
			return true
		}
	}
	return false
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath("pwsh"); err == nil {
			return path, []string{"-NoProfile", "-NonInteractive", "-Command", command}
		}
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatStderr(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}
