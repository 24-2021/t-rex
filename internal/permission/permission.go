package permission

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/t-rex-ai/t-rex/internal/config"
	"github.com/t-rex-ai/t-rex/internal/termui"
)

// Checker evaluates tool permissions and asks for user approval when needed.
type Checker struct {
	config   *config.PermissionConfig
	readLine func() (string, error)
}

func NewChecker(cfg *config.PermissionConfig) *Checker {
	return &Checker{config: cfg}
}

func (c *Checker) SetInputReader(readLine func() (string, error)) {
	c.readLine = readLine
}

func (c *Checker) CheckPermission(toolName string, input map[string]interface{}) (bool, string) {
	if len(c.config.BypassPermissionsForUsers) > 0 {
		currentUser := getCurrentUser()
		for _, user := range c.config.BypassPermissionsForUsers {
			if currentUser == user {
				return true, "bypass (user whitelisted)"
			}
		}
	}
	return c.config.IsToolAllowedInput(toolName, input)
}

func (c *Checker) RequestPermission(toolName string, input map[string]interface{}) (bool, error) {
	if c.IsBypassMode() {
		return true, nil
	}

	inputStr := formatInputForDisplay(input)
	if isShellTool(toolName) {
		if level, reasons := config.ClassifyShellCommand(shellCommandInput(input)); level == config.ShellRiskHigh {
			color.Red("\n[high-risk shell: %s] %s", strings.Join(reasons, ", "), toolName)
		}
	}

	color.Yellow("\n权限请求: %s", toolName)
	color.White("操作详情:\n%s", inputStr)

	choices := []termui.Choice{
		{Label: "拒绝", Description: "不执行这次操作", Value: "deny"},
		{Label: "继续一次", Description: "只允许本次操作", Value: "allow"},
		{Label: "始终允许", Description: "加入本次会话 allow 规则", Value: "always"},
	}
	choiceIndex := 0
	var err error
	if c.readLine == nil {
		choiceIndex, err = termui.Select("是否继续执行这个工具调用？", choices, 0)
	} else {
		choiceIndex, err = termui.SelectWithReader("是否继续执行这个工具调用？", choices, 0, c.readLine)
	}
	if err != nil {
		return false, fmt.Errorf("read user input failed: %w", err)
	}

	switch choices[choiceIndex].Value {
	case "allow":
		color.Green("已允许")
		return true, nil
	case "always":
		if c.config.ManagedRulesOnly {
			color.Green("approved for this operation; managed rules disable persistent grants")
			return true, nil
		}
		color.Green("已添加到允许列表")
		c.config.Allow = append(c.config.Allow, toolName)
		return true, nil
	case "deny":
		color.Red("已拒绝")
		return false, nil
	default:
		color.Red("已拒绝")
		return false, nil
	}
}

func (c *Checker) SetBypassMode(enabled bool) error {
	if (c.config.DisableBypassMode || c.config.ManagedRulesOnly) && enabled {
		return fmt.Errorf("bypass mode is disabled by configuration")
	}
	if enabled {
		c.config.Mode = "bypass"
	} else {
		c.config.Mode = "default"
	}
	return nil
}

func isShellTool(toolName string) bool {
	name := strings.TrimSpace(toolName)
	return strings.EqualFold(name, "shell") || strings.EqualFold(name, "bash")
}

func shellCommandInput(input map[string]interface{}) string {
	if input == nil {
		return ""
	}
	command, _ := input["command"].(string)
	return command
}

func (c *Checker) IsBypassMode() bool {
	return c.config.Mode == "bypass"
}

func (c *Checker) GetCurrentPermissionMode() string {
	return c.config.Mode
}

func formatInputForDisplay(input map[string]interface{}) string {
	if input == nil || len(input) == 0 {
		return "(no parameters)"
	}

	var result strings.Builder
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	for _, key := range keys {
		valueStr := formatValue(input[key])
		if len(valueStr) > 200 {
			valueStr = valueStr[:200] + "... (truncated)"
		}
		fmt.Fprintf(&result, "  - %s: %s\n", key, valueStr)
	}

	return result.String()
}

func formatValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == float64(int(v)) {
			return fmt.Sprintf("%d", int(v))
		}
		return fmt.Sprintf("%.2f", v)
	case bool:
		return fmt.Sprintf("%t", v)
	case []interface{}:
		if len(v) == 0 {
			return "[]"
		}
		if len(v) <= 3 {
			items := make([]string, len(v))
			for i, item := range v {
				items[i] = formatValue(item)
			}
			return "[" + strings.Join(items, ", ") + "]"
		}
		return fmt.Sprintf("[%d items]", len(v))
	case map[string]interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return "{...}"
		}
		result := string(data)
		if len(result) > 100 {
			return result[:100] + "..."
		}
		return result
	default:
		return fmt.Sprintf("%v", value)
	}
}

func getCurrentUser() string {
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	if user := os.Getenv("USERNAME"); user != "" {
		return user
	}
	return "unknown"
}
