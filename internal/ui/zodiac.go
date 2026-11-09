package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/t-rex-ai/t-rex/internal/config"
)

type zodiacAnimal struct {
	Name string
	Sign string
	Art  []string
}

var zodiacAnimals = [...]zodiacAnimal{
	{Name: "rat", Sign: "RAT", Art: []string{
		`   .-"""-.   `,
		`  /  o o  \  `,
		` |    >    | `,
		` |  \___/  | `,
		`  \_______/  `,
		`    nimble   `,
	}},
	{Name: "ox", Sign: "OX", Art: []string{
		`  \       /  `,
		` .-\ (oo) /-.`,
		`/   \____/   \`,
		`|    ||||    |`,
		`     ||||     `,
		`   steadfast  `,
	}},
	{Name: "tiger", Sign: "TIGER", Art: []string{
		`   /\_/\     `,
		`  ( o.o )    `,
		`  /|  V |\   `,
		` (_|___|_)   `,
		`   /   \     `,
		`    fierce    `,
	}},
	{Name: "rabbit", Sign: "RABBIT", Art: []string{
		`    (\_/)    `,
		`    ('.')    `,
		`   /|___|\   `,
		`  (_|   |_)  `,
		`     / \     `,
		`     quiet    `,
	}},
	{Name: "dragon", Sign: "DRAGON", Art: []string{
		`    /)___    `,
		` .-._   _.-. `,
		`/  <  V  >  \`,
		`\__.-._.-.__/`,
		`    /___\    `,
		`    thunder  `,
	}},
	{Name: "snake", Sign: "SNAKE", Art: []string{
		`     __      `,
		`  __/  \___  `,
		` /  o  o  \ `,
		` \__/\/\__/ `,
		`    \__/    `,
		`     silent  `,
	}},
	{Name: "horse", Sign: "HORSE", Art: []string{
		`   ,--./,-. `,
		`  /   __   \`,
		` |   /  \  |`,
		`  \_/    \_/`,
		`    ||  ||  `,
		`    swift   `,
	}},
	{Name: "goat", Sign: "GOAT", Art: []string{
		`   _/\/\_   `,
		` .' o  o '. `,
		`/  .----.  \`,
		`\  (____)  /`,
		`  '------'  `,
		`    steady  `,
	}},
	{Name: "monkey", Sign: "MONKEY", Art: []string{
		` .-"-.-"-.  `,
		`/  o   o  \ `,
		`|    ^    | `,
		`\  \___/  / `,
		`  '-----'   `,
		`    clever  `,
	}},
	{Name: "rooster", Sign: "ROOSTER", Art: []string{
		`     __     `,
		`  __/o \_   `,
		` /  \__/ \  `,
		` \__/  \_/  `,
		`    /__\    `,
		`    signal  `,
	}},
	{Name: "dog", Sign: "DOG", Art: []string{
		`    / \__   `,
		`   (  @\___ `,
		`   /      O `,
		`  / (____/  `,
		`    /   \   `,
		`    loyal   `,
	}},
	{Name: "pig", Sign: "PIG", Art: []string{
		`  .-~~~~-.  `,
		` .' o  o '. `,
		`/   (..)   \`,
		`\  \____/  /`,
		`  '------'  `,
		`    plenty  `,
	}},
}

var buildInfo = struct {
	sync.RWMutex
	version string
	time    string
}{version: "1.0.2", time: "unknown"}

func SetBuildInfo(version, buildTime string) {
	buildInfo.Lock()
	defer buildInfo.Unlock()
	if strings.TrimSpace(version) != "" {
		buildInfo.version = strings.TrimSpace(version)
	}
	if strings.TrimSpace(buildTime) != "" {
		buildInfo.time = strings.TrimSpace(buildTime)
	}
}

func currentVersion() string {
	buildInfo.RLock()
	defer buildInfo.RUnlock()
	return buildInfo.version
}

func zodiacForHour(hour int) (int, zodiacAnimal) {
	index := (hour - 1) % len(zodiacAnimals)
	if index < 0 {
		index += len(zodiacAnimals)
	}
	return index + 1, zodiacAnimals[index]
}

func zodiacForTime(now time.Time) (int, zodiacAnimal) {
	index := now.Minute() / 5
	if index < 0 {
		index = 0
	}
	if index >= len(zodiacAnimals) {
		index = len(zodiacAnimals) - 1
	}
	return index + 1, zodiacAnimals[index]
}

func (r *REPL) showZodiacAnimation(now time.Time) {
	if r.config == nil || !isStartupEnabled(r.config.UI) || !isInteractiveTerminal(os.Stdout) {
		return
	}
	width, height := terminalSize(os.Stdout)
	playZodiacAnimation(color.Output, now, width, height, r.config.UI.StartupStyle, r.config.UI.StartupArt, r.config.UI.StartupSize, time.Sleep)
}

func playZodiacAnimation(w io.Writer, now time.Time, width int, height int, style string, artID string, size string, sleep func(time.Duration)) {
	slot, animal := startupAnimal(now, artID)
	frames := startupFrames(style)
	var previousLines int
	for frame := 0; frame < frames; frame++ {
		if previousLines > 0 {
			fmt.Fprintf(w, "\033[%dA\033[J", previousLines)
		}
		panel := renderZodiacBootPanel(animal, slot, frame, frames-1, width, height, currentVersion(), style, size)
		fmt.Fprint(w, panel)
		previousLines = strings.Count(panel, "\n")
		sleep(90 * time.Millisecond)
	}
	fmt.Fprintln(w)
}

func renderZodiacBootPanel(animal zodiacAnimal, slot int, frame int, finalFrame int, termWidth int, termHeight int, version string, style string, size string) string {
	if finalFrame <= 0 {
		finalFrame = 0
	}
	progress := frame + 1
	if progress < 1 {
		progress = 1
	}
	if progress > finalFrame+1 {
		progress = finalFrame + 1
	}
	if termWidth <= 0 {
		termWidth = 80
	}
	if termHeight <= 0 {
		termHeight = 24
	}

	panelWidth := zodiacBodyWidth(size)
	lines := []string{
		fmt.Sprintf("T-REX %s", version),
		bootSignalLine(animal, slot, frame, panelWidth),
		zodiacPanelTop(animal, panelWidth),
	}
	for i, line := range animal.Art {
		body := line
		if frame < finalFrame && i >= progress {
			body = codeScanLine(i+slot, len(line))
		} else if frame < finalFrame {
			body = shimmerLine(body, frame+i)
		}
		lines = append(lines, zodiacPanelLine(body, panelWidth))
	}
	lines = append(lines, zodiacPanelBottom(animal, panelWidth))
	if normalizeStartupStyle(style) == "full" {
		lines = append(lines, bootProgressBar(progress, finalFrame+1, termWidth))
	}

	topPadding := (termHeight - len(lines)) / 2
	if topPadding < 0 {
		topPadding = 0
	}

	var b strings.Builder
	for i := 0; i < topPadding; i++ {
		b.WriteByte('\n')
	}
	for _, line := range lines {
		b.WriteString(centerLine(line, termWidth))
		b.WriteByte('\n')
	}
	return b.String()
}

func isStartupEnabled(ui config.UIConfig) bool {
	return ui.ZodiacAnimation && normalizeStartupStyle(ui.StartupStyle) != "off"
}

func normalizeStartupStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "", "full":
		return "full"
	case "compact":
		return "compact"
	case "off", "none", "false":
		return "off"
	default:
		return "full"
	}
}

func startupFrames(style string) int {
	if normalizeStartupStyle(style) == "compact" {
		return 1
	}
	return 5
}

func startupAnimal(now time.Time, artID string) (int, zodiacAnimal) {
	artID = strings.ToLower(strings.TrimSpace(artID))
	if artID != "" && artID != "auto" {
		for i, animal := range zodiacAnimals {
			if animal.Name == artID || strings.ToLower(animal.Sign) == artID {
				return i + 1, animal
			}
		}
	}
	return zodiacForTime(now)
}

func zodiacBodyWidth(size string) int {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "compact", "small":
		return 22
	case "large", "big":
		return 38
	default:
		return 28
	}
}

func zodiacPanelTop(animal zodiacAnimal, bodyWidth int) string {
	title := strings.ToUpper(animal.Name)
	if len(title) > 10 {
		title = title[:10]
	}
	label := fmt.Sprintf(" T-REX ZODIAC : %s ", title)
	return "+" + padRunes("="+label+"=", bodyWidth+4) + "+"
}

func zodiacPanelLine(line string, bodyWidth int) string {
	line = padRunes(trimToWidth(line, bodyWidth), bodyWidth)
	return "|  " + line + "  |"
}

func zodiacPanelBottom(animal zodiacAnimal, bodyWidth int) string {
	mark := strings.ToUpper(animal.Sign)
	if len(mark) > 10 {
		mark = mark[:10]
	}
	label := fmt.Sprintf(" five-minute seal / %s ", mark)
	return "+" + padRunes("-"+label+"-", bodyWidth+4) + "+"
}

func bootSignalLine(animal zodiacAnimal, slot int, frame int, bodyWidth int) string {
	marks := []string{"|", "/", "-", "\\"}
	mark := marks[frame%len(marks)]
	label := fmt.Sprintf("%s boot channel %02d/12 %s", mark, slot, strings.ToUpper(animal.Sign))
	return padRunes(label, bodyWidth+6)
}

func codeScanLine(seed int, width int) string {
	if width < 14 {
		width = 14
	}
	patterns := []string{"01011001", "10100110", "{boot}", "<scan>", "0xT-REX"}
	line := patterns[seed%len(patterns)]
	for len(line) < width {
		line += " " + patterns[(seed+len(line))%len(patterns)]
	}
	if len(line) > width {
		return line[:width]
	}
	return line
}

func shimmerLine(line string, seed int) string {
	runes := []rune(line)
	if len(runes) == 0 {
		return line
	}
	pos := seed % len(runes)
	if runes[pos] == ' ' {
		runes[pos] = '.'
	}
	return string(runes)
}

func padRunes(line string, width int) string {
	count := len([]rune(line))
	if count >= width {
		return line
	}
	left := (width - count) / 2
	right := width - count - left
	return strings.Repeat(" ", left) + line + strings.Repeat(" ", right)
}

func bootProgressBar(progress int, total int, termWidth int) string {
	if total <= 0 {
		total = 1
	}
	if progress > total {
		progress = total
	}
	barWidth := termWidth / 3
	if barWidth < 12 {
		barWidth = 12
	}
	if barWidth > 36 {
		barWidth = 36
	}
	filled := progress * barWidth / total
	return fmt.Sprintf("[%s%s] %3d%%",
		strings.Repeat("#", filled),
		strings.Repeat(".", barWidth-filled),
		progress*100/total,
	)
}

func centerLine(line string, width int) string {
	line = trimToWidth(line, width)
	padding := (width - len([]rune(line))) / 2
	if padding < 0 {
		padding = 0
	}
	return strings.Repeat(" ", padding) + line
}

func trimToWidth(line string, width int) string {
	runes := []rune(line)
	if width <= 0 || len(runes) <= width {
		return line
	}
	return string(runes[:width])
}

func isInteractiveTerminal(file *os.File) bool {
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
