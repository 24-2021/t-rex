package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/api"
	"github.com/t-rex-ai/t-rex/internal/config"
	"github.com/t-rex-ai/t-rex/internal/session"
)

func TestTokenBarAndPercent(t *testing.T) {
	if got := percentOf(50000, 200000); got != 25 {
		t.Fatalf("percentOf got %v, want 25", got)
	}
	if got := tokenBar(25, 8); got != "[##......]" {
		t.Fatalf("tokenBar got %q", got)
	}
	if got := tokenBar(150, 4); got != "[####]" {
		t.Fatalf("tokenBar should clamp at full, got %q", got)
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := map[int]string{
		999:     "999",
		1200:    "1.2k",
		200000:  "200.0k",
		1200000: "1.2m",
	}
	for input, want := range tests {
		if got := formatTokenCount(input); got != want {
			t.Fatalf("formatTokenCount(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestLatestInputTokens(t *testing.T) {
	messages := []session.SessionMessage{
		{Usage: &api.Usage{InputTokens: 100, OutputTokens: 5}},
		{Usage: &api.Usage{InputTokens: 250, OutputTokens: 10}},
	}
	if got := latestInputTokens(50, messages); got != 250 {
		t.Fatalf("latestInputTokens got %d", got)
	}
	if got := latestInputTokens(50, nil); got != 50 {
		t.Fatalf("latestInputTokens fallback got %d", got)
	}
}

func TestTokenContextLine(t *testing.T) {
	line := (&REPL{config: nil}).tokenContextLine(&api.Usage{InputTokens: 50000, OutputTokens: 1200})
	for _, want := range []string{"context:", "25.0%", "in=50.0k", "out=1.2k"} {
		if !strings.Contains(line, want) {
			t.Fatalf("token context line missing %q: %q", want, line)
		}
	}
}

func TestTokenContextLineFallsBackToEstimate(t *testing.T) {
	line := (&REPL{config: nil}).tokenContextLine(nil)
	for _, want := range []string{"context:", "current=~0", "200.0k"} {
		if !strings.Contains(line, want) {
			t.Fatalf("estimated token context line missing %q: %q", want, line)
		}
	}
}

func TestEstimateTokensFromBytes(t *testing.T) {
	if got := estimateTokensFromBytes(4000); got != 1000 {
		t.Fatalf("estimateTokensFromBytes got %d", got)
	}
	if got := estimateTokensFromBytes(1); got != 1 {
		t.Fatalf("estimateTokensFromBytes minimum got %d", got)
	}
}

func TestMessagesForRequestDropsOldTurnsToFitBudget(t *testing.T) {
	repl := &REPL{config: &config.Config{AI: config.AIConfig{ContextWindow: 1200, MaxTokens: 128}}}
	messages := []api.Message{
		{Role: "user", Content: strings.Repeat("old ", 2000)},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "new request"},
	}

	got := repl.messagesForRequest(messages, "system")
	if len(got) != 1 || got[0].Content != "new request" {
		t.Fatalf("messagesForRequest did not keep the newest turn only: %#v", got)
	}
}

func TestMessagesForRequestTruncatesOversizedToolResult(t *testing.T) {
	repl := &REPL{config: &config.Config{AI: config.AIConfig{ContextWindow: 4000, MaxTokens: 256}}}
	largeOutput := strings.Repeat("tool output line\n", 2000)
	messages := []api.Message{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", Content: []api.ContentBlock{{Type: "tool_use", ID: "t1", Name: "Shell"}}},
		{Role: "user", Content: []api.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Content: largeOutput}}},
	}

	got := repl.messagesForRequest(messages, "system")
	blocks, ok := got[len(got)-1].Content.([]api.ContentBlock)
	if !ok || len(blocks) != 1 {
		t.Fatalf("tool result blocks were not preserved: %#v", got)
	}
	if len(blocks[0].Content) >= len(largeOutput) {
		t.Fatalf("tool result was not truncated: got %d, want less than %d", len(blocks[0].Content), len(largeOutput))
	}
	if !strings.Contains(blocks[0].Content, "tool result truncated for context") {
		t.Fatalf("truncated tool result missing marker: %q", blocks[0].Content)
	}
}

func TestAPIErrorClassification(t *testing.T) {
	if !isContextLimitError(errors.New("request exceeds the context window")) {
		t.Fatal("expected context window error to be classified")
	}
	if isRetryableAPIError(errors.New("request exceeds the context window")) {
		t.Fatal("context limit should use compact path, not generic retry")
	}
	if !isRetryableAPIError(errors.New("API error (503): unavailable")) {
		t.Fatal("expected 503 to be retryable")
	}
	if isRetryableAPIError(errors.New("API error (401): unauthorized")) {
		t.Fatal("expected 401 to be non-retryable")
	}
}

func TestParseDSMLTextToolCalls(t *testing.T) {
	text := `Setup 页面可访问，获取 token:

< | DSML | tool_calls>
< | DSML | invoke name="Bash">
< | DSML | parameter name="command" string="true">type setup.html | findstr "user_token"</ | DSML | parameter>
< | DSML | parameter name="timeout" string="false">5000</ | DSML | parameter>
</ | DSML | invoke>
</ | DSML | tool_calls>`

	blocks := parseTextToolCalls(text)
	if len(blocks) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(blocks))
	}
	if blocks[0].Name != "Shell" {
		t.Fatalf("tool name = %q, want Shell", blocks[0].Name)
	}
	input, ok := blocks[0].Input.(map[string]interface{})
	if !ok {
		t.Fatalf("input type = %T", blocks[0].Input)
	}
	if input["command"] != `type setup.html | findstr "user_token"` {
		t.Fatalf("command = %#v", input["command"])
	}
	if input["timeout"] != float64(5000) {
		t.Fatalf("timeout = %#v", input["timeout"])
	}
}

