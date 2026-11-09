//go:build !windows

package ui

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func terminalSize(file *os.File) (int, int) {
	size, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
	if err == nil && size != nil && size.Col > 0 && size.Row > 0 {
		return int(size.Col), int(size.Row)
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
