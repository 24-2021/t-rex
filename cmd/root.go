package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/t-rex-ai/t-rex/internal/config"
	"github.com/t-rex-ai/t-rex/internal/ui"
)

var (
	cfgFile                    string
	model                      string
	maxTokens                  int
	maxToolRounds              int
	workMode                   string
	projectPath                string
	resume                     bool
	permissionMode             string
	dangerouslySkipPermissions bool
	verbose                    int
	versionInfo                string
	buildTime                  string
)

var rootCmd = &cobra.Command{
	Use:   "t-rex",
	Short: "T-REX 🦖 - Terminal Reimagined Experience",
	Long: `T-REX (Terminal Reimagined Experience) 是一个用 Go 语言编写的 AI 编程助手。

T-REX 专注终端原生、高性能、流式反馈的 AI 辅助编程体验。

特性:
  • 跨平台原生二进制文件（无需 Node.js）
  • Anthropic Claude API 集成
  • 流式响应输出
  • 内置工具系统（Shell、文件操作、搜索等）
  • MCP (Model Context Protocol) 支持
  • 会话持久化和恢复
  • 细粒度权限控制

开始使用:
  t-rex                    # 启动交互式会话
  t-rex prompt "你的问题"   # 直接发送提示`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 无子命令时默认进入交互模式（或处理直接传入的参数）
		if len(args) > 0 {
			return runSinglePromptMode(strings.Join(args, " "))
		}
		return runPromptMode("")
	},
}

var promptCmd = &cobra.Command{
	Use:   "prompt [message]",
	Short: "发送提示给 AI 助手",
	Long:  `发送一条消息给 AI 助手并获取响应。可以用于非交互式场景或脚本集成。`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		message := ""
		if len(args) > 0 {
			message = args[0]
		}
		return runPromptMode(message)
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "管理配置",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化配置文件",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigInit()
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "显示当前配置",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigShow()
	},
}

var configDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "检查配置、工作区、权限和 MCP 状态",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigDoctor()
	},
}

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "管理会话",
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出历史会话",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSessionList()
	},
}

// Execute 执行根命令
func Execute(version, buildTime string) error {
	versionInfo = version
	ui.SetBuildInfo(version, buildTime)
	initCommands()
	return rootCmd.Execute()
}

func initCommands() {
	// 根命令标志
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "配置文件路径 (默认为 ~/.t-rex/config.json)")
	rootCmd.PersistentFlags().StringVarP(&model, "model", "m", "", "使用的模型 (默认: claude-sonnet-4-20250514)")
	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", 0, "最大 token 数（0 表示使用配置值）")
	rootCmd.PersistentFlags().IntVar(&maxToolRounds, "max-tool-rounds", 0, "单次 agent turn 的最大工具轮数")
	rootCmd.PersistentFlags().StringVar(&workMode, "work-mode", "", "agent 工作模式: coding, code-audit, pentest, ai-security, bugfix, code-review, incident-response, malware-analysis, osint, ctf")
	rootCmd.PersistentFlags().StringVarP(&projectPath, "path", "p", "", "项目工作目录")
	rootCmd.PersistentFlags().BoolVarP(&resume, "resume", "r", false, "恢复上一次会话")
	rootCmd.PersistentFlags().StringVar(&permissionMode, "permission-mode", "", "权限模式: default, plan, bypass")
	rootCmd.PersistentFlags().BoolVar(&dangerouslySkipPermissions, "dangerously-skip-permissions", false, "跳过所有工具权限确认（高风险）")
	rootCmd.PersistentFlags().CountVarP(&verbose, "verbose", "v", "增加输出详细程度")
	_ = rootCmd.RegisterFlagCompletionFunc("work-mode", completeWorkMode)
	_ = rootCmd.RegisterFlagCompletionFunc("permission-mode", completePermissionMode)

	// 版本标志
	rootCmd.Version = fmt.Sprintf("T-REX %s (built: %s)\nGo Version: %s\nPlatform: %s/%s\n",
		versionInfo, buildTime, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	rootCmd.SetVersionTemplate(`{{.Version}}`)

	// 添加子命令
	rootCmd.AddCommand(promptCmd)
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configDoctorCmd)
	rootCmd.AddCommand(sessionCmd)
	sessionCmd.AddCommand(sessionListCmd)
}

