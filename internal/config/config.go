package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Config 应用程序主配置结构
type Config struct {
	AI          AIConfig         `json:"ai"`
	Project     ProjectConfig    `json:"project"`
	Permissions PermissionConfig `json:"permissions"`
	UI          UIConfig         `json:"ui"`
	Session     SessionConfig    `json:"session"`
	MCP         MCPConfig        `json:"mcp"`
	Memory      MemoryConfig     `json:"memory"`
	Skills      SkillsConfig     `json:"skills"`
	Hooks       HooksConfig      `json:"hooks"`
	Audit       AuditConfig      `json:"audit"`
	ConfigPath  string           `json:"-"`
}

// AIConfig AI 相关配置
type AIConfig struct {
	Provider           string            `json:"provider,omitempty"` // "anthropic" | "openai-compatible"
	APIKey             string            `json:"apiKey,omitempty"`
	Model              string            `json:"model"`
	MaxTokens          int               `json:"maxTokens"`
	ContextWindow      int               `json:"contextWindow"`
	Temperature        float64           `json:"temperature"`
	TopP               float64           `json:"topP"`
	BaseURL            string            `json:"baseURL,omitempty"`
	Headers            map[string]string `json:"headers,omitempty"`
	TimeoutSeconds     int               `json:"timeoutSeconds"`
	MaxRetries         int               `json:"maxRetries"`
	MaxToolRounds      int               `json:"maxToolRounds"`
	AllowTextToolCalls bool              `json:"allowTextToolCalls"`
	WorkMode           string            `json:"workMode,omitempty"`
	AgentMode          string            `json:"agentMode,omitempty"` // "normal", "plan", "goal"
	SystemPrompt       string            `json:"systemPrompt,omitempty"`
	CustomInstructions string            `json:"customInstructions,omitempty"`
}

// Agent workflow modes. These are distinct from permission modes:
//   - normal: the agent researches and executes within the configured permissions.
//   - plan:   read-only research that must end with a structured plan awaiting user approval.
//   - goal:   objective + acceptance-criteria driven autonomy until done or blocked.
const (
	AgentModeNormal = "normal"
	AgentModePlan   = "plan"
	AgentModeGoal   = "goal"
)

// ProjectConfig 项目相关配置
type ProjectConfig struct {
	Path               string   `json:"path,omitempty"`
	AllowedPaths       []string `json:"allowedPaths,omitempty"`
	DeniedPaths        []string `json:"deniedPaths,omitempty"`
	IgnorePatterns     []string `json:"ignorePatterns"`
	MaxFileSize        int64    `json:"maxFileSize"`        // bytes
	ToolOutputMaxBytes int      `json:"toolOutputMaxBytes"` // bytes sent back to the model
}

// PermissionConfig 权限配置
type PermissionConfig struct {
	Mode                      string   `json:"mode"` // "default", "bypass", "plan"
	Allow                     []string `json:"allow"`
	Deny                      []string `json:"deny"`
	Ask                       []string `json:"ask"`
	BypassPermissionsForUsers []string `json:"bypassPermissionsForUsers,omitempty"`
	DisableBypassMode         bool     `json:"disableBypassMode"`
	ManagedRulesOnly          bool     `json:"allowManagedPermissionRulesOnly"`
	ShellRiskProfile          string   `json:"shellRiskProfile"` // "default", "strict", "allowlist"
	ShellAllowlist            []string `json:"shellAllowlist,omitempty"`
}

// UIConfig 用户界面配置
type UIConfig struct {
	ColorScheme       string `json:"colorScheme"`  // "auto", "light", "dark"
	OutputFormat      string `json:"outputFormat"` // "streaming", "block"
	ShowTokenUsage    bool   `json:"showTokenUsage"`
	ShowTiming        bool   `json:"showTiming"`
	ToolOutput        string `json:"toolOutput"` // "compact", "full", "hidden"
	ZodiacAnimation   bool   `json:"zodiacAnimation"`
	StartupStyle      string `json:"startupStyle"` // "full", "compact", "off"
	StartupArt        string `json:"startupArt"`   // "auto" or a fixed art id such as "tiger"
	StartupSize       string `json:"startupSize"`  // "compact", "normal", "large"
	ThinkingAnimation bool   `json:"thinkingAnimation"`
	Theme             string `json:"theme"`
	EditorCommand     string `json:"editorCommand,omitempty"`
	PagerCommand      string `json:"pagerCommand,omitempty"`
}

