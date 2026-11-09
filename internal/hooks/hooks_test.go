package hooks

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/config"
)

func TestMatches(t *testing.T) {
	tests := []struct {
		patterns string
		value    string
		want     bool
	}{
		{"", "Bash", true},
		{"*", "Read", true},
		{"Bash, Write", "write", true},
		{"mcp__*", "mcp__github__search", true},
		{"Read,Glob", "Edit", false},
	}
	for _, tt := range tests {
		if got := matches(tt.patterns, tt.value); got != tt.want {
			t.Errorf("matches(%q, %q) = %v, want %v", tt.patterns, tt.value, got, tt.want)
		}
	}
}

func TestRunnerCombinesContextAndBlocks(t *testing.T) {
	if os.Getenv("GO_WANT_HOOK_HELPER") == "1" {
		fmt.Print(`{"continue":false,"reason":"policy denied","additionalContext":"review the policy"}`)
		os.Exit(0)
	}

	t.Setenv("GO_WANT_HOOK_HELPER", "1")
	command := strconv.Quote(os.Args[0]) + " " + strconv.Quote("-test.run=TestRunnerCombinesContextAndBlocks")
	if runtime.GOOS == "windows" {
		command = "& " + command
	}
	cfg := &config.HooksConfig{Hooks: []config.HookConfig{{
		Event:   PreToolUse,
		Matcher: "Bash,Write",
		Command: command,
		Timeout: 10,
		Enabled: true,
	}}}

	result, err := NewRunner(cfg, "session-1", t.TempDir()).Run(context.Background(), PreToolUse, Payload{
		"tool_name":  "Bash",
		"tool_input": map[string]interface{}{"command": "echo ok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Blocked || result.Reason != "policy denied" {
		t.Fatalf("unexpected decision: %#v", result)
	}
	if got := strings.Join(result.AdditionalContext, "\n"); got != "review the policy" {
		t.Fatalf("unexpected context: %q", got)
	}
	if result.Executed != 1 {
		t.Fatalf("executed = %d, want 1", result.Executed)
	}
}