func completeWorkMode(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	modes := []string{
		"coding\t默认代码协作",
		"code-audit\t源码安全审计",
		"pentest\t授权渗透测试",
		"ai-security\tLLM/Agent 安全评估",
		"bugfix\t缺陷修复",
		"code-review\t代码审查",
		"incident-response\t事件响应",
		"malware-analysis\t恶意样本分析",
		"osint\t开源情报",
		"ctf\tCTF 解题",
	}
	return filterCompletionItems(modes, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completePermissionMode(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	modes := []string{
		"default\t敏感操作需要确认",
		"plan\t只读规划模式",
		"bypass\t自动批准工具调用",
	}
	return filterCompletionItems(modes, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func filterCompletionItems(items []string, prefix string) []string {
	if strings.TrimSpace(prefix) == "" {
		return items
	}
	prefix = strings.ToLower(prefix)
	var out []string
	for _, item := range items {
		value := item
		if before, _, ok := strings.Cut(item, "\t"); ok {
			value = before
		}
		if strings.HasPrefix(strings.ToLower(value), prefix) {
			out = append(out, item)
		}
	}
	return out
}

func runPromptMode(message string) error {
	// 加载配置
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if err := applyCLIOverrides(cfg); err != nil {
		return err
	}

	// 如果没有消息，进入交互模式
	if message == "" {
		return runInteractiveMode(cfg)
	}

	// 单次提示模式
	return runSinglePrompt(cfg, message)
}

// runSinglePromptMode 处理无子命令时直接传入的消息
func runSinglePromptMode(message string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if err := applyCLIOverrides(cfg); err != nil {
		return err
	}

	return runSinglePrompt(cfg, message)
}

func applyCLIOverrides(cfg *config.Config) error {
	if model != "" {
		cfg.AI.Model = model
	}
	if maxTokens > 0 {
		cfg.AI.MaxTokens = maxTokens
	}
	if maxToolRounds > 0 {
		cfg.AI.MaxToolRounds = maxToolRounds
	}
	if workMode != "" {
		normalized := config.NormalizeWorkMode(workMode)
		if !config.IsKnownWorkMode(normalized) {
			return fmt.Errorf("invalid work mode %q (available: coding, code-audit, pentest, ai-security, bugfix, code-review, incident-response, malware-analysis, osint, ctf)", workMode)
		}
		cfg.AI.WorkMode = normalized
	}
	if projectPath != "" {
		cfg.Project.Path = projectPath
	}
	if permissionMode != "" {
		switch permissionMode {
		case "default", "plan", "bypass":
			cfg.Permissions.Mode = permissionMode
		default:
			return fmt.Errorf("无效的权限模式 %q（可用: default, plan, bypass）", permissionMode)
		}
	}
	if dangerouslySkipPermissions {
		if cfg.Permissions.DisableBypassMode {
			return fmt.Errorf("bypass 模式已被配置禁用")
		}
		cfg.Permissions.Mode = "bypass"
	}
	return nil
}

func runConfigInit() error {
	color.Cyan("🦖 初始化 T-REX 配置...\n")

	configPath := config.GetDefaultConfigPath()

	// 检查是否已存在
	if _, err := os.Stat(configPath); err == nil {
		color.Yellow("⚠️  配置文件已存在: %s", configPath)
		fmt.Print("是否覆盖? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" {
			color.Green("✓ 配置未修改")
			return nil
		}
	}

	// 创建默认配置
	defaultCfg := config.Default()
	if err := defaultCfg.Save(configPath); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	color.Green("✓ 配置文件已创建: %s", configPath)
	color.Yellow("\n下一步:")
	color.White("  1. 编辑配置文件设置你的 API Key")
	color.White("  2. 或设置环境变量: export ANTHROPIC_API_KEY='your-key'")
	color.White("\n然后运行: t-rex")

	return nil
}

func runConfigShow() error {
	color.Cyan("🦖 当前配置\n")

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 显示脱敏的配置信息
	fmt.Printf(color.WhiteString("AI 模型: %s\n"), cfg.AI.Model)
	fmt.Printf(color.WhiteString("Work mode: %s\n"), config.NormalizeWorkMode(cfg.AI.WorkMode))
	fmt.Printf(color.WhiteString("最大 Tokens: %d\n"), cfg.AI.MaxTokens)
	fmt.Printf(color.WhiteString("温度: %.1f\n"), cfg.AI.Temperature)

	if cfg.AI.APIKey != "" {
		fmt.Printf(color.WhiteString("API Key: %s\n"), maskSecret(cfg.AI.APIKey))
	} else {
		fmt.Println(color.RedString("API Key: 未设置"))
	}

	fmt.Printf(color.WhiteString("项目路径: %s\n"), cfg.Project.Path)
	fmt.Printf(color.WhiteString("权限模式: %s\n"), cfg.Permissions.Mode)

	if len(cfg.Permissions.Allow) > 0 {
		fmt.Printf(color.WhiteString("允许的工具: %v\n"), cfg.Permissions.Allow)
	}
	if len(cfg.Permissions.Ask) > 0 {
		fmt.Printf(color.WhiteString("需授权的工具: %v\n"), cfg.Permissions.Ask)
	}
	if len(cfg.MCP.Servers) > 0 {
		fmt.Printf(color.WhiteString("MCP 服务器: %d 个\n"), len(cfg.MCP.Servers))
		for _, s := range cfg.MCP.Servers {
			fmt.Print(grayString("  • %s (%s)\n", s.Name, s.Command))
		}
	}

	fmt.Println()
	fmt.Println(grayString("配置文件路径: %s", config.GetDefaultConfigPath()))

	return nil
}

func maskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	runes := []rune(secret)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	if len(runes) <= 12 {
		return strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-4:])
	}
	return string(runes[:8]) + "..." + string(runes[len(runes)-4:])
}

func runConfigDoctor() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	if err := applyCLIOverrides(cfg); err != nil {
		return err
	}
	color.Cyan("T-REX config doctor\n")
	ok := true
	check := func(name string, pass bool, detail string) {
		if pass {
			color.Green("✓ %s: %s", name, detail)
		} else {
			ok = false
			color.Red("✗ %s: %s", name, detail)
		}
	}
	check("API key", cfg.AI.APIKey != "", "configured")
	check("provider", cfg.AI.Provider != "", cfg.AI.Provider)
	check("model", cfg.AI.Model != "", cfg.AI.Model)
	if cfg.Project.Path == "" {
		cwd, _ := os.Getwd()
		check("workspace", true, "current directory: "+cwd)
	} else if st, err := os.Stat(cfg.Project.Path); err == nil && st.IsDir() {
		check("workspace", true, cfg.Project.Path)
	} else {
		check("workspace", false, cfg.Project.Path+" is not a directory")
	}
	check("permission mode", cfg.Permissions.Mode != "", cfg.Permissions.Mode)
	check("work mode", config.IsKnownWorkMode(cfg.AI.WorkMode), config.NormalizeWorkMode(cfg.AI.WorkMode))
	check("tool rounds", cfg.AI.MaxToolRounds > 0, fmt.Sprintf("%d", cfg.AI.MaxToolRounds))
	if len(cfg.MCP.Servers) == 0 {
		color.Yellow("! MCP: no servers configured")
	} else {
		color.Green("✓ MCP: %d server(s) configured", len(cfg.MCP.Servers))
	}
	if !ok {
		return fmt.Errorf("doctor found problems")
	}
	return nil
}

func runSessionList() error {
	color.Cyan("🦖 历史会话\n")

	sessions, err := listSessions()
	if err != nil {
		return fmt.Errorf("获取会话列表失败: %w", err)
	}

	if len(sessions) == 0 {
		color.Yellow("没有找到历史会话")
		color.White("提示: 使用 't-rex' 开始新会话")
		return nil
	}

	fmt.Println(color.WhiteString("%-36s %-20s %4s %s", "ID", "时间", "消息数", "标题"))
	fmt.Println(strings.Repeat("-", 80))

	for i, sess := range sessions {
		id := sess.ID
		if len(id) > 34 {
			id = id[:34] + ".."
		}
		timeStr := formatSessionTime(sess.UpdatedAt)
		msgCount := fmt.Sprintf("%d", sess.MessageCount)
		title := sess.Title
		if title == "" {
			title = "(无标题)"
		}
		if len(title) > 30 {
			title = title[:27] + "..."
		}

		fmt.Printf("%-36s %-20s %4s %s\n", id, timeStr, msgCount, title)

		if i >= 19 { // 最多显示20条
			break
		}
	}

	fmt.Printf("\n共 %d 个历史会话\n", len(sessions))

	return nil
}

// formatSessionTime 格式化会话时间
func formatSessionTime(t time.Time) string {
	if t.IsZero() {
		return "未知"
	}
	return t.Local().Format("2006-01-02 15:04")
}
