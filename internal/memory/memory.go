package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/t-rex-ai/t-rex/internal/config"
)

const (
	// MemoryFileName is the file that stores remembered facts and preferences.
	MemoryFileName = "MEMORY.md"
	// maxMemoryEntries bounds the store so the system prompt stays small.
	maxMemoryEntries = 200
	// maxNoteLength bounds a single note to keep entries readable and cheap.
	maxNoteLength = 1000
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
	// Saved memories are always loaded (not only when auto-capture is on) so
	// notes added with /memory add or the Remember tool are never invisible.
	memoryFile := filepath.Join(ctx.AutoMemoryDir, MemoryFileName)
	content, err := readLimited(memoryFile, cfg.Memory.MaxBytes, cfg.Memory.MaxLines)
	if err == nil && strings.TrimSpace(content) != "" {
		ctx.Files = append(ctx.Files, File{
			Path:    memoryFile,
			Label:   "auto-memory",
			Content: content,
		})
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
	note = sanitizeNote(note)
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

	path := filepath.Join(dir, MemoryFileName)
	existing, readErr := os.ReadFile(path)
	if readErr == nil && containsMemoryNote(string(existing), note) {
		return path, nil
	}
	entry := fmt.Sprintf("\n- %s [%s]: %s\n", time.Now().Format(time.RFC3339), category, note)
	if os.IsNotExist(readErr) {
		entry = "# T-REX Auto Memory\n" + entry
	}
	if err := appendFile(path, entry); err != nil {
		return "", err
	}
	if readErr == nil {
		_ = trimMemoryFile(path, maxMemoryEntries)
	}
	return path, nil
}

// Search returns the entries whose note or category contains the query.
func Search(cfg *config.Config, query string) ([]Entry, string, error) {
	entries, path, err := List(cfg)
	if err != nil {
		return nil, path, err
	}
	needle := canonicalNote(query)
	if needle == "" {
		return nil, path, nil
	}
	var matches []Entry
	for _, entry := range entries {
		if strings.Contains(canonicalNote(entry.Note), needle) ||
			strings.Contains(strings.ToLower(entry.Category), needle) {
			matches = append(matches, entry)
		}
	}
	return matches, path, nil
}

// Clear removes all saved memories, leaving no MEMORY.md behind.
func Clear(cfg *config.Config) (string, error) {
	root, err := projectRoot(cfg)
	if err != nil {
		return "", err
	}
	path := filepath.Join(AutoMemoryDir(cfg, root), MemoryFileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

// Count reports how many memory entries are currently saved.
func Count(cfg *config.Config) int {
	entries, _, err := List(cfg)
	if err != nil {
		return 0
	}
	return len(entries)
}

// sanitizeNote collapses any whitespace (including newlines) into single spaces
// so a note is always stored as exactly one line. This keeps entry parsing and
// index-based deletion reliable.
func sanitizeNote(note string) string {
	note = strings.ReplaceAll(note, "\r\n", "\n")
	note = strings.ReplaceAll(note, "\r", "\n")
	note = strings.Join(strings.Fields(strings.ReplaceAll(note, "\n", " ")), " ")
	return truncateRunes(note, maxNoteLength)
}

func truncateRunes(value string, max int) string {
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return strings.TrimSpace(string(runes[:max]))
}

// trimMemoryFile keeps the header plus the most recent max entries.
func trimMemoryFile(path string, max int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var header []string
	var entries []string
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			entries = append(entries, line)
			continue
		}
		if len(entries) == 0 {
			header = append(header, line)
		}
	}
	if max <= 0 || len(entries) <= max {
		return nil
	}
	kept := append(header, entries[len(entries)-max:]...)
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0644)
}

func List(cfg *config.Config) ([]Entry, string, error) {
	root, err := projectRoot(cfg)
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(AutoMemoryDir(cfg, root), MemoryFileName)
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
	path := filepath.Join(AutoMemoryDir(cfg, root), MemoryFileName)
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
	raw := strings.ToLower(strings.TrimSpace(category))
	switch raw {
	case "preference", "preferences", "pref":
		return "preference"
	case "build", "command", "commands":
		return "build"
	case "debug", "debugging", "gotcha", "pitfall":
		return "debug"
	case "pattern", "patterns", "convention", "conventions":
		return "pattern"
	case "decision", "decisions", "adr":
		return "decision"
	case "style", "formatting", "format":
		return "style"
	case "fact", "facts", "":
		return "fact"
	}
	if isSimpleToken(raw) {
		return raw
	}
	return "fact"
}

// isSimpleToken reports whether value is a short, safe single-word category
// that can be stored and parsed back without ambiguity.
func isSimpleToken(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > 24 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
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
	truncated := false
	if int64(len(data)) > maxBytes {
		data = data[:maxBytes]
		truncated = true
	}

	text := string(data)
	if truncated {
		// Byte-level truncation can split a multi-byte rune; drop the trailing
		// partial rune so the excerpt stays valid UTF-8.
		for len(text) > 0 {
			r, size := utf8.DecodeLastRuneInString(text)
			if r != utf8.RuneError || size > 1 {
				break
			}
			text = text[:len(text)-1]
		}
	}

	lines := strings.Split(text, "\n")
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
