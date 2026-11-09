package session

import (
	"strings"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/api"
	"github.com/t-rex-ai/t-rex/internal/config"
)

func TestStructuredMessagesSurviveSaveAndResume(t *testing.T) {
	cfg := &config.SessionConfig{
		StoragePath:      t.TempDir(),
		MaxHistorySize:   100,
		AutoSaveInterval: 3600,
	}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.Create(t.TempDir(), "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddMessage("user", "inspect the repo", nil, nil); err != nil {
		t.Fatal(err)
	}
	toolUse := api.ContentBlock{Type: "tool_use", ID: "tool-1", Name: "Read", Input: map[string]interface{}{"file_path": "main.go"}}
	toolResult := api.ContentBlock{Type: "tool_result", ToolUseID: "tool-1", Content: "1:package main"}
	if err := mgr.AddStructuredMessage("assistant", []api.ContentBlock{toolUse}, true, &api.Usage{InputTokens: 10, OutputTokens: 2}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddStructuredMessage("user", []api.ContentBlock{toolResult}, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Save(); err != nil {
		t.Fatal(err)
	}

	resumed, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := resumed.GetByID(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumed.SetCurrent(loaded)
	messages := resumed.GetMessagesForAPI(0)
	if len(messages) != 3 {
		t.Fatalf("message count = %d, want 3", len(messages))
	}
	blocks, ok := messages[1].Content.([]api.ContentBlock)
	if !ok || len(blocks) != 1 || blocks[0].Name != "Read" || blocks[0].ID != "tool-1" {
		t.Fatalf("tool call was not restored: %#v", messages[1].Content)
	}
	resultBlocks, ok := messages[2].Content.([]api.ContentBlock)
	if !ok || len(resultBlocks) != 1 || resultBlocks[0].ToolUseID != "tool-1" {
		t.Fatalf("tool result was not restored: %#v", messages[2].Content)
	}
	if loaded.TokenUsage.InputTokens != 10 || loaded.TokenUsage.OutputTokens != 2 {
		t.Fatalf("unexpected usage: %#v", loaded.TokenUsage)
	}
}

func TestListAllSessionsSummarizesFullSessionFiles(t *testing.T) {
	cfg := &config.SessionConfig{
		StoragePath:      t.TempDir(),
		MaxHistorySize:   100,
		AutoSaveInterval: 3600,
	}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.Create("project-a", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddMessage("user", "hello", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddMessage("assistant", "hi", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Save(); err != nil {
		t.Fatal(err)
	}

	summaries, err := ListAllSessions(cfg.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summary count = %d, want 1", len(summaries))
	}
	got := summaries[0]
	if got.ID != sess.ID {
		t.Fatalf("summary ID = %q, want %q", got.ID, sess.ID)
	}
	if got.MessageCount != 2 {
		t.Fatalf("summary message count = %d, want 2", got.MessageCount)
	}
	if got.Path != "project-a" {
		t.Fatalf("summary path = %q, want project-a", got.Path)
	}
}

func TestAgentStateJSONSurvivesSaveAndResume(t *testing.T) {
	cfg := &config.SessionConfig{
		StoragePath:      t.TempDir(),
		MaxHistorySize:   100,
		AutoSaveInterval: 3600,
	}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.Create("project-a", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	state := []byte(`{"mission":{"goal":"assess target"},"coverage":[{"id":"C-1","catalogId":"WEB-AC","status":"planned"}]}`)
	mgr.SetAgentStateJSON(state)
	if err := mgr.Save(); err != nil {
		t.Fatal(err)
	}

	resumed, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := resumed.GetByID(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumed.SetCurrent(loaded)
	if got := string(resumed.AgentStateJSON()); !strings.Contains(got, "assess target") || !strings.Contains(got, "WEB-AC") {
		t.Fatalf("agent state not restored: %s", got)
	}
}

func TestCompactPreservesToolTurnBoundary(t *testing.T) {
	cfg := &config.SessionConfig{StoragePath: t.TempDir(), MaxHistorySize: 100, AutoSaveInterval: 3600}
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Create(t.TempDir(), "test-model"); err != nil {
		t.Fatal(err)
	}
	_ = mgr.AddMessage("user", "old turn", nil, nil)
	_ = mgr.AddMessage("assistant", "old answer", nil, nil)
	_ = mgr.AddMessage("user", "new turn", nil, nil)
	_ = mgr.AddStructuredMessage("assistant", []api.ContentBlock{{Type: "tool_use", ID: "t1", Name: "Read"}}, true, nil)
	_ = mgr.AddStructuredMessage("user", []api.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Content: "ok"}}, true, nil)
	_ = mgr.AddMessage("assistant", "new answer", nil, nil)

	remaining := mgr.Compact(3)
	if remaining != 4 {
		t.Fatalf("remaining = %d, want complete four-message turn", remaining)
	}
	messages := mgr.GetMessagesForAPI(3)
	if len(messages) != 4 || messages[0].Role != "user" || messages[0].Content != "new turn" {
		t.Fatalf("invalid compacted transcript: %#v", messages)
	}
}

func TestCompactSummaryPrioritizesStructuredStateAndRecentResults(t *testing.T) {
	messages := []SessionMessage{
		{Role: "user", Content: "inspect the project"},
		{Role: "assistant", Blocks: []api.ContentBlock{{Type: "tool_use", ID: "mission-1", Name: "Mission", Input: map[string]interface{}{"action": "update"}}}},
		{Role: "user", Blocks: []api.ContentBlock{{Type: "tool_result", ToolUseID: "mission-1", Content: `{"goal":"fix parser","phase":"testing"}`}}},
		{Role: "assistant", Blocks: []api.ContentBlock{{Type: "tool_use", ID: "read-1", Name: "Read", Input: map[string]interface{}{"file_path": "internal/api/client.go"}}}},
		{Role: "user", Blocks: []api.ContentBlock{{Type: "tool_result", ToolUseID: "read-1", Content: "stream parser result"}}},
	}
	summary := summarizeMessagesForCompact(messages)
	if !strings.Contains(summary, "Structured state (authoritative)") || !strings.Contains(summary, "Mission:") {
		t.Fatalf("structured mission state missing: %s", summary)
	}
	if !strings.Contains(summary, "Recent key tool results") || !strings.Contains(summary, "Read: stream parser result") {
		t.Fatalf("recent tool result missing: %s", summary)
	}
}
