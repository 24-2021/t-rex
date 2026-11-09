package ui

import "github.com/fatih/color"

func (r *REPL) contextStatusString(s string) string {
	theme := "classic"
	if r != nil && r.config != nil && r.config.UI.Theme != "" {
		theme = r.config.UI.Theme
	}
	switch theme {
	case "mono":
		return s
	case "neon":
		return color.New(color.FgHiMagenta, color.Bold).Sprint(s)
	case "cmd-safe":
		return color.New(color.FgHiWhite).Sprint(s)
	default:
		return color.New(color.FgHiCyan).Sprint(s)
	}
}
