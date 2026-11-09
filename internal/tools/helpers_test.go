package tools

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/config"
)

func TestGlobPatternMatchDoubleStar(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"**/*.go", "main.go", true},
		{"**/*.go", "internal/tools/helpers.go", true},
		{"internal/**/*.go", "internal/helpers.go", true},
		{"internal/**/*.go", "internal/tools/helpers.go", true},
		{"cmd/*.go", "cmd/root.go", true},
		{"cmd/*.go", "cmd/nested/root.go", false},
	}
	for _, tt := range tests {
		got, err := globPatternMatch(tt.pattern, tt.path)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("globPatternMatch(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestGlobSearchFindsNestedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"root.go", filepath.Join("a", "one.go"), filepath.Join("a", "b", "two.go"), filepath.Join("a", "note.txt")} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := globSearch(root, "**/*.go")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"a/b/two.go", "a/one.go", "root.go"}
	for i := range got {
		got[i] = filepath.ToSlash(got[i])
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestShellCommandForOS(t *testing.T) {
	lookup := func(name string) (string, error) {
		switch name {
		case "pwsh":
			return `C:\Program Files\PowerShell\pwsh.exe`, nil
		case "bash":
			return "/bin/bash", nil
		default:
			return "", errors.New("not found")
		}
	}

	shell, args := shellCommandForOS("windows", "Write-Output ok", lookup)
	if shell != `C:\Program Files\PowerShell\pwsh.exe` || args[0] != "-NoProfile" || args[len(args)-1] != "Write-Output ok" {
		t.Fatalf("unexpected windows shell: %q %#v", shell, args)
	}

	shell, args = shellCommandForOS("linux", "echo ok", lookup)
	if shell != "/bin/bash" || args[0] != "-lc" || args[1] != "echo ok" {
		t.Fatalf("unexpected linux shell: %q %#v", shell, args)
	}

	shell, args = shellCommandForOS("darwin", "echo ok", lookup)
	if shell != "/bin/bash" || args[0] != "-lc" || args[1] != "echo ok" {
		t.Fatalf("unexpected darwin shell: %q %#v", shell, args)
	}
}

func TestShellCommandForWindowsFallsBackToCmd(t *testing.T) {
	lookup := func(name string) (string, error) {
		if name == "cmd" {
			return `C:\Windows\System32\cmd.exe`, nil
		}
		return "", errors.New("not found")
	}
	shell, args := shellCommandForOS("windows", "echo ok", lookup)
	if shell != `C:\Windows\System32\cmd.exe` || args[0] != "/C" || args[1] != "echo ok" {
		t.Fatalf("unexpected windows fallback shell: %q %#v", shell, args)
	}
}

func TestResolveWorkspacePathRejectsOutsidePath(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Project: config.ProjectConfig{Path: root}}

	if _, err := resolveWorkspacePath(cfg, filepath.Join("nested", "file.txt")); err != nil {
		t.Fatalf("workspace path should be allowed: %v", err)
	}
	if _, err := resolveWorkspacePath(cfg, filepath.Join("..", "outside.txt")); err == nil {
		t.Fatal("expected path outside workspace to be rejected")
	}
}

func TestProjectPathPolicyUsesAllowedAndDeniedPaths(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Project: config.ProjectConfig{
		Path:         root,
		AllowedPaths: []string{"src"},
		DeniedPaths:  []string{"src/private"},
		MaxFileSize:  1024,
	}}
	if _, err := resolveWorkspacePath(cfg, filepath.Join("src", "main.go")); err != nil {
		t.Fatalf("allowed path rejected: %v", err)
	}
	if _, err := resolveWorkspacePath(cfg, filepath.Join("src", "private", "secret.txt")); err == nil {
		t.Fatal("denied path should be rejected")
	}
	if _, err := resolveWorkspacePath(cfg, "README.md"); err == nil {
		t.Fatal("path outside allowed paths should be rejected")
	}
}

func TestReadReturnsPartialViewWhenFileExceedsMaxFileSize(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "large.txt")
	if err := os.WriteFile(filePath, []byte("line 1\nline 2\nline 3\nline 4\nline 5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Project: config.ProjectConfig{Path: root, MaxFileSize: 12}}
	result, err := (&ReadTool{config: cfg}).Execute(nil, map[string]interface{}{"file_path": "large.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected partial read success, got %#v", result)
	}
	if !strings.Contains(result.Content, "partial read") || !strings.Contains(result.Content, "line 1") {
		t.Fatalf("expected partial read notice and content, got %q", result.Content)
	}
	output, ok := result.Output.(map[string]interface{})
	if !ok || output["partial"] != true {
		t.Fatalf("expected partial metadata, got %#v", result.Output)
	}
}

func TestReadOversizedFileSupportsOffsetAndLimit(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "large.txt")
	if err := os.WriteFile(filePath, []byte("line 1\nline 2\nline 3\nline 4\nline 5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Project: config.ProjectConfig{Path: root, MaxFileSize: 12}}
	result, err := (&ReadTool{config: cfg}).Execute(nil, map[string]interface{}{"file_path": "large.txt", "offset": float64(3), "limit": float64(2)})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected offset read success, got %#v", result)
	}
	if strings.Contains(result.Content, "line 2") || !strings.Contains(result.Content, "line 3") || !strings.Contains(result.Content, "line 4") {
		t.Fatalf("expected lines 3-4 only, got %q", result.Content)
	}
}

func TestEditRequiresUniqueMatchUnlessReplaceAll(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(filePath, []byte("x\nx\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Project: config.ProjectConfig{Path: root, MaxFileSize: 1024}}
	tool := &EditTool{config: cfg}
	result, err := tool.Execute(nil, map[string]interface{}{"file_path": "sample.txt", "old_string": "x", "new_string": "y"})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError || !strings.Contains(result.Content, "replace_all=true") {
		t.Fatalf("expected unique-match error, got %#v", result)
	}
	result, err = tool.Execute(nil, map[string]interface{}{"file_path": "sample.txt", "old_string": "x", "new_string": "y", "replace_all": true})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("replace_all failed: result=%#v err=%v", result, err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "y\ny\n" {
		t.Fatalf("unexpected edited content: %q", content)
	}
}

func TestValidateShellWorkspaceRejectsOutsideReferences(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Project: config.ProjectConfig{Path: root}}

	if _, err := validateShellWorkspace(cfg, "", "go test ./..."); err != nil {
		t.Fatalf("workspace-local command should be allowed: %v", err)
	}
	if _, err := validateShellWorkspace(cfg, "", "type ..\\secret.txt"); err == nil {
		t.Fatal("expected command path outside workspace to be rejected")
	}
}

func TestValidateShellWorkspaceRejectsCdAndRedirectOutside(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Project: config.ProjectConfig{Path: root}}
	if _, err := validateShellWorkspace(cfg, "", "cd .."); err == nil {
		t.Fatal("expected cd outside workspace to be rejected")
	}
	if _, err := validateShellWorkspace(cfg, "", "echo hi > ..\\outside.txt"); err == nil {
		t.Fatal("expected redirect outside workspace to be rejected")
	}
}

func TestValidateShellWorkspaceAppliesDeniedPaths(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Project: config.ProjectConfig{Path: root, DeniedPaths: []string{".env"}}}
	if _, err := validateShellWorkspace(cfg, "", "rm .env"); err == nil {
		t.Fatal("shell file operation should honor DeniedPaths")
	}
}

func TestAbbreviateToolOutputKeepsHeadTailAndMarker(t *testing.T) {
	content := strings.Repeat("a", 2000) + "TAIL"
	got := abbreviateToolOutput(content, 800)
	if len(got) >= len(content) {
		t.Fatalf("expected abbreviated output, got %d >= %d", len(got), len(content))
	}
	if !strings.Contains(got, "output truncated") {
		t.Fatalf("missing truncation marker: %q", got)
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Fatalf("expected tail to be preserved: %q", got[len(got)-20:])
	}
}

func TestToolOutputMaxBytesUsesConfigAndInputOverride(t *testing.T) {
	cfg := &config.Config{Project: config.ProjectConfig{ToolOutputMaxBytes: 4096}}
	if got := toolOutputMaxBytes(cfg, nil); got != 4096 {
		t.Fatalf("config max output = %d, want 4096", got)
	}
	if got := toolOutputMaxBytes(cfg, map[string]interface{}{"max_output_bytes": float64(2048)}); got != 2048 {
		t.Fatalf("input override max output = %d, want 2048", got)
	}
}
