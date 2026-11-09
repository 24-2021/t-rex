package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestZodiacForHour(t *testing.T) {
	tests := []struct {
		hour int
		slot int
		sign string
	}{
		{0, 12, "PIG"},
		{1, 1, "RAT"},
		{2, 2, "OX"},
		{12, 12, "PIG"},
		{13, 1, "RAT"},
		{23, 11, "DOG"},
	}
	for _, tt := range tests {
		slot, animal := zodiacForHour(tt.hour)
		if slot != tt.slot || animal.Sign != tt.sign {
			t.Errorf("hour %d got (%d, %s), want (%d, %s)", tt.hour, slot, animal.Sign, tt.slot, tt.sign)
		}
	}
}

func TestZodiacForTimeUsesFiveMinuteSlots(t *testing.T) {
	tests := []struct {
		minute int
		slot   int
		sign   string
	}{
		{0, 1, "RAT"},
		{4, 1, "RAT"},
		{5, 2, "OX"},
		{10, 3, "TIGER"},
		{55, 12, "PIG"},
	}
	for _, tt := range tests {
		now := time.Date(2026, 7, 10, 15, tt.minute, 0, 0, time.Local)
		slot, animal := zodiacForTime(now)
		if slot != tt.slot || animal.Sign != tt.sign {
			t.Errorf("minute %d got (%d, %s), want (%d, %s)", tt.minute, slot, animal.Sign, tt.slot, tt.sign)
		}
	}
}

func TestPlayZodiacAnimation(t *testing.T) {
	var output bytes.Buffer
	now := time.Date(2026, 7, 10, 15, 8, 0, 0, time.Local)
	SetBuildInfo("1.0.2", "test")
	playZodiacAnimation(&output, now, 80, 24, "full", "auto", "normal", func(time.Duration) {})
	for _, expected := range []string{"T-REX 1.0.2", "T-REX ZODIAC", "OX", "02/12", "[", "%"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("animation output missing %q: %q", expected, output.String())
		}
	}
	for _, forbidden := range []string{"zodiac.render", "03/12", "15:08"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("animation output should not include %q: %q", forbidden, output.String())
		}
	}
}

func TestStartupFixedArtAndCompactMode(t *testing.T) {
	var output bytes.Buffer
	now := time.Date(2026, 7, 10, 15, 8, 0, 0, time.Local)
	playZodiacAnimation(&output, now, 80, 24, "compact", "dragon", "normal", func(time.Duration) {})
	text := output.String()
	if strings.Contains(text, "[") || strings.Contains(text, "%") {
		t.Fatalf("compact startup should not render progress bar: %q", text)
	}
	if !strings.Contains(text, "DRAGON") || !strings.Contains(text, "/)___") {
		t.Fatalf("fixed dragon art was not rendered: %q", text)
	}
}

func TestEveryZodiacHasBootArt(t *testing.T) {
	for _, animal := range zodiacAnimals {
		if animal.Name == "" || animal.Sign == "" || len(animal.Art) < 6 {
			t.Fatalf("zodiac animal has incomplete metadata: %#v", animal)
		}
	}
}
