package main

import (
	"os"

	"github.com/t-rex-ai/t-rex/cmd"
)

// Version information is injected via ldflags in release builds.
var version = "1.0.2"
var buildTime = "unknown"

func main() {
	if err := cmd.Execute(version, buildTime); err != nil {
		os.Exit(1)
	}
}
