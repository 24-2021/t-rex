package termui

import "testing"

func TestSelectWithReaderDefaultsOnEnter(t *testing.T) {
	lines := []string{"\n"}
	got, err := SelectWithReader("choose", testChoices(), 1, lineReader(lines))
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("selected = %d, want 1", got)
	}
}

func TestSelectWithReaderSupportsNumber(t *testing.T) {
	lines := []string{"3\n"}
	got, err := SelectWithReader("choose", testChoices(), 0, lineReader(lines))
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("selected = %d, want 2", got)
	}
}

func TestSelectWithReaderSupportsJKNavigation(t *testing.T) {
	lines := []string{"j\n", "j\n", "k\n", "\n"}
	got, err := SelectWithReader("choose", testChoices(), 0, lineReader(lines))
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("selected = %d, want 1", got)
	}
}

func TestParseKeySequenceSupportsArrowKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		wantKey  string
		wantUsed int
	}{
		{name: "ansi up", input: []byte{27, '[', 'A'}, wantKey: "up", wantUsed: 3},
		{name: "ansi down", input: []byte{27, '[', 'B'}, wantKey: "down", wantUsed: 3},
		{name: "windows up", input: []byte{224, 72}, wantKey: "up", wantUsed: 2},
		{name: "windows down", input: []byte{224, 80}, wantKey: "down", wantUsed: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotUsed, needMore := parseKeySequence(tt.input)
			if needMore {
				t.Fatal("needMore = true, want false")
			}
			if gotKey != tt.wantKey || gotUsed != tt.wantUsed {
				t.Fatalf("parseKeySequence() = (%q, %d), want (%q, %d)", gotKey, gotUsed, tt.wantKey, tt.wantUsed)
			}
		})
	}
}

func TestParseKeySequenceWaitsForPartialArrows(t *testing.T) {
	tests := [][]byte{
		{27},
		{27, '['},
		{224},
	}
	for _, input := range tests {
		if key, used, needMore := parseKeySequence(input); key != "" || used != 0 || !needMore {
			t.Fatalf("parseKeySequence(%v) = (%q, %d, %t), want partial", input, key, used, needMore)
		}
	}
}

func testChoices() []Choice {
	return []Choice{
		{Label: "one", Value: "one"},
		{Label: "two", Value: "two"},
		{Label: "three", Value: "three"},
	}
}

func lineReader(lines []string) func() (string, error) {
	index := 0
	return func() (string, error) {
		if index >= len(lines) {
			return "\n", nil
		}
		line := lines[index]
		index++
		return line, nil
	}
}