// SessionConfig 会话配置
type SessionConfig struct {
	StoragePath      string `json:"storagePath,omitempty"`
	MaxHistorySize   int    `json:"maxHistorySize"`
	AutoSaveInterval int    `json:"autoSaveInterval"` // seconds
	CompressHistory  bool   `json:"compressHistory"`
}

// MemoryConfig controls project instructions and auto memory loading.
type MemoryConfig struct {
	Enabled             bool     `json:"enabled"`
	AutoMemoryEnabled   bool     `json:"autoMemoryEnabled"`
	AutoMemoryDirectory string   `json:"autoMemoryDirectory,omitempty"`
	AdditionalFiles     []string `json:"additionalFiles,omitempty"`
	MaxBytes            int64    `json:"maxBytes"`
	MaxLines            int      `json:"maxLines"`
}

// SkillsConfig controls local Claude Code style skill discovery.
type SkillsConfig struct {
	Enabled bool     `json:"enabled"`
	Paths   []string `json:"paths,omitempty"`
}

// MCPConfig MCP 服务器配置
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Type    string            `json:"type,omitempty"` // "stdio", "http", "streamable-http", "sse"
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"` // for HTTP/SSE mode
	Headers map[string]string `json:"headers,omitempty"`
	Enabled bool              `json:"enabled"`
}

type MCPConfig struct {
	Servers []MCPServerConfig `json:"servers"`
}

// HookConfig Hook 配置
type HookConfig struct {
	Event       string `json:"event"`   // "SessionStart", "UserPromptSubmit", etc.
	Matcher     string `json:"matcher"` // 工具名模式
	Command     string `json:"command"` // 要执行的命令
	AsyncRewake bool   `json:"asyncRewake,omitempty"`
	Timeout     int    `json:"timeout"` // seconds
	Enabled     bool   `json:"enabled"`
}

type HooksConfig struct {
	Hooks []HookConfig `json:"hooks"`
}

type AuditConfig struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path,omitempty"`
}

// Default 返回默认配置
func Default() *Config {
	return &Config{
		AI: AIConfig{
			Provider:       "anthropic",
			Model:          "claude-sonnet-4-20250514",
			MaxTokens:      8192,
			ContextWindow:  200000,
			Temperature:    1.0,
			TopP:           1.0,
			TimeoutSeconds: 300,
			MaxRetries:     3,
			MaxToolRounds:  16,
			WorkMode:       "coding",
		},
		Project: ProjectConfig{
			IgnorePatterns: []string{
				".git/",
				"node_modules/",
				"__pycache__/",
				".DS_Store",
				"*.exe",
				"*.dll",
				"*.so",
				"*.dylib",
				"dist/",
				"build/",
				".cache/",
			},
			MaxFileSize:        100 * 1024, // 100KB 默认限制
			ToolOutputMaxBytes: 64 * 1024,
		},
		Permissions: PermissionConfig{
			Mode:             "default",
			Allow:            []string{"Read", "Glob", "Grep", "LSP", "Skills_List", "Skills_Read", "Skills_ReadResource", "Remember", "Todo", "Mission", "Scope", "Finding", "Evidence", "AssessmentPlan", "SecurityCoverage", "SecurityCatalog", "SecurityTest", "CodeAuditCatalog"},
			Ask:              []string{"Shell", "Bash", "Write", "Edit", "MCP_Install", "Skills_Install", "mcp__*"},
			Deny:             []string{},
			ShellRiskProfile: "default",
		},
		UI: UIConfig{
			ColorScheme:       "auto",
			OutputFormat:      "streaming",
			ShowTokenUsage:    true,
			ShowTiming:        true,
			ToolOutput:        "compact",
			ZodiacAnimation:   true,
			StartupStyle:      "full",
			StartupArt:        "auto",
			StartupSize:       "normal",
			ThinkingAnimation: true,
			Theme:             "classic",
		},
		Session: SessionConfig{
			MaxHistorySize:   100,
			AutoSaveInterval: 30,
			CompressHistory:  true,
		},
		MCP: MCPConfig{
			Servers: []MCPServerConfig{},
		},
		Memory: MemoryConfig{
			Enabled:           true,
			AutoMemoryEnabled: true,
			MaxBytes:          25 * 1024,
			MaxLines:          200,
		},
		Skills: SkillsConfig{
			Enabled: true,
			Paths:   []string{},
		},
		Hooks: HooksConfig{
			Hooks: []HookConfig{},
		},
		Audit: AuditConfig{
			Enabled: true,
			Path:    ".cache/audit.jsonl",
		},
	}
}

