package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/t-rex-ai/t-rex/internal/config"
)

func TestRememberDeduplicatesNotes(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Project.Path = root
	cfg.Memory.AutoMemoryDirectory = filepath.Join(root, ".memory")

	path, err := Remember(cfg, "use go test ./...")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Remember(cfg, "use go test ./..."); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "use go test ./..."); count != 1 {
		t.Fatalf("expected one saved note, got %d\n%s", count, data)
	}
}

func TestMemoryListAndDelete(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Project.Path = root
	cfg.Memory.AutoMemoryDirectory = filepath.Join(root, ".memory")

	if _, err := RememberCategory(cfg, "build", "use go test ./..."); err != nil {
		t.Fatal(err)
	}
	if _, err := RememberCategory(cfg, "preference", "keep output concise"); err != nil {
		t.Fatal(err)
	}

	entries, _, err := List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Category != "build" || entries[1].Category != "preference" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if _, err := Delete(cfg, 1); err != nil {
		t.Fatal(err)
	}
	entries, _, err = List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Note != "keep output concise" {
		t.Fatalf("unexpected entries after delete: %#v", entries)
	}
}

func TestRememberDeduplicatesCanonicalNotes(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Project.Path = root
	cfg.Memory.AutoMemoryDirectory = filepath.Join(root, ".memory")

	if _, err := Remember(cfg, "Use go test ./..."); err != nil {
		t.Fatal(err)
	}
	if _, err := Remember(cfg, " use   GO test ./... "); err != nil {
		t.Fatal(err)
	}
	entries, _, err := List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one canonical entry, got %#v", entries)
	}
}

func TestSanitizeProjectKeyRemovesUnsafePathCharacters(t *testing.T) {
	key := sanitizeProjectKey(`C:\Users\noqq\Music\T-Rex`)
	if strings.ContainsAny(key, `:\/`) {
		t.Fatalf("project key still contains unsafe path characters: %q", key)
	}
	if key == "" {
		t.Fatal("expected non-empty project key")
	}
}

func TestRememberStoresMultilineNoteAsSingleLine(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Project.Path = root
	cfg.Memory.AutoMemoryDirectory = filepath.Join(root, ".memory")

	if _, err := RememberCategory(cfg, "debug", "line one\nline two\r\nline three"); err != nil {
		t.Fatal(err)
	}

	entries, _, err := List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected a single entry, got %#v", entries)
	}
	if entries[0].Note != "line one line two line three" {
		t.Fatalf("note was not collapsed to a single line: %q", entries[0].Note)
	}
	if _, err := Delete(cfg, 1); err != nil {
		t.Fatalf("delete failed after multiline note: %v", err)
	}
	entries, _, err = List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected the entry to be removed, got %#v", entries)
	}
}

func TestMemorySearchAndClear(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Project.Path = root
	cfg.Memory.AutoMemoryDirectory = filepath.Join(root, ".memory")

	if _, err := RememberCategory(cfg, "build", "run go test ./..."); err != nil {
		t.Fatal(err)
	}
	if _, err := RememberCategory(cfg, "preference", "keep answers concise"); err != nil {
		t.Fatal(err)
	}

	matches, _, err := Search(cfg, "GO TEST")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Category != "build" {
		t.Fatalf("unexpected search results: %#v", matches)
	}

	if _, err := Clear(cfg); err != nil {
		t.Fatal(err)
	}
	if count := Count(cfg); count != 0 {
		t.Fatalf("expected zero entries after clear, got %d", count)
	}
}

func TestRememberKeepsUnknownSimpleCategory(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Project.Path = root
	cfg.Memory.AutoMemoryDirectory = filepath.Join(root, ".memory")

	if _, err := RememberCategory(cfg, "security", "never log secrets"); err != nil {
		t.Fatal(err)
	}
	entries, _, err := List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Category != "security" {
		t.Fatalf("expected category to be preserved, got %#v", entries)
	}
}

func TestReadLimitedKeepsValidUTF8WhenTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MEMORY.md")
	content := strings.Repeat("记忆内容-", 200)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := readLimited(path, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncated content is not valid UTF-8: %q", got)
	}
}

func TestTrimMemoryFileKeepsNewestEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MEMORY.md")
	var b strings.Builder
	b.WriteString("# T-REX Auto Memory\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, "\n- 2026-01-01T00:00:00Z [fact]: note %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}

	if err := trimMemoryFile(path, 3); err != nil {
		t.Fatal(err)
	}
	entries, err := parseEntriesFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries after trim, got %d", len(entries))
	}
	if entries[0].Note != "note 7" || entries[2].Note != "note 9" {
		t.Fatalf("trim kept the wrong entries: %#v", entries)
	}
}

func parseEntriesFile(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseEntries(string(data)), nil
}
