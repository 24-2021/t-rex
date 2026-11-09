package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/config"
)

func TestRegistryIncludesInstallTools(t *testing.T) {
	cfg := config.Default()
	cfg.Project.Path = t.TempDir()
	registry := NewRegistry(cfg, allowAllPermission{})
	if _, ok := registry.Get("MCP_Install"); !ok {
		t.Fatalf("MCP_Install not registered")
	}
	if _, ok := registry.Get("Skills_Install"); !ok {
		t.Fatalf("Skills_Install not registered")
	}
	if _, ok := registry.Get("Skills_List"); !ok {
		t.Fatalf("Skills_List should be registered even when no skills exist")
	}
}

func TestMCPInstallAddsServer(t *testing.T) {
	cfg := config.Default()
	cfg.Project.Path = t.TempDir()
	tool := &MCPInstallTool{config: cfg}
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"name":    "browser",
		"type":    "stdio",
		"command": "npx",
		"args":    []interface{}{"-y", "@modelcontextprotocol/server-browser"},
		"save":    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Name != "browser" || cfg.MCP.Servers[0].Command != "npx" {
		t.Fatalf("server not installed: %#v", cfg.MCP.Servers)
	}
}

func TestSkillsInstallCreatesSkill(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Project.Path = root
	cfg.ConfigPath = filepath.Join(root, "config.json")
	tool := &SkillsInstallTool{config: cfg}
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"name":    "audit-helper",
		"content": "Use focused source-to-sink review.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	data, err := os.ReadFile(filepath.Join(root, "skills", "audit-helper", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Use focused source-to-sink review.") {
		t.Fatalf("skill content missing: %s", string(data))
	}
}

type allowAllPermission struct{}

func (allowAllPermission) CheckPermission(string, map[string]interface{}) (bool, string) {
	return true, "allowed"
}

func (allowAllPermission) RequestPermission(string, map[string]interface{}) (bool, error) {
	return true, nil
}
