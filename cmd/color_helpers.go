package cmd

import "github.com/fatih/color"

func grayString(format string, a ...interface{}) string {
	return color.New(color.FgHiBlack).Sprintf(format, a...)
}
