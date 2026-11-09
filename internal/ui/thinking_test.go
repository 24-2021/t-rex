package ui

import (
	"strings"
	"testing"
	"time"
)

func TestRenderThinkingFrame(t *testing.T) {
	frame := renderThinkingFrame(2)
	for _, expected := range []string{"\r\033[2K", "thinking", ".."} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("thinking frame missing %q: %q", expected, frame)
		}
	}
}

func TestRenderActivityFrameWithToolLabel(t *testing.T) {
	frame := renderActivityFrame(1, "running command")
	for _, expected := range []string{"\r\033[2K", "running command", "."} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("activity frame missing %q: %q", expected, frame)
		}
	}
}

func TestToolActionLabel(t *testing.T) {
	tests := []struct {
		tool  string
		label string
		ok    bool
	}{
		{"Bash", "running command", true},
		{"Shell", "running command", true},
		{"Write", "writing file", true},
		{"Edit", "writing file", true},
		{"Read", "reading file", true},
		{"Grep", "searching", true},
		{"Remember", "saving memory", true},
	}
	for _, tt := range tests {
		label, ok := toolActionLabel(tt.tool)
		if label != tt.label || ok != tt.ok {
			t.Fatalf("%s got (%q, %v), want (%q, %v)", tt.tool, label, ok, tt.label, tt.ok)
		}
	}
}

func TestAnimationDrawsBeforeFastStop(t *testing.T) {
	var out strings.Builder
	anim := startThinkingAnimation(&out, "writing file", func(time.Duration) {})
	anim.Stop()
	if !strings.Contains(out.String(), "writing file") {
		t.Fatalf("expected animation to draw at least one frame, got %q", out.String())
	}
}