func TestStripTextToolCalls(t *testing.T) {
	text := `before
< | DSML | tool_calls>
< | DSML | invoke name="Bash">< | DSML | parameter name="command" string="true">echo ok</ | DSML | parameter></ | DSML | invoke>
</ | DSML | tool_calls>
after`
	got := stripTextToolCalls(text)
	if strings.Contains(got, "tool_calls") || strings.Contains(got, "invoke") {
		t.Fatalf("tool markup was not stripped: %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("surrounding text was not preserved: %q", got)
	}
}

func TestShouldParseDSMLTextToolCallsEvenWhenCompatibilityDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.AI.AllowTextToolCalls = false
	repl := &REPL{config: cfg}
	if !repl.shouldParseTextToolCalls(`< | DSML | tool_calls>< | DSML | invoke name="Shell"></ | DSML | invoke></ | DSML | tool_calls>`) {
		t.Fatal("expected DSML tool calls to be parsed as a fallback")
	}
	if repl.shouldParseTextToolCalls("ordinary answer without tool markup") {
		t.Fatal("ordinary text should not trigger text tool parsing")
	}
}

func TestParseDSMLTextToolCallsWithExtraPipeSpacing(t *testing.T) {
	text := `< | DSML | tool_calls>
< | DSML | invoke name="Read">
< | DSML | parameter name="file_path" string="true">admin/controller/Admin.php</ | | DSML | | parameter>
< | DSML | parameter name="limit" string="false">100</ | | DSML | | parameter>
</ | | DSML | | invoke>
</ | | DSML | | tool_calls>`

	blocks := parseTextToolCalls(text)
	if len(blocks) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(blocks))
	}
	if blocks[0].Name != "Read" {
		t.Fatalf("tool name = %q, want Read", blocks[0].Name)
	}
	input, ok := blocks[0].Input.(map[string]interface{})
	if !ok {
		t.Fatalf("input type = %T", blocks[0].Input)
	}
	if input["file_path"] != "admin/controller/Admin.php" {
		t.Fatalf("file_path = %#v", input["file_path"])
	}
	if input["limit"] != float64(100) {
		t.Fatalf("limit = %#v", input["limit"])
	}
}

func TestEffectiveMaxToolRoundsExpandsCodeAuditDefault(t *testing.T) {
	cfg := config.Default()
	cfg.AI.WorkMode = "code-audit"
	cfg.AI.MaxToolRounds = 16
	repl := &REPL{config: cfg}
	if got := repl.effectiveMaxToolRounds(); got != 48 {
		t.Fatalf("effective max tool rounds = %d, want 48", got)
	}

	cfg.AI.MaxToolRounds = 80
	if got := repl.effectiveMaxToolRounds(); got != 80 {
		t.Fatalf("explicit max tool rounds = %d, want 80", got)
	}
}
