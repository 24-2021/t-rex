package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/t-rex-ai/t-rex/internal/api"
	"github.com/t-rex-ai/t-rex/internal/config"
	"github.com/t-rex-ai/t-rex/internal/mcp"
	"github.com/t-rex-ai/t-rex/internal/permission"
	promptpkg "github.com/t-rex-ai/t-rex/internal/prompt"
	"github.com/t-rex-ai/t-rex/internal/session"
	"github.com/t-rex-ai/t-rex/internal/tools"
	"github.com/t-rex-ai/t-rex/internal/ui"
)

// runInteractiveMode 运行交互式 REPL 模式
func runInteractiveMode(cfg *config.Config) error {
	// 验证 API Key
	if cfg.AI.APIKey == "" {
		color.Red("❌ 错误: 未设置 API Key")
		color.Yellow("请设置 ANTHROPIC_API_KEY 环境变量或在配置文件中配置")
		color.White("\n快速开始:")
		color.White("  export ANTHROPIC_API_KEY='your-api-key'")
		color.White("  t-rex")
		return fmt.Errorf("API Key 未设置")
	}

	// 初始化组件
	apiClient := api.NewClient(&cfg.AI)
	permChecker := permission.NewChecker(&cfg.Permissions)
	toolRegistry := tools.NewRegistry(cfg, permChecker)
	mcpManager := mcp.NewManager(&cfg.MCP)
	if err := mcpManager.StartAll(context.Background()); err != nil {
		color.Yellow("MCP startup warning: %v", err)
	}
	defer mcpManager.CloseAll()
	if err := toolRegistry.RegisterMCPTools(context.Background(), mcpManager); err != nil {
		color.Yellow("MCP tool registration warning: %v", err)
	}
	sessionMgr, err := session.NewManager(&cfg.Session)
	if err != nil {
		return fmt.Errorf("初始化会话管理器失败: %w", err)
	}
	defer sessionMgr.Close()

	// 创建或恢复会话
	var currentSession *session.Session
	if resume {
		currentSession, err = resumeLastSession(sessionMgr)
	} else {
		currentSession, err = sessionMgr.Create(cfg.Project.Path, cfg.AI.Model)
	}

	if err != nil {
		return fmt.Errorf("创建/恢复会话失败: %w", err)
	}

	sessionMgr.SetCurrent(currentSession)

	// 创建并启动 REPL
	repl := ui.NewREPL(cfg, apiClient, sessionMgr, toolRegistry, permChecker)
	repl.SetMCPManager(mcpManager)

	// 设置信号处理
	setupSignalHandler(repl)

	return repl.Run()
}

// runSinglePrompt 运行单次提示模式（非交互式）
func runSinglePrompt(cfg *config.Config, message string) error {
	// 验证 API Key
	if cfg.AI.APIKey == "" {
		return fmt.Errorf("API Key 未设置")
	}

	// 创建临时会话
	sessionMgr, err := session.NewManager(&cfg.Session)
	if err != nil {
		return fmt.Errorf("初始化会话失败: %w", err)
	}
	defer sessionMgr.Close()

	sess, err := sessionMgr.Create(cfg.Project.Path, cfg.AI.Model)
	if err != nil {
		return fmt.Errorf("创建会话失败: %w", err)
	}
	sessionMgr.SetCurrent(sess)

	apiClient := api.NewClient(&cfg.AI)
	permChecker := permission.NewChecker(&cfg.Permissions)
	toolRegistry := tools.NewRegistry(cfg, permChecker)
	mcpManager := mcp.NewManager(&cfg.MCP)
	if err := mcpManager.StartAll(context.Background()); err != nil {
		color.Yellow("MCP startup warning: %v", err)
	}
	defer mcpManager.CloseAll()
	if err := toolRegistry.RegisterMCPTools(context.Background(), mcpManager); err != nil {
		color.Yellow("MCP tool registration warning: %v", err)
	}

	// Use the same multi-round agent loop as interactive mode. This makes
	// `t-rex prompt` useful for automation instead of limiting it to chat-only.
	repl := ui.NewREPL(cfg, apiClient, sessionMgr, toolRegistry, permChecker)
	repl.SetMCPManager(mcpManager)
	defer repl.Close()
	return repl.ProcessMessage(message)
}

// resumeLastSession 恢复上一次会话
func resumeLastSession(mgr *session.Manager) (*session.Session, error) {
	storageDir := config.GetSessionDir()

	summaries, err := session.ListAllSessions(storageDir)
	if err != nil || len(summaries) == 0 {
		// 没有历史会话，创建新的
		return mgr.Create("", "")
	}

	limit := len(summaries)
	if limit > 10 {
		limit = 10
	}
	color.Cyan("选择要恢复的会话，回车默认最近：")
	for i := 0; i < limit; i++ {
		title := summaries[i].Title
		if title == "" {
			title = "(untitled)"
		}
		color.White("  %d. %s  %s  %s", i+1, summaries[i].UpdatedAt.Format("2006-01-02 15:04"), truncateMessage(title, 40), summaries[i].ID[:8])
	}
	fmt.Print("resume> ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	index := 0
	if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n >= 1 && n <= limit {
		index = n - 1
	}
	chosen := summaries[index]
	color.Green("恢复会话: %s", chosen.ID)
	return mgr.GetByID(chosen.ID)
}

// listSessions 获取会话列表（供命令行使用）
func listSessions() ([]session.SessionSummary, error) {
	storageDir := config.GetSessionDir()
	return session.ListAllSessions(storageDir)
}

// setupSignalHandler 设置信号处理器
func setupSignalHandler(repl *ui.REPL) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println()
		color.Yellow("\n接收到中断信号...")
		// REPL 的 cleanup 会在 Run() 返回后自动调用
	}()
}

// buildSystemPrompt 构建系统提示词
func buildSystemPrompt(cfg *config.Config) string {
	return promptpkg.BuildSystemPrompt(cfg)
}

// 辅助格式化函数
func formatTime(t time.Time) string {
	return t.Format("01-02 15:04")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return d.String()
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func truncateMessage(msg string, maxLen int) string {
	runes := []rune(msg)
	if len(runes) <= maxLen {
		return msg
	}
	return string(runes[:maxLen]) + "..."
}

// runtimeOS 返回运行时操作系统
func getRuntimeOS() string {
	return runtime.GOOS
}

// runtimeArch 返回运行时架构
func getRuntimeArch() string {
	return runtime.GOARCH
}

// runtimeVersion 返回 Go 版本
func getRuntimeVersion() string {
	return "1.23+"
}

// UUID 生成辅助
func generateUUID() string {
	return uuid.New().String()
}
