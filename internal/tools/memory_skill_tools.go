package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/t-rex-ai/t-rex/internal/config"
	"github.com/t-rex-ai/t-rex/internal/memory"
	"github.com/t-rex-ai/t-rex/internal/skills"
)

type RememberTool struct {
	config *config.Config
}

func (t *RememberTool) Name() string { return "Remember" }

func (t *RememberTool) Description() string {
	return "Save a concise, durable project memory when the user asks you to remember a preference, build command, debugging insight, or repository-specific fact."
}

func (t *RememberTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"note": map[string]interface{}{
				"type":        "string",
				"description": "The memory to save. Keep it short, specific, reusable, and on a single line.",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"description": "Optional category: fact (default), preference, build, debug, pattern, decision, or style.",
			},
		},
		"required": []string{"note"},
	}
}

func (t *RememberTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	note, _ := input["note"].(string)
	category, _ := input["category"].(string)
	path, err := memory.RememberCategory(t.config, category, note)
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("memory save failed: %v", err), IsError: true}, nil
	}
	return &ToolResult{
		Content: fmt.Sprintf("Saved memory to %s", path),
		Output:  map[string]string{"path": path},
	}, nil
}

type SkillListTool struct {
	catalog *skills.Catalog
}

func (t *SkillListTool) Name() string { return "Skills_List" }

func (t *SkillListTool) Description() string {
	return "List locally installed skills. Use this when deciding whether a repeatable workflow skill applies."
}

func (t *SkillListTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SkillListTool) Execute(_ context.Context, _ map[string]interface{}) (*ToolResult, error) {
	items := t.catalog.List()
	data, _ := json.MarshalIndent(items, "", "  ")
	return &ToolResult{Content: string(data), Output: items}, nil
}

type SkillReadTool struct {
	catalog *skills.Catalog
}

func (t *SkillReadTool) Name() string { return "Skills_Read" }

func (t *SkillReadTool) Description() string {
	return "Read the full SKILL.md instructions for a specific skill before applying it."
}

func (t *SkillReadTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Skill id or name, for example frontend-design.",
			},
		},
		"required": []string{"id"},
	}
}

func (t *SkillReadTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	id, _ := input["id"].(string)
	skill, content, err := t.catalog.Read(id)
	if err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}
	return &ToolResult{
		Content: content,
		Output: map[string]string{
			"id":          skill.ID,
			"name":        skill.Name,
			"description": skill.Description,
			"path":        skill.Path,
		},
	}, nil
}

type SkillReadResourceTool struct {
	catalog *skills.Catalog
}

func (t *SkillReadResourceTool) Name() string { return "Skills_ReadResource" }

func (t *SkillReadResourceTool) Description() string {
	return "Read an additional file referenced by a skill, using a path relative to that skill directory."
}

func (t *SkillReadResourceTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Skill id or name.",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Relative path inside the skill directory, for example references/guide.md.",
			},
		},
		"required": []string{"id", "path"},
	}
}

func (t *SkillReadResourceTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	id, _ := input["id"].(string)
	path, _ := input["path"].(string)
	skill, content, err := t.catalog.ReadResource(id, path)
	if err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}
	return &ToolResult{
		Content: content,
		Output: map[string]string{
			"id":          skill.ID,
			"name":        skill.Name,
			"description": skill.Description,
			"path":        path,
		},
	}, nil
}

func registerMemoryAndSkillTools(r *Registry) {
	if r.config.Memory.Enabled && r.config.Memory.AutoMemoryEnabled {
		r.Register(&RememberTool{config: r.config})
	}

	catalog, err := skills.Discover(r.config)
	if err != nil {
		return
	}
	r.Register(&SkillListTool{catalog: catalog})
	r.Register(&SkillReadTool{catalog: catalog})
	r.Register(&SkillReadResourceTool{catalog: catalog})
}