// Load 加载配置文件
func Load(configPath string) (*Config, error) {
	cfg := Default()

	// 确定配置文件路径
	if configPath == "" {
		configPath = GetDefaultConfigPath()
	}
	cfg.ConfigPath = configPath

	// 尝试读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在，使用默认值并尝试从环境变量加载
			loadFromEnvironment(cfg)
			cfg.AI.WorkMode = NormalizeWorkMode(cfg.AI.WorkMode)
			return cfg, nil
		}
		return nil, err
	}

	// 解析 JSON
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// 从环境变量覆盖（环境变量优先级更高）
	loadFromEnvironment(cfg)
	cfg.AI.WorkMode = NormalizeWorkMode(cfg.AI.WorkMode)
	cfg.ConfigPath = configPath

	return cfg, nil
}

// loadFromEnvironment 从环境变量加载敏感配置
func loadFromEnvironment(cfg *Config) {
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		cfg.AI.APIKey = apiKey
	}
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		cfg.AI.APIKey = apiKey
	}
	if baseURL := os.Getenv("ANTHROPIC_BASE_URL"); baseURL != "" {
		cfg.AI.BaseURL = baseURL
	}
	if baseURL := os.Getenv("T_REX_BASE_URL"); baseURL != "" {
		cfg.AI.BaseURL = baseURL
	}
	if model := os.Getenv("T_REX_MODEL"); model != "" {
		cfg.AI.Model = model
	}
	if provider := os.Getenv("T_REX_PROVIDER"); provider != "" {
		cfg.AI.Provider = provider
	}
	if workMode := os.Getenv("T_REX_WORK_MODE"); workMode != "" {
		cfg.AI.WorkMode = NormalizeWorkMode(workMode)
	}
	if agentMode := os.Getenv("T_REX_AGENT_MODE"); agentMode != "" {
		cfg.AI.AgentMode = NormalizeAgentMode(agentMode)
	}
}

// NormalizeWorkMode returns the canonical agent work mode name.
func NormalizeWorkMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "coding", "code", "dev", "default":
		return "coding"
	case "bugfix", "bug", "fix":
		return "bugfix"
	case "code-review", "codereview", "review":
		return "code-review"
	case "code-audit", "codeaudit", "audit", "security-audit", "secure-code-review", "sast", "whitebox", "white-box", "源码审计", "代码审计", "白盒审计":
		return "code-audit"
	case "incident", "incident-response", "ir", "dfir":
		return "incident-response"
	case "malware", "malware-analysis":
		return "malware-analysis"
	case "osint":
		return "osint"
	case "ctf":
		return "ctf"
	case "ai-security", "ai-security-test", "llm-security", "llm-redteam", "agent-security", "agent-redteam", "prompt-security":
		return "ai-security"
	case "pentest", "pen-test", "penetration", "penetration-test", "redteam", "red-team", "security", "sec", "渗透", "渗透测试", "红队", "红队打点":
		return "pentest"
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

