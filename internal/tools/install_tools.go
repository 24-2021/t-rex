package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/t-rex-ai/t-rex/internal/config"
)

var installNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type MCPInstallTool struct {
	config *config.Config
}

func (t *MCPInstallTool) Name() string { return "MCP_Install" }

func (t *MCPInstallTool) Description() string {
	return "Install or update an MCP server in T-REX config. Use this only after the user asks to add MCP capability or approves a specific server."
}

func (t *MCPInstallTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":    map[string]interface{}{"type": "string", "description": "Stable MCP server name."},
			"type":    map[string]interface{}{"type": "string", "enum": []string{"stdio", "http", "streamable-http", "sse"}},
			"command": map[string]interface{}{"type": "string", "description": "Command for stdio servers, such as npx or python."},
			"args":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"url":     map[string]interface{}{"type": "string", "description": "URL for http/streamable-http/sse servers."},
			"env":     map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
			"headers": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
			"enabled": map[string]interface{}{"type": "boolean"},
			"replace": map[string]interface{}{"type": "boolean", "description": "Replace an existing server with the same name."},
			"save":    map[string]interface{}{"type": "boolean", "description": "Persist to config file. Defaults to true."},
		},
		"required": []string{"name", "type"},
	}
}

func (t *MCPInstallTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	if t.config == nil {
		return &ToolResult{Content: "config is not initialized", IsError: true}, nil
	}
	name := strings.TrimSpace(toString(input["name"]))
	if !installNamePattern.MatchString(name) {
		return &ToolResult{Content: "invalid MCP server name", IsError: true}, nil
	}
	serverType := strings.ToLower(strings.TrimSpace(toString(input["type"])))
	if serverType == "" {
		serverType = "stdio"
	}
	cfg := config.MCPServerConfig{
		Name:    name,
		Type:    serverType,
		Command: strings.TrimSpace(toString(input["command"])),
		Args:    stringSliceInput(input["args"]),
		Env:     stringMapInput(input["env"]),
		URL:     strings.TrimSpace(toString(input["url"])),
		Headers: stringMapInput(input["headers"]),
		Enabled: true,
	}
	if v, ok := input["enabled"].(bool); ok {
		cfg.Enabled = v
	}
	switch serverType {
	case "stdio":
		if cfg.Command == "" {
			return &ToolResult{Content: "stdio MCP server requires command", IsError: true}, nil
		}
	case "http", "streamable-http", "sse":
		if cfg.URL == "" {
			return &ToolResult{Content: serverType + " MCP server requires url", IsError: true}, nil
		}
	default:
		return &ToolResult{Content: "unsupported MCP server type: " + serverType, IsError: true}, nil
	}

	replace := boolInput(input["replace"])
	replaced := false
	for i := range t.config.MCP.Servers {
		if strings.EqualFold(t.config.MCP.Servers[i].Name, name) {
			if !replace {
				return &ToolResult{Content: "MCP server already exists; set replace=true to update it", IsError: true}, nil
			}
			t.config.MCP.Servers[i] = cfg
			replaced = true
			break
		}
	}
	if !replaced {
		t.config.MCP.Servers = append(t.config.MCP.Servers, cfg)
	}

	savedTo := ""
	if saveEnabled(input) {
		path := configPath(t.config)
		if err := t.config.Save(path); err != nil {
			return &ToolResult{Content: fmt.Sprintf("MCP server installed in memory but config save failed: %v", err), IsError: true}, nil
		}
		savedTo = path
	}

	out := map[string]interface{}{"server": cfg, "replaced": replaced, "savedTo": savedTo}
	data, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: string(data), Output: out}, nil
}

type SkillsInstallTool struct {
	config *config.Config
}

func (t *SkillsInstallTool) Name() string { return "Skills_Install" }

func (t *SkillsInstallTool) Description() string {
	return "Install a local skill into the global skills directory beside T-REX config.json, either by copying an existing SKILL.md/directory or by creating SKILL.md content."
}

