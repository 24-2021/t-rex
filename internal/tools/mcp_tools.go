package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/t-rex-ai/t-rex/internal/mcp"
)

type MCPTool struct {
	server string
	tool   mcp.Tool
	client mcp.Client
}

func (t *MCPTool) Name() string {
	return "mcp__" + sanitizeToolName(t.server) + "__" + sanitizeToolName(t.tool.Name)
}

func (t *MCPTool) Description() string {
	desc := strings.TrimSpace(t.tool.Description)
	if desc == "" {
		desc = "MCP tool"
	}
	return fmt.Sprintf("[MCP:%s] %s", t.server, desc)
}

func (t *MCPTool) InputSchema() map[string]interface{} {
	if len(t.tool.InputSchema) == 0 {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(t.tool.InputSchema, &schema); err != nil || schema == nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	return schema
}

func (t *MCPTool) Execute(ctx context.Context, input map[string]interface{}) (*ToolResult, error) {
	result, err := t.client.CallTool(ctx, t.tool.Name, input)
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("MCP tool failed: %v", err), IsError: true}, nil
	}
	var parts []string
	for _, block := range result.Content {
		switch block.Type {
		case "text", "":
			parts = append(parts, block.Text)
		default:
			data, _ := json.Marshal(block)
			parts = append(parts, string(data))
		}
	}
	return &ToolResult{
		Content: strings.TrimSpace(strings.Join(parts, "\n")),
		Output:  result,
		IsError: result.IsError,
	}, nil
}

func (r *Registry) RegisterMCPTools(ctx context.Context, manager *mcp.Manager) error {
	if manager == nil {
		return nil
	}
	allTools, err := manager.GetAllTools(ctx)
	if err != nil {
		return err
	}
	for server, list := range allTools {
		client, ok := manager.GetClient(server)
		if !ok {
			continue
		}
		for _, tool := range list {
			r.Register(&MCPTool{server: server, tool: tool, client: client})
		}
	}
	return nil
}

func sanitizeToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevUnderscore := false
	for _, r := range name {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteRune('_')
			prevUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}