// IsKnownWorkMode reports whether mode is supported by T-REX.
func IsKnownWorkMode(mode string) bool {
	switch NormalizeWorkMode(mode) {
	case "coding", "pentest", "ai-security", "bugfix", "code-review", "code-audit", "incident-response", "malware-analysis", "osint", "ctf":
		return true
	default:
		return false
	}
}

// NormalizeAgentMode returns the canonical agent workflow mode name.
func NormalizeAgentMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "normal", "default", "exec", "execute", "act", "work":
		return AgentModeNormal
	case "plan", "planning", "plan-mode", "readonly", "read-only", "计划", "计划模式":
		return AgentModePlan
	case "goal", "goals", "goal-mode", "auto", "autonomous", "loop", "目标", "目标模式":
		return AgentModeGoal
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

// IsKnownAgentMode reports whether mode is a supported agent workflow mode.
func IsKnownAgentMode(mode string) bool {
	switch NormalizeAgentMode(mode) {
	case AgentModeNormal, AgentModePlan, AgentModeGoal:
		return true
	default:
		return false
	}
}

// DetectWorkModeFromPrompt infers the most useful built-in workflow from a
// user's request. It is intentionally conservative and returns an empty string
// when the prompt does not clearly ask for a specialized mode.
func DetectWorkModeFromPrompt(prompt string) string {
	text := strings.ToLower(strings.TrimSpace(prompt))
	if text == "" {
		return ""
	}

	if containsAny(text,
		"代码审计", "源码审计", "白盒审计", "安全审计", "审计代码", "审一下代码",
		"code audit", "source audit", "secure code review", "security review", "sast", "whitebox", "white-box",
	) {
		return "code-audit"
	}
	if containsAny(text,
		"渗透测试", "打点", "红队", "漏洞扫描", "漏洞验证", "授权测试", "渗透",
		"pentest", "pen test", "penetration test", "red team", "red-team", "recon", "exploit", "vulnerability assessment",
	) {
		return "pentest"
	}
	if containsAny(text,
		"写代码", "实现", "开发", "修复", "改代码", "加功能", "重构", "编译", "测试失败",
		"write code", "implement", "build", "fix", "bug", "refactor", "develop", "add feature",
	) {
		return "coding"
	}
	return ""
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

// Save 保存配置到文件
func (cfg *Config) Save(path string) error {
	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// GetDefaultConfigPath 获取默认配置文件路径
func GetDefaultConfigPath() string {
	configDir := GetConfigDir()
	return filepath.Join(configDir, "config.json")
}

// GetConfigDir 获取配置目录路径
func GetSkillsDir(cfg *Config) string {
	if cfg != nil && strings.TrimSpace(cfg.ConfigPath) != "" {
		return filepath.Join(filepath.Dir(cfg.ConfigPath), "skills")
	}
	return filepath.Join(GetConfigDir(), "skills")
}

func GetConfigDir() string {
	var baseDir string

	// 优先使用 XDG_CONFIG_HOME
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		baseDir = xdg
	} else {
		// 使用默认位置
		switch runtime.GOOS {
		case "windows":
			if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
				baseDir = filepath.Join(appData, "T-Rex")
			} else {
				baseDir = filepath.Join(os.Getenv("USERPROFILE"), ".t-rex")
			}
		case "darwin":
			baseDir = filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "T-Rex")
		default: // linux, freebsd, etc.
			baseDir = filepath.Join(os.Getenv("HOME"), ".config", "t-rex")
		}
	}

	return baseDir
}

// GetSessionDir 获取会话存储目录
func GetSessionDir() string {
	return filepath.Join(GetConfigDir(), "sessions")
}

// IsToolAllowed 检查工具权限
func (p *PermissionConfig) IsToolAllowed(toolName string) (bool, string) {
	return p.IsToolAllowedInput(toolName, nil)
}

