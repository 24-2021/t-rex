package ui

import (
	"regexp"
	"strings"
	"testing"
)

var renderedANSI = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")

func TestRenderMarkdownFormatsCommonGFM(t *testing.T) {
	input := "# 项目说明\n\n**重点内容**，包含 `inline code` 和 [文档链接](https://example.com/docs)。\n\n- 第一项\n- 第二项\n\n```go\nfunc main() {}\n```\n\n| 名称 | 状态 |\n| --- | --- |\n| 测试 | 通过 |"
	got := renderedANSI.ReplaceAllString(renderMarkdown(input), "")

	for _, want := range []string{"项目说明", "重点内容", "inline code", "文档链接", "第一项", "func main()", "测试", "通过"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered Markdown missing %q:\n%s", want, got)
		}
	}
	for _, marker := range []string{"**重点内容**", "`inline code`", "```go", "| 名称 | 状态 |"} {
		if strings.Contains(got, marker) {
			t.Errorf("Markdown marker %q was not rendered:\n%s", marker, got)
		}
	}
}

func TestStripTerminalControls(t *testing.T) {
	input := "safe\x1b[31mred\x1b[0m\x07\r\nnext\rline"
	got := stripTerminalControls(input)
	if got != "safered\nnextline" {
		t.Fatalf("stripTerminalControls = %q, want %q", got, "safered\nnextline")
	}
}
