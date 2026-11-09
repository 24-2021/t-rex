//go:build windows

package termui

import (
	"os"

	"golang.org/x/sys/windows"
)

const (
	enableProcessedInput       = 0x0001
	enableLineInput            = 0x0002
	enableEchoInput            = 0x0004
	enableVirtualTerminalInput = 0x0200
)

func makeRaw() (func(), bool) {
	handle := windows.Handle(os.Stdin.Fd())
	var oldMode uint32
	if err := windows.GetConsoleMode(handle, &oldMode); err != nil {
		return func() {}, false
	}
	newMode := oldMode
	newMode &^= enableLineInput | enableEchoInput
	newMode |= enableProcessedInput | enableVirtualTerminalInput
	if err := windows.SetConsoleMode(handle, newMode); err != nil {
		newMode = oldMode &^ (enableLineInput | enableEchoInput)
		newMode |= enableProcessedInput
		if retryErr := windows.SetConsoleMode(handle, newMode); retryErr != nil {
			return func() {}, false
		}
	}
	return func() {
		_ = windows.SetConsoleMode(handle, oldMode)
	}, true
}
