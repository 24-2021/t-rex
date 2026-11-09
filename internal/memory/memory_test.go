package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