// IsToolAllowedInput supports Claude Code style Tool(parameter:pattern) rules,
// for example Bash(command:git status*) or Write(file_path:*.env).
func (p *PermissionConfig) IsToolAllowedInput(toolName string, input map[string]interface{}) (bool, string) {
	toolName = strings.TrimSpace(toolName)

	if p.Mode == "bypass" {
		if p.DisableBypassMode || p.ManagedRulesOnly {
			return false, "denied"
		}
		return true, "allowed (bypass mode)"
	}
	if isShellToolName(toolName) {
		profile := strings.ToLower(strings.TrimSpace(p.ShellRiskProfile))
		if profile == "allowlist" && !ShellCommandAllowed(shellCommandFromInput(input), p.ShellAllowlist) {
			return false, "denied"
		}
		if profile == "strict" && shellRiskIsHigh(input) && !ShellCommandAllowed(shellCommandFromInput(input), p.ShellAllowlist) {
			return false, "denied"
		}
	}

	// 检查 deny 列表（最高优先级）
	for _, pattern := range p.Deny {
		if matchPermissionRule(toolName, input, pattern) {
			return false, "denied"
		}
	}
	if p.Mode == "plan" && !isReadOnlyTool(toolName) {
		return false, "plan mode"
	}

	// 检查 allow 列表
	for _, pattern := range p.Allow {
		if matchPermissionRule(toolName, input, pattern) {
			return true, "allowed"
		}
	}

	// 检查 ask 列表
	for _, pattern := range p.Ask {
		if matchPermissionRule(toolName, input, pattern) {
			if isShellToolName(toolName) && shellRiskIsHigh(input) {
				return false, "ask-high-risk"
			}
			return false, "ask"
		}
	}

	// 默认行为取决于模式
	switch p.Mode {
	case "plan":
		return false, "plan mode"
	default: // "default"
		if isShellToolName(toolName) && shellRiskIsHigh(input) {
			return false, "ask-high-risk"
		}
		return false, "ask (default)"
	}
}

func isReadOnlyTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "read", "glob", "grep", "lsp", "skills_list", "skills_read", "skills_readresource", "securitycatalog", "codeauditcatalog":
		return true
	default:
		return false
	}
}

// IsReadOnlyTool reports whether toolName only observes state and never mutates
// the workspace or external systems.
func IsReadOnlyTool(toolName string) bool {
	return isReadOnlyTool(toolName)
}

// IsPlanModeAllowedTool reports whether a tool may run while the agent is in
// plan mode. Plan mode permits read-only investigation plus non-destructive
// planning state tools, and blocks anything that changes the workspace or
// external systems so the user can approve a plan before execution.
func IsPlanModeAllowedTool(toolName string) bool {
	if isReadOnlyTool(toolName) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "todo", "mission", "goal":
		return true
	default:
		return false
	}
}

func matchPermissionRule(toolName string, input map[string]interface{}, rule string) bool {
	rule = strings.TrimSpace(rule)
	open := strings.Index(rule, "(")
	if open < 0 || !strings.HasSuffix(rule, ")") {
		return matchPattern(toolName, rule)
	}
	if !strings.EqualFold(strings.TrimSpace(rule[:open]), toolName) {
		return false
	}
	selector := strings.TrimSpace(rule[open+1 : len(rule)-1])
	parts := strings.SplitN(selector, ":", 2)
	if len(parts) != 2 || input == nil {
		return false
	}
	value, ok := input[strings.TrimSpace(parts[0])]
	if !ok {
		return false
	}
	return wildcardValueMatch(fmt.Sprint(value), strings.TrimSpace(parts[1]))
}

func wildcardValueMatch(value, pattern string) bool {
	expression := regexp.QuoteMeta(pattern)
	expression = strings.ReplaceAll(expression, `\*`, `.*`)
	expression = strings.ReplaceAll(expression, `\?`, `.`)
	matched, err := regexp.MatchString(`(?i)^`+expression+`$`, value)
	return err == nil && matched
}

// matchPattern 简单的模式匹配（支持通配符）
func matchPattern(name, pattern string) bool {
	name = strings.ToLower(name)
	pattern = strings.ToLower(pattern)

	// 完全匹配
	if name == pattern {
		return true
	}

	// 通配符匹配
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(name, prefix)
	}

	return false
}
