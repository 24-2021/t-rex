//go:build windows

package ui

import (
	"os"

	"github.com/fatih/color"
	"golang.org/x/sys/windows"
)

const enableVirtualTerminalProcessing uint32 = 0x0004
const utf8CodePage uint32 = 65001

var (
	setConsoleOutputCP = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleOutputCP")
	setConsoleCP       = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCP")
)

func enableTerminalStyling() {
	_, _, _ = setConsoleOutputCP.Call(uintptr(utf8CodePage))
	_, _, _ = setConsoleCP.Call(uintptr(utf8CodePage))
	enableVirtualTerminal(os.Stdout)
	enableVirtualTerminal(os.Stderr)
	color.NoColor = false
}

func enableVirtualTerminal(file *os.File) {
	handle := windows.Handle(file.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return
	}
	_ = windows.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing)
}
