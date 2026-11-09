package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/t-rex-ai/t-rex/internal/config"
)

type Skill struct {
	ID          string
	Name        string
	Description string
	Path        string
	Source      string
}

type Catalog struct {
	Skills []Skill
	byID   map[string]Skill
}

const maxDiscoveryDepth = 8

var skillIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func Discover(cfg *config.Config) (*Catalog, error) {
	c := &Catalog{byID: make(map[string]Skill)}
	if cfg == nil || !cfg.Skills.Enabled {
		return c, nil
	}

	root, err := projectRoot(cfg)
	if err != nil {
		return c, err
	}
	root = filepath.Clean(root)
	configSkillsRoot := filepath.Clean(config.GetSkillsDir(cfg))

	for _, dir := range discoveryDirs(cfg, root) {
		abs := expandPath(dir, root)
		if !isWithinRoot(abs, root) && !isWithinRoot(abs, configSkillsRoot) {
			continue
		}
		_ = discoverInPath(c, abs)
	}

	sort.Slice(c.Skills, func(i, j int) bool {
		return c.Skills[i].ID < c.Skills[j].ID
	})
	return c, nil
}

func (c *Catalog) List() []Skill {
	if c == nil {
		return nil
	}
	out := make([]Skill, len(c.Skills))
	copy(out, c.Skills)
	return out
}

func (c *Catalog) Read(id string) (Skill, string, error) {
	if c == nil {
		return Skill{}, "", fmt.Errorf("skills catalog is not initialized")
	}
	key := normalizeID(id)
	skill, ok := c.byID[key]
	if !ok {
		return Skill{}, "", fmt.Errorf("skill %q not found", id)
	}
	data, err := os.ReadFile(skill.Path)
	if err != nil {
		return Skill{}, "", err
	}
	return skill, string(data), nil
}

func (c *Catalog) ReadResource(id, resourcePath string) (Skill, string, error) {
	if c == nil {
		return Skill{}, "", fmt.Errorf("skills catalog is not initialized")
	}
	key := normalizeID(id)
	skill, ok := c.byID[key]
	if !ok {
		return Skill{}, "", fmt.Errorf("skill %q not found", id)
	}
	resourcePath = strings.TrimSpace(resourcePath)
	if resourcePath == "" {
		return Skill{}, "", fmt.Errorf("resource path is empty")
	}
	if filepath.IsAbs(resourcePath) {
		return Skill{}, "", fmt.Errorf("resource path must be relative to the skill directory")
	}
	base := filepath.Dir(skill.Path)
	target := filepath.Clean(filepath.Join(base, resourcePath))
	rel, err := filepath.Rel(base, target)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return Skill{}, "", fmt.Errorf("resource path escapes skill directory")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return Skill{}, "", err
	}
	return skill, string(data), nil
}

func (c *Catalog) add(skill Skill) {
	id := normalizeID(skill.ID)
	if id == "" {
		return
	}
	if _, exists := c.byID[id]; exists {
		return
	}
	skill.ID = id
	c.byID[id] = skill
	c.Skills = append(c.Skills, skill)
}

func discoveryDirs(cfg *config.Config, root string) []string {
	dirs := []string{
		config.GetSkillsDir(cfg),
		filepath.Join(root, ".claude", "skills"),
		filepath.Join(root, ".codex", "skills"),
		filepath.Join(root, "skills"),
	}
	dirs = append(dirs, cfg.Skills.Paths...)
	return dirs
}

func isWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

func discoverInPath(c *Catalog, abs string) error {
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if strings.EqualFold(filepath.Base(abs), "SKILL.md") {
			skill, err := readSkill(abs, filepath.Base(filepath.Dir(abs)), filepath.Dir(abs))
			if err == nil {
				c.add(skill)
			}
		}
		return nil
	}

	root := abs
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr == nil && depth(rel) > maxDiscoveryDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(entry.Name(), "SKILL.md") {
			return nil
		}
		skillDir := filepath.Dir(path)
		skill, readErr := readSkill(path, filepath.Base(skillDir), root)
		if readErr == nil {
			c.add(skill)
		}
		return nil
	})
}

func shouldSkipDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "dist", "build", ".cache", "vendor":
		return true
	default:
		return false
	}
}

func depth(path string) int {
	if path == "." || path == "" {
		return 0
	}
	return len(strings.Split(filepath.Clean(path), string(filepath.Separator)))
}

func readSkill(path, fallbackName, source string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	content := string(data)
	name, desc, err := parseMetadata(content)
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", path, err)
	}
	if name == "" {
		name = fallbackName
	}
	if !skillIDPattern.MatchString(name) {
		return Skill{}, fmt.Errorf("%s: invalid skill name %q", path, name)
	}
	if desc == "" {
		desc = firstUsefulLine(content)
	}
	if len(desc) > 500 {
		return Skill{}, fmt.Errorf("%s: skill description is too long", path)
	}
	if desc == "" {
		desc = "Local workflow skill"
	}

	return Skill{
		ID:          name,
		Name:        name,
		Description: desc,
		Path:        path,
		Source:      source,
	}, nil
}

func parseMetadata(content string) (string, string, error) {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", nil
	}

	var name, desc string
	closed := false
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "---" {
			closed = true
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return "", "", fmt.Errorf("invalid frontmatter line %q", line)
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			name = value
		case "description":
			desc = value
		}
	}
	if !closed {
		return "", "", fmt.Errorf("frontmatter is not closed")
	}
	return name, desc, nil
}

func firstUsefulLine(content string) string {
	inFrontmatter := false
	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if i == 0 && line == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if line == "---" {
				inFrontmatter = false
			}
			continue
		}
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func normalizeID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	id = strings.ReplaceAll(id, " ", "-")
	id = strings.ReplaceAll(id, "_", "-")
	return id
}

func projectRoot(cfg *config.Config) (string, error) {
	if cfg != nil && cfg.Project.Path != "" {
		return filepath.Abs(cfg.Project.Path)
	}
	return os.Getwd()
}

func expandPath(path, root string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			rest := strings.TrimLeft(strings.TrimPrefix(path, "~"), `\/`)
			return filepath.Join(home, rest)
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}
