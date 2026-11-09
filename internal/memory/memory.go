package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/t-rex-ai/t-rex/internal/config"
)

type File struct {
	Path    string
	Label   string
	Content string
}

type Context struct {
	Files         []File
	AutoMemoryDir string
	AutoMemoryOn  bool
}

type Entry struct {
	Index     int
	Timestamp string
	Category  string
	Note      string
}

func Load(cfg *config.Config) (*Context, error) {
	ctx := &Context{}
	if cfg == nil || !cfg.Memory.Enabled {
		return ctx, nil
	}

	root, err := projectRoot(cfg)
	if err != nil {
		return ctx, err
	}

	paths := defaultInstructionFiles(root)
	paths = append(paths, cfg.Memory.AdditionalFiles...)

	for _, p := range paths {
		abs := expandPath(p, root)
		content, err := readLimited(abs, cfg.Memory.MaxBytes, cfg.Memory.MaxLines)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			continue
		}
		ctx.Files = append(ctx.Files, File{
			Path:    abs,
			Label:   filepath.Base(abs),
			Content: content,
		})
	}

	ctx.AutoMemoryOn = cfg.Memory.AutoMemoryEnabled
	ctx.AutoMemoryDir = AutoMemoryDir(cfg, root)
	if ctx.AutoMemoryOn {
		memoryFile := filepath.Join(ctx.AutoMemoryDir, "MEMORY.md")
		content, err := readLimited(memoryFile, cfg.Memory.MaxBytes, cfg.Memory.MaxLines)
		if err == nil && strings.TrimSpace(content) != "" {
			ctx.Files = append(ctx.Files, File{
				Path:    memoryFile,
				Label:   "auto-memory",
				Content: content,
			})
		}
	}

	return ctx, nil
}

func Remember(cfg *config.Config, note string) (string, error) {
	return RememberCategory(cfg, "fact", note)
}

func RememberCategory(cfg *config.Config, category string, note string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("missing config")
	}
	category = normalizeCategory(category)
	note = strings.TrimSpace(note)
	if note == "" {
		return "", fmt.Errorf("memory note is empty")
	}

	root, err := projectRoot(cfg)
	if err != nil {
		return "", err
	}
	dir := AutoMemoryDir(cfg, root)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, "MEMORY.md")
	if existing, err := os.ReadFile(path); err == nil && containsMemoryNote(string(existing), note) {
		return path, nil
	}
	entry := fmt.Sprintf("\n- %s [%s]: %s\n", time.Now().Format(time.RFC3339), category, note)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		entry = "# T-REX Auto Memory\n" + entry
	}
	if err := appendFile(path, entry); err != nil {
		return "", err
	}
	return path, nil
}

func List(cfg *config.Config) ([]Entry, string, error) {
	root, err := projectRoot(cfg)
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(AutoMemoryDir(cfg, root), "MEMORY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, path, nil
		}
		return nil, path, err
	}
	return parseEntries(string(data)), path, nil
}

func Delete(cfg *config.Config, index int) (string, error) {
	if index <= 0 {
		return "", fmt.Errorf("memory index must be greater than zero")
	}
	root, err := projectRoot(cfg)
	if err != nil {
		return "", err
	}
	path := filepath.Join(AutoMemoryDir(cfg, root), "MEMORY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	current := 0
	var kept []string
	deleted := false
	for _, line := range lines {
		if parseEntryLine(strings.TrimSpace(line), current+1).Note != "" {
			current++
			if current == index {
				deleted = true
				continue
			}
		}
		kept = append(kept, line)
	}
	if !deleted {
		return "", fmt.Errorf("memory index %d not found", index)
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func AutoMemoryDir(cfg *config.Config, root string) string {
	if cfg != nil && cfg.Memory.AutoMemoryDirectory != "" {
		return expandPath(cfg.Memory.AutoMemoryDirectory, root)
	}
	projectKey := sanitizeProjectKey(root)
	if projectKey == "" {
		projectKey = "default"
	}
	return filepath.Join(config.GetConfigDir(), "projects", projectKey, "memory")
}

func containsMemoryNote(content, note string) bool {
	note = canonicalNote(note)
	if note == "" {
		return false
	}
	for _, entry := range parseEntries(content) {
		if canonicalNote(entry.Note) == note {
			return true
		}
	}
	return false
}

func parseEntries(content string) []Entry {
	var entries []Entry
	for _, line := range strings.Split(content, "\n") {
		entry := parseEntryLine(strings.TrimSpace(line), len(entries)+1)
		if entry.Note != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func parseEntryLine(line string, index int) Entry {
	if !strings.HasPrefix(line, "- ") {
		return Entry{}
	}
	body := strings.TrimSpace(strings.TrimPrefix(line, "- "))
	head, note, ok := strings.Cut(body, ": ")
	if !ok {
		return Entry{Index: index, Category: "fact", Note: body}
	}
	category := "fact"
	timestamp := strings.TrimSpace(head)
	if open := strings.LastIndex(head, "["); open >= 0 && strings.HasSuffix(head, "]") {
		timestamp = strings.TrimSpace(head[:open])
		category = normalizeCategory(strings.TrimSuffix(head[open+1:], "]"))
	}
	return Entry{Index: index, Timestamp: timestamp, Category: category, Note: strings.TrimSpace(note)}
}

func normalizeCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "preference", "preferences", "pref":
		return "preference"
	case "build", "command", "commands":
		return "build"
	case "debug", "debugging":
		return "debug"
	case "fact", "facts", "":
		return "fact"
	default:
		return "fact"
	}
}

func canonicalNote(note string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(note))), " ")
}

func sanitizeProjectKey(root string) string {
	root = filepath.Clean(root)
	root = strings.Trim(filepath.ToSlash(root), "/")
	re := regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	return strings.Trim(re.ReplaceAllString(root, "-"), "-")
}

func defaultInstructionFiles(root string) []string {
	paths := []string{
		filepath.Join(root, "CLAUDE.md"),
		filepath.Join(root, "CLAUDE.local.md"),
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(config.GetConfigDir(), "CLAUDE.md"),
	}

	rulesDir := filepath.Join(root, ".claude", "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				continue
			}
			paths = append(paths, filepath.Join(rulesDir, entry.Name()))
		}
	}

	return paths
}

func projectRoot(cfg *config.Config) (string, error) {
	if cfg != nil && cfg.Project.Path != "" {
		return filepath.Abs(cfg.Project.Path)
	}
	return os.Getwd()
}

func expandPath(path, root string) string {
	if path == "" {
		return path
	}
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

func readLimited(path string, maxBytes int64, maxLines int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 25 * 1024
	}
	if maxLines <= 0 {
		maxLines = 200
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		data = data[:maxBytes]
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

func appendFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}
