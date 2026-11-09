package termui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Choice struct {
	Label       string
	Description string
	Value       string
}

func Select(title string, choices []Choice, defaultIndex int) (int, error) {
	if len(choices) == 0 {
		return -1, fmt.Errorf("no choices")
	}
	if defaultIndex < 0 || defaultIndex >= len(choices) {
		defaultIndex = 0
	}
	restore, ok := makeRaw()
	if !ok {
		return SelectWithReader(title, choices, defaultIndex, func() (string, error) {
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			return line, err
		})
	}
	defer restore()

	selected := defaultIndex
	fmt.Println()
	renderChoices(title, choices, selected, false)
	defer fmt.Print("\033[?25h")

	var pending []byte
	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return selected, err
		}
		if n == 0 {
			continue
		}
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			key, consumed, needMore := parseKeySequence(pending)
			if needMore {
				break
			}
			if consumed <= 0 {
				pending = pending[1:]
				continue
			}
			pending = pending[consumed:]
			switch key {
			case "up":
				if selected > 0 {
					selected--
				} else {
					selected = len(choices) - 1
				}
				renderChoices(title, choices, selected, true)
			case "down", "tab":
				if selected < len(choices)-1 {
					selected++
				} else {
					selected = 0
				}
				renderChoices(title, choices, selected, true)
			case "enter":
				fmt.Println()
				return selected, nil
			case "ctrl-c", "esc":
				fmt.Println()
				return selected, fmt.Errorf("cancelled")
			}
		}
	}
}

func SelectWithReader(title string, choices []Choice, defaultIndex int, readLine func() (string, error)) (int, error) {
	if len(choices) == 0 {
		return -1, fmt.Errorf("no choices")
	}
	if defaultIndex < 0 || defaultIndex >= len(choices) {
		defaultIndex = 0
	}
	selected := defaultIndex
	defer fmt.Print("\033[?25h")
	for {
		fmt.Println()
		renderChoices(title, choices, selected, false)
		fmt.Print("选择编号，或输入 j/k 后回车切换，直接回车确认: ")
		line, err := readLine()
		if err != nil {
			return selected, err
		}
		input := strings.TrimSpace(strings.ToLower(line))
		switch input {
		case "":
			return selected, nil
		case "j", "down", "\x1b[b":
			if selected < len(choices)-1 {
				selected++
			} else {
				selected = 0
			}
		case "k", "up", "\x1b[a":
			if selected > 0 {
				selected--
			} else {
				selected = len(choices) - 1
			}
		default:
			n, convErr := strconv.Atoi(input)
			if convErr == nil && n >= 1 && n <= len(choices) {
				return n - 1, nil
			}
			for i, choice := range choices {
				if input == strings.ToLower(choice.Value) || input == strings.ToLower(choice.Label) {
					return i, nil
				}
			}
			fmt.Printf("无效选择: %s\n", strings.TrimSpace(line))
		}
	}
}

func renderChoices(title string, choices []Choice, selected int, redraw bool) {
	if redraw {
		fmt.Printf("\033[%dA", len(choices)+2)
	}
	fmt.Print("\033[?25l")
	fmt.Printf("\033[K%s\n", title)
	fmt.Print("\033[K使用 ↑/↓ 选择，Enter 确认，Tab 切换\n")
	for i, choice := range choices {
		prefix := "  "
		if i == selected {
			prefix = "> "
		}
		line := fmt.Sprintf("%s%d. %s", prefix, i+1, choice.Label)
		if strings.TrimSpace(choice.Description) != "" {
			line += " - " + choice.Description
		}
		fmt.Printf("\033[K%s\n", line)
	}
}

func parseKey(data []byte) string {
	key, _, needMore := parseKeySequence(data)
	if needMore {
		return ""
	}
	return key
}

func parseKeySequence(data []byte) (key string, consumed int, needMore bool) {
	if len(data) == 0 {
		return "", 0, true
	}
	if data[0] == 3 {
		return "ctrl-c", 1, false
	}
	if data[0] == '\r' || data[0] == '\n' {
		return "enter", 1, false
	}
	if data[0] == '\t' {
		return "tab", 1, false
	}
	if data[0] == 0 || data[0] == 224 {
		if len(data) < 2 {
			return "", 0, true
		}
		switch data[1] {
		case 72:
			return "up", 2, false
		case 80:
			return "down", 2, false
		}
		return "", 2, false
	}
	if data[0] == 27 {
		if len(data) == 1 {
			return "", 0, true
		}
		if data[1] == '[' && len(data) < 3 {
			return "", 0, true
		}
		if len(data) >= 3 && data[1] == '[' {
			switch data[2] {
			case 'A':
				return "up", 3, false
			case 'B':
				return "down", 3, false
			}
			return "", 3, false
		}
		return "esc", 1, false
	}
	switch strings.ToLower(string(data)) {
	case "k":
		return "up", 1, false
	case "j":
		return "down", 1, false
	}
	return "", 1, false
}
