package config

import (
	"regexp"
	"strings"
)

type ShellRiskLevel string

const (
	ShellRiskNormal ShellRiskLevel = "normal"
	ShellRiskHigh   ShellRiskLevel = "high"
)

// ClassifyShellCommand is a conservative permission UX signal, not a shell
// parser or a sandbox boundary.
func ClassifyShellCommand(command string) (ShellRiskLevel, []string) {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" {
		return ShellRiskNormal, nil
	}

	patterns := []struct {
		pattern string
		reason  string
	}{
		{`(^|[\s;&|"'(])(?:rm|del|erase|rmdir|remove-item|ri)(?:[\s;&|"' )]|$)`, "file deletion"},
		{`(^|[\s;&|"'(])(?:mv|move|move-item|rename|ren)(?:[\s;&|"' )]|$)`, "file move/rename"},
		{`(^|[\s;&|"'(])(?:cp|copy|copy-item)(?:[\s;&|"' )]|$)`, "file copy"},
		{`(^|[\s;&|"'(])(?:curl|wget|invoke-webrequest|invoke-restmethod|iwr|irm|ssh|scp|sftp|ftp|nc|ncat|netcat)(?:[\s;&|"' )]|$)`, "network access"},
		{`(^|[\s;&|"'(])(?:start-process|start-job|start-threadjob|nohup|setsid|start)(?:[\s;&|"' )]|$)`, "background process"},
		{`(^|[\s;&|"'(])(?:stop-process|kill|taskkill|shutdown|restart-computer|sudo|chmod|icacls|set-acl|schtasks)(?:[\s;&|"' )]|$)`, "process/system control"},
		{`(?:^|[\s])(?:>>|1>>|2>>|>|1>|2>)`, "file redirection"},
	}

	var reasons []string
	for _, item := range patterns {
		matched, _ := regexp.MatchString(item.pattern, lower)
		if matched && !containsString(reasons, item.reason) {
			reasons = append(reasons, item.reason)
		}
	}
	if strings.Contains(lower, " & ") || strings.HasSuffix(lower, "&") {
		reasons = appendUniqueString(reasons, "background/chained execution")
	}
	if len(reasons) == 0 {
		return ShellRiskNormal, nil
	}
	return ShellRiskHigh, reasons
}

func ShellCommandAllowed(command string, allowlist []string) bool {
	command = strings.TrimSpace(command)
	if command == "" || len(allowlist) == 0 {
		return false
	}
	for _, rule := range allowlist {
		if wildcardMatch(command, strings.TrimSpace(rule)) {
			return true
		}
	}
	return false
}

func isShellToolName(toolName string) bool {
	name := strings.TrimSpace(toolName)
	return strings.EqualFold(name, "shell") || strings.EqualFold(name, "bash")
}

func shellCommandFromInput(input map[string]interface{}) string {
	if input == nil {
		return ""
	}
	command, _ := input["command"].(string)
	return command
}

func shellRiskIsHigh(input map[string]interface{}) bool {
	level, _ := ClassifyShellCommand(shellCommandFromInput(input))
	return level == ShellRiskHigh
}

func wildcardMatch(value, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	expression := regexp.QuoteMeta(pattern)
	expression = strings.ReplaceAll(expression, `\*`, `.*`)
	expression = strings.ReplaceAll(expression, `\?`, `.`)
	matched, err := regexp.MatchString(`(?is)^`+expression+`$`, value)
	return err == nil && matched
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func appendUniqueString(items []string, value string) []string {
	if containsString(items, value) {
		return items
	}
	return append(items, value)
}
