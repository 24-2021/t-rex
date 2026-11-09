//go:build windows

package ui

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

func terminalSize(file *os.File) (int, int) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(file.Fd()), &info); err == nil {
		width := int(info.Window.Right - info.Window.Left + 1)
		height := int(info.Window.Bottom - info.Window.Top + 1)
		if width > 0 && height > 0 {
			return width, height
		}
	}
	return fallbackTerminalSize()
}

func fallbackTerminalSize() (int, int) {
	return envInt("COLUMNS", 80), envInt("LINES", 24)
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
