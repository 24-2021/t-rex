package ui

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/config"
)

func TestReadMultilineCommandPreservesLines(t *testing.T) {
	repl := &REPL{reader: bufio.NewReader(strings.NewReader("line 1\nline 2\n/end\n"))}
	got, err := repl.readMultilineCommand("/paste 请总结下面文档")
	if err != nil {
		t.Fatal(err)
	}
	want := "请总结下面文档\n\nline 1\nline 2"
	if got != want {
		t.Fatalf("multiline input = %q, want %q", got, want)
	}
}

func TestWriteMultilineFileWritesInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	repl := &REPL{
		config: &config.Config{Project: config.ProjectConfig{Path: root}},
		reader: bufio.NewReader(strings.NewReader("alpha\nbeta\n/end\n")),
	}
	if err := repl.writeMultilineFile("/writepaste docs/report.md"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\nbeta\n" {
		t.Fatalf("written content = %q", string(data))
	}
}

func TestResolveUserWritePathRejectsWorkspaceEscape(t *testing.T) {
	root := t.TempDir()
	repl := &REPL{config: &config.Config{Project: config.ProjectConfig{Path: root}}}
	if _, err := repl.resolveUserWritePath("..\\outside.txt"); err == nil {
		t.Fatal("expected workspace escape to be rejected")
	}
}