func (t *SkillsInstallTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":        map[string]interface{}{"type": "string", "description": "Skill directory/name."},
			"source_path": map[string]interface{}{"type": "string", "description": "Existing local SKILL.md file or skill directory inside the workspace."},
			"content":     map[string]interface{}{"type": "string", "description": "SKILL.md content to create when source_path is omitted."},
			"replace":     map[string]interface{}{"type": "boolean", "description": "Replace existing installed skill."},
		},
		"required": []string{"name"},
	}
}

func (t *SkillsInstallTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	if t.config == nil {
		return &ToolResult{Content: "config is not initialized", IsError: true}, nil
	}
	name := strings.TrimSpace(toString(input["name"]))
	if !installNamePattern.MatchString(name) {
		return &ToolResult{Content: "invalid skill name", IsError: true}, nil
	}
	targetDir := filepath.Join(config.GetSkillsDir(t.config), name)
	if exists(targetDir) {
		if !boolInput(input["replace"]) {
			return &ToolResult{Content: "skill already exists; set replace=true to update it", IsError: true}, nil
		}
		if err := os.RemoveAll(targetDir); err != nil {
			return &ToolResult{Content: fmt.Sprintf("failed to replace skill: %v", err), IsError: true}, nil
		}
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return &ToolResult{Content: fmt.Sprintf("failed to create skill directory: %v", err), IsError: true}, nil
	}

	sourcePath := strings.TrimSpace(toString(input["source_path"]))
	if sourcePath != "" {
		source, err := resolveWorkspacePath(t.config, sourcePath)
		if err != nil {
			return &ToolResult{Content: err.Error(), IsError: true}, nil
		}
		if err := copySkillSource(source, targetDir); err != nil {
			return &ToolResult{Content: fmt.Sprintf("skill install failed: %v", err), IsError: true}, nil
		}
	} else {
		content := strings.TrimSpace(toString(input["content"]))
		if content == "" {
			return &ToolResult{Content: "missing source_path or content", IsError: true}, nil
		}
		if !strings.HasPrefix(content, "---") {
			content = fmt.Sprintf("---\nname: %s\ndescription: Local T-REX skill\n---\n\n%s\n", name, content)
		}
		if err := os.WriteFile(filepath.Join(targetDir, "SKILL.md"), []byte(content), 0644); err != nil {
			return &ToolResult{Content: fmt.Sprintf("failed to write SKILL.md: %v", err), IsError: true}, nil
		}
	}
	out := map[string]string{"name": name, "path": targetDir}
	data, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: string(data), Output: out}, nil
}

func registerInstallTools(r *Registry) {
	r.Register(&MCPInstallTool{config: r.config})
	r.Register(&SkillsInstallTool{config: r.config})
}

func configPath(cfg *config.Config) string {
	if cfg != nil && strings.TrimSpace(cfg.ConfigPath) != "" {
		return cfg.ConfigPath
	}
	return config.GetDefaultConfigPath()
}

func saveEnabled(input map[string]interface{}) bool {
	if v, ok := input["save"].(bool); ok {
		return v
	}
	return true
}

func stringSliceInput(value interface{}) []string {
	var out []string
	if raw, ok := value.([]interface{}); ok {
		for _, item := range raw {
			out = append(out, toString(item))
		}
	}
	if raw, ok := value.([]string); ok {
		out = append(out, raw...)
	}
	return out
}

func stringMapInput(value interface{}) map[string]string {
	out := map[string]string{}
	if raw, ok := value.(map[string]interface{}); ok {
		for k, v := range raw {
			if strings.TrimSpace(k) != "" {
				out[k] = toString(v)
			}
		}
	}
	if raw, ok := value.(map[string]string); ok {
		for k, v := range raw {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func copySkillSource(source, targetDir string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Base(source), "SKILL.md") {
			return fmt.Errorf("source file must be named SKILL.md")
		}
		return copyFile(source, filepath.Join(targetDir, "SKILL.md"))
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == source {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if shouldSkipInstallDir(entry.Name()) && entry.IsDir() {
			return filepath.SkipDir
		}
		target := filepath.Join(targetDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func shouldSkipInstallDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "dist", "build", ".cache", "vendor":
		return true
	default:
		return false
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
