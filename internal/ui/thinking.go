package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

var thinkingFrames = []string{
	"✧       ",
	"✧ ✦     ",
	"✧ ✦ ✶   ",
	"  ✧ ✦ ✶ ",
	"    ✦ ✶ ",
	"      ✶ ",
}

type thinkingAnimation struct {
	stop        chan struct{}
	done        chan struct{}
	once        sync.Once
	startedAt   time.Time
	minDuration time.Duration
	sleep       func(time.Duration)
}

func (r *REPL) startThinkingAnimation() *thinkingAnimation {
	if r.config == nil || !r.config.UI.ThinkingAnimation || !isInteractiveTerminal(os.Stdout) {
		return nil
	}
	return startThinkingAnimation(color.Output, "thinking | "+r.compactContextStatus(), time.Sleep)
}

func (r *REPL) startToolAnimation(toolName string) *thinkingAnimation {
	if r.config == nil || !r.config.UI.ThinkingAnimation || !isInteractiveTerminal(os.Stdout) {
		return nil
	}
	label, ok := toolActionLabel(toolName)
	if !ok {
		return nil
	}
	return startThinkingAnimation(color.Output, label+" | "+r.compactContextStatus(), time.Sleep)
}

func startThinkingAnimation(w io.Writer, label string, sleep func(time.Duration)) *thinkingAnimation {
	anim := &thinkingAnimation{
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
		startedAt:   time.Now(),
		minDuration: 320 * time.Millisecond,
		sleep:       sleep,
	}
	go anim.run(w, label, sleep)
	return anim
}

func (a *thinkingAnimation) Stop() {
	if a == nil {
		return
	}
	a.once.Do(func() {
		close(a.stop)
		<-a.done
	})
}

func (a *thinkingAnimation) run(w io.Writer, label string, sleep func(time.Duration)) {
	defer close(a.done)
	for i := 0; ; i++ {
		fmt.Fprint(w, renderActivityFrame(i, label))
		sleep(110 * time.Millisecond)
		select {
		case <-a.stop:
			a.waitMinimumVisible()
			fmt.Fprint(w, "\r\033[2K")
			return
		default:
		}
	}
}

func (a *thinkingAnimation) waitMinimumVisible() {
	if a == nil || a.sleep == nil || a.minDuration <= 0 {
		return
	}
	if remaining := a.minDuration - time.Since(a.startedAt); remaining > 0 {
		a.sleep(remaining)
	}
}

func renderThinkingFrame(index int) string {
	return renderActivityFrame(index, "thinking")
}

func renderActivityFrame(index int, label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "thinking"
	}
	frame := thinkingFrames[index%len(thinkingFrames)]
	return fmt.Sprintf("\r\033[2K  %s %s %s", frame, label, strings.Repeat(".", index%4))
}

func toolActionLabel(toolName string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "shell", "bash":
		return "running command", true
	case "write", "edit":
		return "writing file", true
	case "read":
		return "reading file", true
	case "grep", "glob":
		return "searching", true
	case "remember":
		return "saving memory", true
	case "skills_read", "skills_readresource", "skills_list":
		return "loading skill", true
	default:
		return "", false
	}
}
