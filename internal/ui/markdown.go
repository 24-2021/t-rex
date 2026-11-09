package ui

import (
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/glamour"
	"github.com/fatih/color"
	"github.com/muesli/termenv"
)

var terminalEscapeSequence = regexp.MustCompile("\x1b(\\[[0-?]*[ -/]*[@-~]|\\][^\\x07]*(\\x07|\\x1b\\\\)|[@-_])")

func renderMarkdown(markdown string) string {
	if strings.TrimSpace(markdown) == "" {
		return ""
	}

	width, _ := terminalSize(os.Stdout)
	if width > 2 {
		width -= 2
	}
	if width < 40 {
		width = 40
	}

	profile := termenv.TrueColor
	if color.NoColor {
		profile = termenv.Ascii
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithColorProfile(profile),
		glamour.WithWordWrap(width),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return markdown
	}

	rendered, err := renderer.Render(stripTerminalControls(markdown))
	if err != nil {
		return markdown
	}
	return strings.TrimRight(rendered, "\r\n") + "\n"
}

func stripTerminalControls(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = terminalEscapeSequence.ReplaceAllString(text, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}
