package cmd

import (
	"testing"
	"time"
)

func TestFormatSessionTime(t *testing.T) {
	when := time.Date(2026, 7, 13, 20, 57, 30, 0, time.Local)
	if got := formatSessionTime(when); got != "2026-07-13 20:57" {
		t.Fatalf("formatSessionTime() = %q, want %q", got, "2026-07-13 20:57")
	}
}

func TestFormatSessionTimeZero(t *testing.T) {
	if got := formatSessionTime(time.Time{}); got != "未知" {
		t.Fatalf("formatSessionTime(zero) = %q, want %q", got, "未知")
	}
}

func TestMaskSecretHandlesShortValues(t *testing.T) {
	tests := map[string]string{
		"":              "",
		"abc":           "***",
		"abcdef":        "**cdef",
		"short-key":     "*****-key",
		"sk-1234567890": "sk-12345...7890",
		"密钥123456789":   "*******6789",
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := maskSecret(input); got != want {
				t.Fatalf("maskSecret(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestCompleteWorkModeFiltersByPrefix(t *testing.T) {
	got, directive := completeWorkMode(nil, nil, "code-")
	if directive == 0 {
		t.Fatalf("expected no-file completion directive")
	}
	if len(got) != 2 {
		t.Fatalf("completion count = %d, want 2: %#v", len(got), got)
	}
	if got[0][:10] != "code-audit" || got[1][:11] != "code-review" {
		t.Fatalf("unexpected completions: %#v", got)
	}
}
