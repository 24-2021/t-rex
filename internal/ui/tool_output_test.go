package ui

import (
	"strings"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/tools"
)

func TestToolResultSummaryForShellIsCompact(t *testing.T) {
	result := &tools.ToolResult{
		Content: `{"output":"long output","exitCode":0}`,
		Output: map[string]interface{}{
			"output":   strings.Repeat("x", 2048),
			"exitCode": 0,
		},
	}
	got := toolResultSummary("Shell", map[string]interface{}{"command": "curl http://example.test"}, result)
	if got != "exit=0, output=2.0 KB" {
		t.Fatalf("unexpected shell summary: %q", got)
	}
}

func TestToolStartLabelTruncatesCommand(t *testing.T) {
	got := toolStartLabel("Shell", map[string]interface{}{"command": strings.Repeat("a", 120)})
	if len(got) > 100 || !strings.HasSuffix(got, "...") {
		t.Fatalf("expected compact truncated label, got %q", got)
	}
}

func TestToolStartLabelMapsBashToShell(t *testing.T) {
	got := toolStartLabel("Bash", map[string]interface{}{"command": "echo ok"})
	if got != "Shell: echo ok" {
		t.Fatalf("expected Bash alias to render as Shell, got %q", got)
	}
	got = toolStartLabel("Bash", nil)
	if got != "Shell" {
		t.Fatalf("expected missing command to avoid <nil>, got %q", got)
	}
}

func TestNormalizeToolInputParsesRawJSON(t *testing.T) {
	got := normalizeToolInput(map[string]interface{}{"raw": `{"file_path":"report.md","content":"ok"}`})
	if got["file_path"] != "report.md" || got["content"] != "ok" {
		t.Fatalf("normalizeToolInput = %#v, want parsed raw JSON", got)
	}
}

func TestWriteErrorSummaryUsesErrorContent(t *testing.T) {
	result := &tools.ToolResult{Content: "Write 缺少 file_path 参数", IsError: true}
	got := toolResultSummary("Write", map[string]interface{}{}, result)
	if got != "Write 缺少 file_path 参数" {
		t.Fatalf("summary = %q", got)
	}
}
