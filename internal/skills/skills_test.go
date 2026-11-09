package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/config"
)

func TestDiscoverFindsProjectRootSkillsAndResources(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".codex", "skills", "planner")
	if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: planner\ndescription: Plan carefully\n---\nSee references/guide.md\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "references", "guide.md"), []byte("extra instructions"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Project.Path = root

	catalog, err := Discover(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Read("planner"); err != nil {
		t.Fatalf("expected project skill to be discovered: %v", err)
	}
	_, content, err := catalog.ReadResource("planner", "references/guide.md")
	if err != nil {
		t.Fatalf("expected skill resource to be readable: %v", err)
	}
	if content != "extra instructions" {
		t.Fatalf("unexpected resource content: %q", content)
	}
}

func TestDiscoverFindsConfigDirSkills(t *testing.T) {
	root := t.TempDir()
	configDir := t.TempDir()
	skillDir := filepath.Join(configDir, "skills", "global-helper")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: global-helper\ndescription: Shared helper\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Project.Path = root
	cfg.ConfigPath = filepath.Join(configDir, "config.json")

	catalog, err := Discover(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Read("global-helper"); err != nil {
		t.Fatalf("expected config-dir skill to be discovered: %v", err)
	}
}

func TestDiscoverIgnoresOtherProjectSkills(t *testing.T) {
	root := t.TempDir()
	otherProjectSkill := filepath.Join(root, "claude-code-main", "plugins", "demo", "skills", "foreign")
	if err := os.MkdirAll(otherProjectSkill, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherProjectSkill, "SKILL.md"), []byte("---\nname: foreign\ndescription: other project skill\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	externalRoot := t.TempDir()
	externalSkill := filepath.Join(externalRoot, "skills", "external")
	if err := os.MkdirAll(externalSkill, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalSkill, "SKILL.md"), []byte("---\nname: external\ndescription: external skill\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Project.Path = root
	cfg.Skills.Paths = []string{filepath.Join(externalRoot, "skills")}

	catalog, err := Discover(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Read("foreign"); err == nil {
		t.Fatal("expected embedded other-project skill to be ignored")
	}
	if _, _, err := catalog.Read("external"); err == nil {
		t.Fatal("expected external configured skill path to be ignored")
	}
}

func TestReadResourceRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "safe")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("safe skill"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Project.Path = root
	catalog, err := Discover(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.ReadResource("safe", "../outside.md"); err == nil {
		t.Fatal("expected path escape to be rejected")
	}
}

func TestDiscoverRejectsInvalidSkillFrontmatter(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "bad")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: bad/skill\ndescription: invalid\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Project.Path = root
	catalog, err := Discover(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Read("bad/skill"); err == nil {
		t.Fatal("expected invalid skill name to be rejected")
	}
}
