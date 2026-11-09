package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/t-rex-ai/t-rex/internal/api"
	"github.com/t-rex-ai/t-rex/internal/config"
	"github.com/t-rex-ai/t-rex/internal/hooks"
	"github.com/t-rex-ai/t-rex/internal/mcp"
	"github.com/t-rex-ai/t-rex/internal/memory"
	"github.com/t-rex-ai/t-rex/internal/permission"
	promptpkg "github.com/t-rex-ai/t-rex/internal/prompt"
	"github.com/t-rex-ai/t-rex/internal/session"
	"github.com/t-rex-ai/t-rex/internal/skills"
	"github.com/t-rex-ai/t-rex/internal/termui"
	"github.com/t-rex-ai/t-rex/internal/tools"
)

// REPL 交互式终端界面
type REPL struct {
	config       *config.Config
	apiClient    *api.Client
	sessionMgr   *session.Manager
	toolRegistry *tools.Registry
	mcpManager   *mcp.Manager
	permChecker  *permission.Checker
	hookRunner   *hooks.Runner
	hookContext  []string
	hooksStarted bool
	closed       bool
	interactive  bool
	pendingPrompt string
	reader       *bufio.Reader
	inputCh      chan string
	inputErrCh   chan error
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewREPL 创建新的 REPL 实例
func NewREPL(
	cfg *config.Config,
	client *api.Client,
	sessMgr *session.Manager,
	registry *tools.Registry,
	permCheck *permission.Checker,
) *REPL {
	ctx, cancel := context.WithCancel(context.Background())
	repl := &REPL{
		config:       cfg,
		apiClient:    client,
		sessionMgr:   sessMgr,
		toolRegistry: registry,
		permChecker:  permCheck,
		reader:       bufio.NewReader(os.Stdin),
		ctx:          ctx,
		cancel:       cancel,
	}
	if sess := sessMgr.Get(); sess != nil {
		repl.hookRunner = hooks.NewRunner(&cfg.Hooks, sess.ID, cfg.Project.Path)
	}
	if permCheck != nil {
		// Route permission prompts through the same input path as the REPL so a
		// single reader owns stdin (needed for reliable mid-turn interjection).
		permCheck.SetInputReader(repl.readLineInput)
	}
	repl.restoreAgentStateFromSession()
	return repl
}

func (r *REPL) SetMCPManager(manager *mcp.Manager) {
	r.mcpManager = manager
}

// readLineInput reads one line from the shared input source. It is used by the
// REPL, multiline paste, and interactive select prompts so they all consume
// from the same reader and never steal bytes from each other.
func (r *REPL) readLineInput() (string, error) {
	return r.readQueuedInput()
}

// Run 启动交互式循环
func (r *REPL) Run() error {
	r.interactive = true
	r.startInputReader()
	r.ensureHooksStarted()
	enableTerminalStyling()
	r.showZodiacAnimation(time.Now())
	// 显示欢迎信息
	r.showWelcome()

	// 设置信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println()
		color.Yellow("\n接收到中断信号，正在优雅退出...")
		r.cancel()
	}()

	// 主循环
	for r.ctx.Err() == nil {
		// 显示提示符并读取输入
		input, err := r.readInput()
		if err != nil {
			if err == ErrInterrupted || err == ErrEOF {
				break
			}
			color.Red("读取输入错误: %v", err)
			continue
		}

		// 处理空输入
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if strings.HasPrefix(input, "/paste") {
			collected, err := r.readMultilineCommand(input)
			if err != nil {
				color.Red("读取多行输入失败: %v", err)
				continue
			}
			input = strings.TrimSpace(collected)
			if input == "" {
				color.Yellow("多行输入为空")
				continue
			}
		}
		if strings.HasPrefix(input, "/writepaste") {
			if err := r.writeMultilineFile(input); err != nil {
				color.Red("写入多行文件失败: %v", err)
			}
			continue
		}

		// 处理特殊命令
		if strings.HasPrefix(input, "/") {
			handled := r.handleCommand(input)
			if pending := r.takePendingPrompt(); pending != "" {
				if err := r.ProcessMessage(pending); err != nil {
					color.Red("处理消息失败: %v", err)
					if r.ctx.Err() != nil {
						break
					}
				}
				continue
			}
			if !handled {
				continue
			}
		}

		// 发送消息给 AI
		if err := r.ProcessMessage(input); err != nil {
			color.Red("处理消息失败: %v", err)
			// 如果是上下文取消错误，退出循环
			if r.ctx.Err() != nil {
				break
			}
		}
	}

	// 清理
	r.cleanup(true)
	return nil
}

// ProcessMessage runs one complete agent turn, including tools and hooks. It is
// exported so non-interactive mode uses the same capable loop as the REPL.
func (r *REPL) ProcessMessage(userMsg string) error {
	r.ensureHooksStarted()
	return r.processUserMessage(userMsg)
}

// readInput 读取用户输入
func (r *REPL) readInput() (string, error) {
	if r.config != nil && r.config.UI.ShowTokenUsage {
		fmt.Println(r.contextStatusString(r.statusLine()))
	}
	prompt := color.CyanString("> ")
	fmt.Print(prompt)
	if r.inputCh != nil {
		return r.readQueuedInput()
	}

	input, err := r.reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSuffix(input, "\n"), nil
}

func (r *REPL) startInputReader() {
	if r.inputCh != nil {
		return
	}
	r.inputCh = make(chan string)
	r.inputErrCh = make(chan error, 1)
	go func() {
		for {
			input, err := r.reader.ReadString('\n')
			if err != nil {
				r.inputErrCh <- err
				return
			}
			select {
			case r.inputCh <- strings.TrimSuffix(input, "\n"):
			case <-r.ctx.Done():
				return
			}
		}
	}()
}

func (r *REPL) readQueuedInput() (string, error) {
	if r.inputCh == nil {
		input, err := r.reader.ReadString('\n')
		if err == io.EOF {
			return strings.TrimSuffix(input, "\n"), ErrEOF
		}
		return strings.TrimSuffix(input, "\n"), err
	}
	select {
	case input := <-r.inputCh:
		return input, nil
	case err := <-r.inputErrCh:
		if err == io.EOF {
			return "", ErrEOF
		}
		return "", err
	case <-r.ctx.Done():
		return "", ErrInterrupted
	}
}

func (r *REPL) readMultilineCommand(firstLine string) (string, error) {
	prefix := strings.TrimSpace(strings.TrimPrefix(firstLine, "/paste"))
	color.Cyan("多行粘贴模式：粘贴内容后，单独输入 /end 结束并提交。")
	var b strings.Builder
	if prefix != "" {
		b.WriteString(prefix)
		b.WriteString("\n\n")
	}
	for {
		fmt.Print(color.CyanString("... "))
		line, err := r.readLineInput()
		if err != nil {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "/end" {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (r *REPL) writeMultilineFile(firstLine string) error {
	parts := strings.Fields(firstLine)
	if len(parts) < 2 {
		return fmt.Errorf("usage: /writepaste <file_path>")
	}
	target, err := r.resolveUserWritePath(parts[1])
	if err != nil {
		return err
	}
	color.Cyan("多行写入模式：粘贴文件内容后，单独输入 /end 结束并写入。")
	var b strings.Builder
	for {
		fmt.Print(color.CyanString("... "))
		line, err := r.readLineInput()
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "/end" {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(target, []byte(b.String()), 0644); err != nil {
		return err
	}
	color.Green("wrote %s (%d bytes)", target, b.Len())
	return nil
}

func (r *REPL) resolveUserWritePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("empty file path")
	}
	root := "."
	if r != nil && r.config != nil && strings.TrimSpace(r.config.Project.Path) != "" {
		root = r.config.Project.Path
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(rootAbs, target)
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes workspace: %s", path)
	}
	return targetAbs, nil
}

func (r *REPL) watchTurnInterrupts(ctx context.Context, cancel context.CancelFunc, enabled *atomic.Bool) <-chan string {
	ch := make(chan string, 1)
	if r.inputCh == nil {
		close(ch)
		return ch
	}
	go func() {
		defer close(ch)
		for {
			if !enabled.Load() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(50 * time.Millisecond):
					continue
				}
			}
			select {
			case input := <-r.inputCh:
				input = strings.TrimSpace(input)
				if input == "" {
					continue
				}
				ch <- input
				cancel()
				return
			case <-r.inputErrCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func receiveInterrupt(ch <-chan string) string {
	select {
	case input, ok := <-ch:
		if ok {
			return input
		}
	default:
	}
	return ""
}

// processUserMessage 处理用户消息（核心逻辑）
func (r *REPL) processUserMessage(userMsg string) error {
	startTime := time.Now()
	r.autoSelectWorkMode(userMsg)
	r.seedWorkModeState()
	if r.agentMode() == config.AgentModeGoal && strings.TrimSpace(tools.CurrentGoal().Objective) == "" {
		tools.SetGoal(userMsg, nil)
	}
	promptHook := r.runHook(hooks.UserPromptSubmit, hooks.Payload{"prompt": userMsg})
	if promptHook.Blocked {
		color.Red("请求被 UserPromptSubmit hook 阻止: %s", promptHook.Reason)
		return nil
	}

	if err := r.sessionMgr.AddMessage("user", userMsg, nil, nil); err != nil {
		return fmt.Errorf("保存消息失败: %w", err)
	}

	messages := r.sessionMgr.GetMessagesForAPI(r.getMaxMessages())
	fmt.Println()

	var lastUsage *api.Usage
	var lastErr error
	completed := false
	maxRounds := r.effectiveMaxToolRounds()
	toolRoundsUsed := 0
	apiTurnsUsed := 0
	streamRecoveries := 0
	textToolParseRecoveries := 0
	planRefinements := 0
	goalContinuations := 0
	forceFinalResponse := false

	for {
		toolsAvailable := !forceFinalResponse
		systemPrompt := r.buildSystemPrompt()
		if r.autoCompactIfNeeded(systemPrompt) {
			messages = r.sessionMgr.GetMessagesForAPI(r.getMaxMessages())
		}
		var apiTools interface{}
		var toolChoice interface{}
		if toolsAvailable {
			apiTools = r.toolRegistry.ToAPIDefinitions()
			toolChoice = map[string]interface{}{"type": "auto"}
		} else {
			systemPrompt += "\n\nYou have reached the configured tool-use limit for this turn. Do not call tools. Provide the best final answer from the information already gathered, including any uncertainty or recommended next step."
		}
		req := &api.APIRequest{
			Model:       r.config.AI.Model,
			MaxTokens:   r.config.AI.MaxTokens,
			Temperature: r.config.AI.Temperature,
			TopP:        r.config.AI.TopP,
			Messages:    r.messagesForRequest(messages, systemPrompt),
			System:      systemPrompt,
			Tools:       apiTools,
			ToolChoice:  toolChoice,
		}

		var roundText strings.Builder
		var assistantToolBlocks []api.ContentBlock
		var toolResultBlocks []api.ContentBlock
		var roundUsage *api.Usage
		interruptEnabled := &atomic.Bool{}
		interruptEnabled.Store(true)
		apiCtx, cancelAPI := context.WithCancel(r.ctx)
		interruptCtx, cancelInterrupt := context.WithCancel(apiCtx)
		interruptCh := r.watchTurnInterrupts(interruptCtx, cancelAPI, interruptEnabled)
		thinking := r.startThinkingAnimation()
		stopThinking := func() {
			if thinking != nil {
				thinking.Stop()
				thinking = nil
			}
		}

		maxAttempts := 1 + r.maxAPIRetries()
		for attempt := 0; attempt < maxAttempts; attempt++ {
			lastErr = r.apiClient.SendMessageStream(
				apiCtx,
				req,
				func(text string) {
					stopThinking()
					fmt.Print(text)
					roundText.WriteString(text)
				},
				func(tool *api.ContentBlock) {
					stopThinking()
				},
				func(content *api.ContentBlock) {
					interruptEnabled.Store(false)
					cancelInterrupt()
					result, toolErr := r.executeTool(apiCtx, content)
					interruptEnabled.Store(true)
					if toolErr != nil {
						color.Red("工具执行失败: %v\n", toolErr)
						result = &tools.ToolResult{
							Content: fmt.Sprintf("执行错误: %v", toolErr),
							IsError: true,
						}
					}

					input, _ := content.Input.(map[string]interface{})
					if input == nil {
						input = map[string]interface{}{}
					}
					assistantToolBlocks = append(assistantToolBlocks, api.ContentBlock{
						Type:  "tool_use",
						ID:    content.ID,
						Name:  content.Name,
						Input: input,
					})
					toolResultBlocks = append(toolResultBlocks, api.ContentBlock{
						Type:      "tool_result",
						ToolUseID: content.ID,
						Content:   result.Content,
						IsError:   result.IsError,
					})
				},
				func(resp *api.APIResponse) {
					stopThinking()
					usage := resp.Usage
					if usage.InputTokens > 0 || usage.OutputTokens > 0 || roundUsage == nil {
						roundUsage = &usage
					}
					if usage.InputTokens > 0 || usage.OutputTokens > 0 {
						lastUsage = &usage
					}
				},
				func(err error) {
					stopThinking()
					color.Red("\n❌ API 错误: %v\n", err)
				},
			)
			if lastErr == nil || r.ctx.Err() != nil || roundText.Len() > 0 || len(assistantToolBlocks) > 0 || len(toolResultBlocks) > 0 {
				break
			}
			if isContextLimitError(lastErr) && r.compactForRetry() {
				messages = r.sessionMgr.GetMessagesForAPI(r.getMaxMessages())
				req.Messages = r.messagesForRequest(messages, systemPrompt)
				color.Yellow("context was compacted; retrying request")
				continue
			}
			if attempt >= maxAttempts-1 || !isRetryableAPIError(lastErr) {
				break
			}
			delay := apiRetryDelay(attempt)
			color.Yellow("API request failed; retrying in %v (%d/%d)", delay, attempt+1, maxAttempts-1)
			select {
			case <-time.After(delay):
			case <-r.ctx.Done():
				attempt = maxAttempts
			}
		}
		cancelInterrupt()
		cancelAPI()
		stopThinking()
		apiTurnsUsed++

		if interrupted := receiveInterrupt(interruptCh); interrupted != "" {
			color.Yellow("\ninterrupted: %s", truncateSingleLine(interrupted, 160))
			if roundText.Len() > 0 {
				assistantBlock := api.ContentBlock{Type: "text", Text: roundText.String()}
				_ = r.sessionMgr.AddStructuredMessage("assistant", []api.ContentBlock{assistantBlock}, false, roundUsage)
				messages = append(messages, api.Message{Role: "assistant", Content: []api.ContentBlock{assistantBlock}})
			}
			feedbackBlock := api.ContentBlock{Type: "text", Text: "User interrupted the current turn with a new instruction:\n" + interrupted}
			_ = r.sessionMgr.AddStructuredMessage("user", []api.ContentBlock{feedbackBlock}, true, nil)
			messages = append(messages, api.Message{Role: "user", Content: []api.ContentBlock{feedbackBlock}})
			lastErr = nil
			fmt.Println()
			continue
		}

		if lastErr != nil {
			if roundText.Len() > 0 {
				assistantBlock := api.ContentBlock{Type: "text", Text: roundText.String()}
				_ = r.sessionMgr.AddStructuredMessage("assistant", []api.ContentBlock{assistantBlock}, false, roundUsage)
				messages = append(messages, api.Message{Role: "assistant", Content: []api.ContentBlock{assistantBlock}})
				if streamRecoveries < 1 && isRetryableAPIError(lastErr) {
					recoveryBlock := api.ContentBlock{Type: "text", Text: "The previous assistant response was interrupted before the task finished. Continue from the partial response above. Do not repeat completed work; continue with the next needed step."}
					_ = r.sessionMgr.AddStructuredMessage("user", []api.ContentBlock{recoveryBlock}, true, nil)
					messages = append(messages, api.Message{Role: "user", Content: []api.ContentBlock{recoveryBlock}})
					lastErr = nil
					streamRecoveries++
					fmt.Println()
					continue
				}
			}
			break
		}
		if len(toolResultBlocks) == 0 && toolsAvailable && r.shouldParseTextToolCalls(roundText.String()) {
			textToolBlocks := parseTextToolCalls(roundText.String())
			if len(textToolBlocks) > 0 {
				cleanText := stripTextToolCalls(roundText.String())
				roundText.Reset()
				roundText.WriteString(cleanText)
				for i := range textToolBlocks {
					block := textToolBlocks[i]
					result, toolErr := r.executeTool(r.ctx, &block)
					if toolErr != nil {
						color.Red("工具执行失败: %v\n", toolErr)
						result = &tools.ToolResult{
							Content: fmt.Sprintf("执行错误: %v", toolErr),
							IsError: true,
						}
					}
					input, _ := block.Input.(map[string]interface{})
					if input == nil {
						input = map[string]interface{}{}
					}
					assistantToolBlocks = append(assistantToolBlocks, api.ContentBlock{
						Type:  "tool_use",
						ID:    block.ID,
						Name:  block.Name,
						Input: input,
					})
					toolResultBlocks = append(toolResultBlocks, api.ContentBlock{
						Type:      "tool_result",
						ToolUseID: block.ID,
						Content:   result.Content,
						IsError:   result.IsError,
					})
				}
			} else if textToolParseRecoveries < 2 && apiTurnsUsed < maxRounds {
				assistantBlock := api.ContentBlock{Type: "text", Text: roundText.String()}
				feedbackBlock := api.ContentBlock{Type: "text", Text: "Your previous response contained malformed text/DSML tool-call markup that T-Rex could not execute, so the audit is not complete. Continue from the current audit position, do not repeat completed checks, and use the native tool-calling interface for Glob/Grep/Read. If you must emit text tool calls, emit complete well-formed <tool_calls><invoke ...><parameter ...>...</parameter></invoke></tool_calls> markup only."}
				_ = r.sessionMgr.AddStructuredMessage("assistant", []api.ContentBlock{assistantBlock}, true, roundUsage)
				_ = r.sessionMgr.AddStructuredMessage("user", []api.ContentBlock{feedbackBlock}, true, nil)
				messages = append(messages, api.Message{Role: "assistant", Content: []api.ContentBlock{assistantBlock}})
				messages = append(messages, api.Message{Role: "user", Content: []api.ContentBlock{feedbackBlock}})
				textToolParseRecoveries++
				fmt.Println()
				continue
			}
		}
		if len(toolResultBlocks) == 0 {
			if roundText.Len() > 0 {
				_ = r.sessionMgr.AddStructuredMessage("assistant", []api.ContentBlock{{Type: "text", Text: roundText.String()}}, false, roundUsage)
			}
			stopResult := r.runHook(hooks.Stop, hooks.Payload{"response": roundText.String()})
			if stopResult.Blocked && toolsAvailable && apiTurnsUsed < maxRounds {
				feedback := firstNonEmpty(stopResult.Reason, strings.Join(stopResult.AdditionalContext, "\n"), "A Stop hook requested that you continue.")
				assistantBlock := api.ContentBlock{Type: "text", Text: roundText.String()}
				feedbackBlock := api.ContentBlock{Type: "text", Text: feedback}
				messages = append(messages, api.Message{Role: "assistant", Content: []api.ContentBlock{assistantBlock}})
				messages = append(messages, api.Message{Role: "user", Content: []api.ContentBlock{feedbackBlock}})
				_ = r.sessionMgr.AddStructuredMessage("user", []api.ContentBlock{feedbackBlock}, true, nil)
				fmt.Println()
				continue
			}
			// Plan mode: the agent stops to present a plan. Gate execution behind
			// an explicit user decision, and only continue executing after approval.
			if r.agentMode() == config.AgentModePlan && toolsAvailable && apiTurnsUsed < maxRounds {
				switch r.promptPlanDecision() {
				case planDecisionApprove:
					r.setAgentMode(config.AgentModeNormal)
					feedback := "The user approved the plan. Exit planning now and execute it. Start with step 1, use tools, and continue until the plan is complete."
					messages = r.appendTurnContinuation(messages, roundText.String(), feedback)
					fmt.Println()
					continue
				case planDecisionRefine:
					if planRefinements < 2 {
						planRefinements++
						feedback := "The user wants a more complete plan before execution. Address missing steps, files, risks, or validation, then present the updated plan."
						messages = r.appendTurnContinuation(messages, roundText.String(), feedback)
						fmt.Println()
						continue
					}
				default:
					r.setAgentMode(config.AgentModeNormal)
				}
				completed = true
				break
			}
			// Goal mode: keep working autonomously until the acceptance criteria
			// are met or the agent is genuinely blocked.
			if r.agentMode() == config.AgentModeGoal && toolsAvailable && apiTurnsUsed < maxRounds {
				if feedback, ok := r.goalContinuationFeedback(goalContinuations); ok {
					goalContinuations++
					messages = r.appendTurnContinuation(messages, roundText.String(), feedback)
					fmt.Println()
					continue
				}
				r.finishGoalTurn()
			}
			completed = true
			break
		}
		if forceFinalResponse {
			lastErr = fmt.Errorf("model attempted to call tools after the configured maxToolRounds limit (%d)", maxRounds)
			break
		}

		var assistantBlocks []api.ContentBlock
		if roundText.Len() > 0 {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{Type: "text", Text: roundText.String()})
		}
		assistantBlocks = append(assistantBlocks, assistantToolBlocks...)
		_ = r.sessionMgr.AddStructuredMessage("assistant", assistantBlocks, true, roundUsage)
		_ = r.sessionMgr.AddStructuredMessage("user", toolResultBlocks, true, nil)
		messages = append(messages, api.Message{Role: "assistant", Content: assistantBlocks})
		messages = append(messages, api.Message{Role: "user", Content: toolResultBlocks})
		fmt.Println()
		toolRoundsUsed++
		if toolRoundsUsed >= maxRounds {
			forceFinalResponse = true
		}
	}

	elapsed := time.Since(startTime)

	if lastErr == nil && !completed {
		lastErr = fmt.Errorf("agent reached the configured maxToolRounds limit (%d)", maxRounds)
	}

	if r.config.UI.ShowTiming || r.config.UI.ShowTokenUsage {
		fmt.Print("\n---\n")
		if r.config.UI.ShowTokenUsage {
			fmt.Println(r.tokenContextLine(lastUsage))
		}
		if r.config.UI.ShowTiming {
			fmt.Printf("time: %v\n", elapsed)
		}
	}

	fmt.Println()

	return lastErr
}

// agentMode returns the normalized agent workflow mode (normal, plan, or goal).
func (r *REPL) agentMode() string {
	if r == nil || r.config == nil {
		return config.AgentModeNormal
	}
	return config.NormalizeAgentMode(r.config.AI.AgentMode)
}

// setAgentMode switches the agent workflow mode and reports the change.
func (r *REPL) setAgentMode(mode string) {
	if r == nil || r.config == nil {
		return
	}
	mode = config.NormalizeAgentMode(mode)
	if !config.IsKnownAgentMode(mode) {
		mode = config.AgentModeNormal
	}
	if r.config.AI.AgentMode == mode {
		return
	}
	r.config.AI.AgentMode = mode
}

// takePendingPrompt returns and clears a prompt queued by a slash command.
func (r *REPL) takePendingPrompt() string {
	pending := r.pendingPrompt
	r.pendingPrompt = ""
	return pending
}

// appendTurnContinuation appends the assistant's turn output and a synthetic
// user feedback message so the loop continues with the model aware of both.
// The assistant text was already persisted by the caller.
func (r *REPL) appendTurnContinuation(messages []api.Message, assistantText, feedback string) []api.Message {
	if strings.TrimSpace(assistantText) != "" {
		messages = append(messages, api.Message{Role: "assistant", Content: []api.ContentBlock{{Type: "text", Text: assistantText}}})
	}
	feedbackBlock := api.ContentBlock{Type: "text", Text: feedback}
	messages = append(messages, api.Message{Role: "user", Content: []api.ContentBlock{feedbackBlock}})
	_ = r.sessionMgr.AddStructuredMessage("user", []api.ContentBlock{feedbackBlock}, true, nil)
	return messages
}

type planDecision int

const (
	planDecisionApprove planDecision = iota
	planDecisionRefine
	planDecisionCancel
)

// promptPlanDecision asks the user to approve, refine, or cancel a plan. In
// non-interactive runs it cancels so nothing executes without explicit approval.
func (r *REPL) promptPlanDecision() planDecision {
	if r == nil || !r.interactive {
		return planDecisionCancel
	}
	choices := []termui.Choice{
		{Label: "批准并执行", Description: "退出计划模式并立即执行该计划", Value: "approve"},
		{Label: "继续完善", Description: "留在计划模式，继续调研并完善计划", Value: "refine"},
		{Label: "取消", Description: "退出计划模式，不执行", Value: "cancel"},
	}
	color.Cyan("\n计划已生成，请确认是否执行。")
	index, err := termui.SelectWithReader("是否执行该计划？", choices, 0, r.readLineInput)
	if err != nil {
		color.Yellow("计划确认已跳过：%v", err)
		return planDecisionCancel
	}
	if index < 0 || index >= len(choices) {
		return planDecisionCancel
	}
	switch choices[index].Value {
	case "approve":
		color.Green("已批准，开始执行计划。")
		return planDecisionApprove
	case "refine":
		color.Cyan("继续完善计划。")
		return planDecisionRefine
	default:
		color.Yellow("已取消执行。")
		return planDecisionCancel
	}
}

const maxGoalContinuations = 12

// goalContinuationFeedback decides whether goal mode should keep going. It
// returns a self-check prompt and true while the goal is active and the
// continuation budget is not exhausted.
func (r *REPL) goalContinuationFeedback(used int) (string, bool) {
	if used >= maxGoalContinuations {
		color.Yellow("goal mode reached the autonomous continuation limit (%d); handing back to the user.", maxGoalContinuations)
		return "", false
	}
	status := tools.GoalStatus()
	if status == "achieved" || status == "blocked" {
		return "", false
	}
	goal := tools.CurrentGoal()
	tools.IncrementGoalIteration()

	var b strings.Builder
	b.WriteString("Goal-mode self-check before stopping.\n")
	if goal.Objective != "" {
		fmt.Fprintf(&b, "Objective: %s\n", goal.Objective)
	}
	if len(goal.DoneWhen) > 0 {
		fmt.Fprintf(&b, "Acceptance criteria:\n- %s\n", strings.Join(goal.DoneWhen, "\n- "))
	} else {
		b.WriteString("No explicit acceptance criteria are recorded yet. Define them with the Goal tool (done_when) before continuing.\n")
	}
	b.WriteString("Evaluate rigorously, from evidence already gathered, whether every acceptance criterion is met. ")
	b.WriteString("If any criterion is unmet and still achievable, state the single most important next action and continue with tools. ")
	b.WriteString("If all criteria are met, or you are genuinely blocked, finish now with a concise final report and set the Goal status to achieved or blocked.")
	return b.String(), true
}

// finishGoalTurn records the terminal outcome of a goal-mode turn.
func (r *REPL) finishGoalTurn() {
	status := tools.GoalStatus()
	switch status {
	case "achieved":
		color.Green("goal achieved")
	case "blocked":
		color.Yellow("goal blocked")
	}
}

func (r *REPL) shouldParseTextToolCalls(text string) bool {
	if r == nil || r.config == nil {
		return false
	}
	if r.config.AI.AllowTextToolCalls {
		return true
	}
	normalized := normalizeTextToolMarkup(text)
	return strings.Contains(strings.ToLower(normalized), "<tool_calls>") ||
		strings.Contains(strings.ToLower(normalized), "<invoke ")
}

func (r *REPL) effectiveMaxToolRounds() int {
	maxRounds := 16
	if r != nil && r.config != nil && r.config.AI.MaxToolRounds > 0 {
		maxRounds = r.config.AI.MaxToolRounds
	}
	if r == nil || r.config == nil {
		return maxRounds
	}
	switch config.NormalizeWorkMode(r.config.AI.WorkMode) {
	case "code-audit", "pentest", "ai-security":
		if maxRounds <= 16 {
			return 48
		}
	}
	return maxRounds
}

func (r *REPL) autoSelectWorkMode(userMsg string) {
	if r == nil || r.config == nil {
		return
	}
	detected := config.DetectWorkModeFromPrompt(userMsg)
	if detected == "" {
		return
	}
	current := config.NormalizeWorkMode(r.config.AI.WorkMode)
	if current == detected {
		return
	}
	r.config.AI.WorkMode = detected
	color.Cyan("auto work mode: %s", detected)
}

// executeTool 执行工具调用
func (r *REPL) executeTool(ctx context.Context, toolBlock *api.ContentBlock) (*tools.ToolResult, error) {
	name := toolBlock.Name
	input := normalizeToolInput(toolBlock.Input)

	r.printToolStart(&api.ContentBlock{Name: name, Input: input})
	if r.toolOutputMode() == "debug" {
		color.White("  input: %s\n", formatToolInputShort(input))
	}
	if preview := toolChangePreview(name, input); preview != "" {
		color.Cyan("  preview:\n%s", preview)
	}
	pre := r.runHook(hooks.PreToolUse, hooks.Payload{"tool_name": name, "tool_input": input})
	if pre.Blocked {
		return &tools.ToolResult{
			Content: "操作被 PreToolUse hook 阻止: " + pre.Reason,
			IsError: true,
		}, nil
	}

	// Plan mode is read-only: block anything that would change the workspace or
	// external systems until the user approves the plan and execution resumes.
	if r.agentMode() == config.AgentModePlan && !config.IsPlanModeAllowedTool(name) {
		result := &tools.ToolResult{
			Content: fmt.Sprintf("plan mode is read-only: the %s tool is blocked. Investigate with read-only tools (Read, Glob, Grep, LSP, Skills) and present a plan for the user to approve before executing. Do not retry mutating tools.", name),
			IsError: true,
		}
		r.printToolResult(name, input, result)
		return result, nil
	}

	// 通过工具注册表执行
	var toolAnimation *thinkingAnimation
	if result := r.enforcePentestTestLevel(name, input); result != nil {
		r.printToolResult(name, input, result)
		return result, nil
	}

	decision, err := r.toolRegistry.EnsureAllowed(name, input)
	if err != nil {
		if toolAnimation != nil {
			toolAnimation.Stop()
		}
		return nil, err
	}
	if !decision.Allowed {
		if toolAnimation != nil {
			toolAnimation.Stop()
		}
		if decision.Result == nil {
			decision.Result = &tools.ToolResult{Content: "operation not allowed: " + decision.Reason, IsError: true}
		}
		r.printToolResult(name, input, decision.Result)
		return decision.Result, nil
	}
	if toolAnimation != nil {
		toolAnimation.Stop()
	}
	toolAnimation = r.startToolAnimation(name)
	result, err := r.toolRegistry.ExecuteAllowed(ctx, name, input)
	if toolAnimation != nil {
		toolAnimation.Stop()
	}
	if err != nil {
		r.runHook(hooks.PostToolUseFailure, hooks.Payload{"tool_name": name, "tool_input": input, "error": err.Error()})
		return nil, err
	}
	event := hooks.PostToolUse
	if result.IsError {
		event = hooks.PostToolUseFailure
	}
	r.runHook(event, hooks.Payload{"tool_name": name, "tool_input": input, "tool_response": result})
	if r.toolOutputMode() == "trace" {
		r.writeToolTrace(name, input, result)
	}
	if isAgentStateTool(name) {
		r.syncAgentStateToSession()
	}

	// 显示结果摘要（截断）
	outputPreview := result.Content
	_ = outputPreview
	if len(outputPreview) > 500 {
		outputPreview = outputPreview[:500] + "\n... (输出已截断)"
	}
	r.printToolResult(name, input, result)

	return result, nil
}

// handleCommand 处理斜杠命令
func (r *REPL) restoreAgentStateFromSession() {
	if r == nil || r.sessionMgr == nil {
		return
	}
	data := r.sessionMgr.AgentStateJSON()
	if len(data) == 0 {
		data = []byte("{}")
	}
	if err := tools.RestoreAgentStateJSON(data); err != nil {
		color.Yellow("agent state restore warning: %v", err)
	}
}

func (r *REPL) syncAgentStateToSession() {
	if r == nil || r.sessionMgr == nil {
		return
	}
	data, err := tools.ExportAgentStateJSON()
	if err != nil {
		color.Yellow("agent state sync warning: %v", err)
		return
	}
	r.sessionMgr.SetAgentStateJSON(data)
}

func isAgentStateTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "todo", "mission", "goal", "scope", "finding", "evidence", "assessmentplan", "securitycoverage", "securitytest":
		return true
	default:
		return false
	}
}

func (r *REPL) enforcePentestTestLevel(name string, input map[string]interface{}) *tools.ToolResult {
	if r == nil || r.config == nil || config.NormalizeWorkMode(r.config.AI.WorkMode) != "pentest" {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(name), "Shell") && !strings.EqualFold(strings.TrimSpace(name), "Bash") {
		return nil
	}
	level := strings.ToLower(strings.TrimSpace(tools.CurrentAssessmentPlan().TestLevel))
	if level == "" {
		return nil
	}
	command := strings.ToLower(strings.TrimSpace(fmt.Sprint(input["command"])))
	if command == "" {
		return nil
	}
	if blocked, reason := commandBlockedByTestLevel(level, command); blocked {
		return &tools.ToolResult{
			Content: fmt.Sprintf("blocked by pentest test_level=%s: %s. Update AssessmentPlan.test_level only when explicitly authorized.", level, reason),
			IsError: true,
		}
	}
	return nil
}

func commandBlockedByTestLevel(level string, command string) (bool, string) {
	intrusiveTerms := []string{" rm ", " del ", " erase ", " remove-item ", " format ", " shutdown", " reboot", " msfconsole", " meterpreter", " persist", " reverse shell", " bind shell"}
	bruteForceTerms := []string{" hydra", " medusa", " patator", " credential stuffing", " password spray", " brute"}
	scannerTerms := []string{" nmap", " masscan", " zmap", " fscan", " gobuster", " ffuf", " dirsearch", " dirb", " nikto", " nuclei", " sqlmap"}
	wafBypassTerms := []string{" chunked", " ibm037", " waf bypass", " evasion", " smuggling", " h2c"}
	internalScanTerms := []string{" 10.", " 172.16.", " 172.17.", " 172.18.", " 172.19.", " 172.2", " 172.30.", " 172.31.", " 192.168.", " 127.0.0.1", " localhost"}
	padded := " " + command + " "
	switch level {
	case "passive":
		if containsAnySubstring(padded, scannerTerms...) || containsAnySubstring(padded, bruteForceTerms...) {
			return true, "passive mode allows observation and very small verification only, not scanners or brute-force tooling"
		}
		if containsAnySubstring(padded, " curl ", " wget ", " invoke-webrequest ", " iwr ", " irm ") && !containsAnySubstring(padded, " -i ", " --head ", " -i ", " -l ") {
			return true, "passive mode blocks active HTTP fetching except header-style checks"
		}
		fallthrough
	case "low-noise":
		if containsAnySubstring(padded, scannerTerms...) || containsAnySubstring(padded, bruteForceTerms...) {
			return true, "low-noise mode blocks scanners, brute force, and broad enumeration"
		}
		if containsAnySubstring(padded, wafBypassTerms...) {
			return true, "low-noise mode blocks WAF/CDN bypass and protocol stress techniques"
		}
	case "active":
		if containsAnySubstring(padded, bruteForceTerms...) {
			return true, "active mode still blocks brute force and credential attacks"
		}
		if containsAnySubstring(padded, wafBypassTerms...) {
			return true, "active mode requires explicit intrusive authorization for WAF/CDN bypass or protocol stress"
		}
		if containsAnySubstring(padded, internalScanTerms...) && containsAnySubstring(padded, scannerTerms...) {
			return true, "active mode blocks internal network scanning without intrusive authorization"
		}
	case "intrusive":
	default:
		return true, "unknown test level"
	}
	if level != "intrusive" && containsAnySubstring(padded, intrusiveTerms...) {
		return true, "this command looks destructive, persistent, or post-exploitation oriented"
	}
	return false, ""
}

func containsAnySubstring(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func (r *REPL) printToolStart(tool *api.ContentBlock) {
	if r.toolOutputMode() == "hidden" {
		return
	}
	input, _ := tool.Input.(map[string]interface{})
	label := toolStartLabel(tool.Name, input)
	if label == "" || r.toolOutputMode() == "debug" {
		fmt.Printf("\n\n> %s\n", displayToolName(tool.Name))
		return
	}
	fmt.Printf("\n\n> %s\n", label)
}

func (r *REPL) printToolResult(name string, input map[string]interface{}, result *tools.ToolResult) {
	mode := r.toolOutputMode()
	if mode == "hidden" && (result == nil || !result.IsError) {
		return
	}
	if result == nil {
		color.Yellow("  no result\n")
		return
	}
	if mode == "minimal" && !result.IsError {
		color.Green("  done\n")
		return
	}
	if mode == "debug" {
		outputPreview := result.Content
		if len(outputPreview) > 500 {
			outputPreview = outputPreview[:500] + "\n... (output truncated)"
		}
		fmt.Printf("  output: %s\n", outputPreview)
		return
	}
	summary := toolResultSummary(name, input, result)
	if result.IsError {
		color.Red("  failed: %s\n", summary)
	} else {
		color.Green("  done: %s\n", summary)
	}
}

func (r *REPL) toolOutputMode() string {
	if r == nil || r.config == nil {
		return "compact"
	}
	switch strings.ToLower(strings.TrimSpace(r.config.UI.ToolOutput)) {
	case "full", "debug":
		return "debug"
	case "minimal":
		return "minimal"
	case "trace":
		return "trace"
	case "hidden", "off", "quiet":
		return "hidden"
	default:
		return "compact"
	}
}

func toolStartLabel(name string, input map[string]interface{}) string {
	switch strings.ToLower(name) {
	case "shell", "bash":
		command := stringInput(input, "command")
		if command == "" {
			return displayToolName(name)
		}
		return fmt.Sprintf("%s: %s", displayToolName(name), truncateSingleLine(command, 90))
	case "read":
		filePath := stringInput(input, "file_path")
		if filePath == "" {
			return "Read: missing file_path"
		}
		return fmt.Sprintf("Read: %s", truncateSingleLine(filePath, 90))
	case "write", "edit":
		filePath := stringInput(input, "file_path")
		if filePath == "" {
			return fmt.Sprintf("%s: missing file_path", name)
		}
		return fmt.Sprintf("%s: %s", name, truncateSingleLine(filePath, 90))
	case "grep":
		return fmt.Sprintf("Grep: %s", truncateSingleLine(fmt.Sprint(input["pattern"]), 90))
	case "glob":
		return fmt.Sprintf("Glob: %s", truncateSingleLine(fmt.Sprint(input["pattern"]), 90))
	default:
		return displayToolName(name)
	}
}

func normalizeToolInput(raw interface{}) map[string]interface{} {
	if raw == nil {
		return map[string]interface{}{}
	}
	if input, ok := raw.(map[string]interface{}); ok {
		if rawJSON, ok := input["raw"].(string); ok {
			if parsed := parseToolInputJSON(rawJSON); parsed != nil {
				return parsed
			}
		}
		return input
	}
	if rawJSON, ok := raw.(string); ok {
		if parsed := parseToolInputJSON(rawJSON); parsed != nil {
			return parsed
		}
		return map[string]interface{}{"raw": rawJSON}
	}
	return map[string]interface{}{"raw": fmt.Sprint(raw)}
}

func parseToolInputJSON(value string) map[string]interface{} {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(value), &input); err != nil {
		return nil
	}
	return input
}

func displayToolName(name string) string {
	if strings.EqualFold(strings.TrimSpace(name), "bash") {
		return "Shell"
	}
	return strings.TrimSpace(name)
}

func stringInput(input map[string]interface{}, key string) string {
	if input == nil {
		return ""
	}
	value, ok := input[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func toolResultSummary(name string, input map[string]interface{}, result *tools.ToolResult) string {
	if result == nil {
		return "no result"
	}
	outputMap, _ := result.Output.(map[string]interface{})
	switch strings.ToLower(name) {
	case "shell", "bash":
		exitCode := fmt.Sprint(outputMap["exitCode"])
		output := fmt.Sprint(outputMap["output"])
		if output == "<nil>" {
			output = result.Content
		}
		return fmt.Sprintf("exit=%s, output=%s", exitCode, formatBytes(len(output)))
	case "read":
		if result.IsError {
			return truncateSingleLine(result.Content, 140)
		}
		return fmt.Sprintf("read %s", truncateSingleLine(stringInput(input, "file_path"), 80))
	case "write":
		if result.IsError {
			return truncateSingleLine(result.Content, 140)
		}
		return fmt.Sprintf("wrote %s", truncateSingleLine(stringInput(input, "file_path"), 80))
	case "edit":
		if result.IsError {
			return truncateSingleLine(result.Content, 140)
		}
		return fmt.Sprintf("edited %s", truncateSingleLine(stringInput(input, "file_path"), 80))
	case "grep", "glob":
		return summarizeJSONCount(result.Content)
	default:
		if result.IsError {
			return truncateSingleLine(result.Content, 140)
		}
		return "ok"
	}
}

func summarizeJSONCount(content string) string {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(content), &data); err == nil {
		if count, ok := data["count"]; ok {
			return fmt.Sprintf("count=%v", count)
		}
	}
	return truncateSingleLine(content, 120)
}

func truncateSingleLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func formatBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func (r *REPL) writeToolTrace(name string, input map[string]interface{}, result *tools.ToolResult) {
	root := r.config.Project.Path
	if strings.TrimSpace(root) == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		}
	}
	dir := filepath.Join(root, ".cache", "tool-runs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		color.Yellow("trace warning: %v", err)
		return
	}
	data, _ := json.MarshalIndent(map[string]interface{}{
		"time":   time.Now().Format(time.RFC3339),
		"tool":   name,
		"input":  input,
		"result": result,
	}, "", "  ")
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.json", time.Now().Format("20060102-150405.000"), strings.ToLower(name)))
	if err := os.WriteFile(path, data, 0644); err != nil {
		color.Yellow("trace warning: %v", err)
		return
	}
	color.Cyan("  trace: %s", path)
}

func (r *REPL) showTokenStatus() {
	sess := r.sessionMgr.Get()
	if sess == nil {
		color.Yellow("no active session")
		return
	}
	fmt.Println(color.CyanString("\nToken context:"))
	fmt.Printf("  %s\n", r.sessionContextLine())
	fmt.Printf("  total: input=%s output=%s combined=%s\n",
		formatTokenCount(sess.TokenUsage.InputTokens),
		formatTokenCount(sess.TokenUsage.OutputTokens),
		formatTokenCount(sess.TokenUsage.InputTokens+sess.TokenUsage.OutputTokens),
	)
	fmt.Printf("  context window: %s\n", formatTokenCount(r.contextWindow()))
	fmt.Println()
}

func (r *REPL) tokenContextLine(usage *api.Usage) string {
	if usage == nil {
		estimated := r.estimatedContextTokens()
		window := r.contextWindow()
		percent := percentOf(estimated, window)
		return fmt.Sprintf("context: %s %5.1f%%  current=~%s / %s  total: %s",
			tokenBar(percent, 18),
			percent,
			formatTokenCount(estimated),
			formatTokenCount(window),
			r.sessionTotalTokens(),
		)
	}
	window := r.contextWindow()
	percent := percentOf(usage.InputTokens, window)
	return fmt.Sprintf("context: %s %5.1f%%  turn: in=%s out=%s  total: %s",
		tokenBar(percent, 18),
		percent,
		formatTokenCount(usage.InputTokens),
		formatTokenCount(usage.OutputTokens),
		r.sessionTotalTokens(),
	)
}

func (r *REPL) realtimeContextLine() string {
	current := r.estimatedContextTokens()
	if r != nil && r.sessionMgr != nil {
		if sess := r.sessionMgr.Get(); sess != nil {
			current = latestInputTokens(current, sess.Messages)
			if current == 0 {
				current = r.estimatedContextTokens()
			}
		}
	}
	window := r.contextWindow()
	percent := percentOf(current, window)
	return fmt.Sprintf("ctx %s %4.1f%%  %s / %s",
		tokenBar(percent, 14),
		percent,
		formatTokenCount(current),
		formatTokenCount(window),
	)
}

func (r *REPL) statusLine() string {
	cwd := ""
	if r != nil && r.config != nil {
		cwd = filepath.Base(strings.TrimRight(r.config.Project.Path, `\/`))
	}
	if cwd == "" || cwd == "." {
		if wd, err := os.Getwd(); err == nil {
			cwd = filepath.Base(wd)
		}
	}
	mode := "default"
	if r != nil && r.permChecker != nil {
		mode = r.permChecker.GetCurrentPermissionMode()
	}
	model := ""
	if r != nil && r.config != nil {
		model = r.config.AI.Model
	}
	return fmt.Sprintf("%s | mode %s | cwd %s | model %s", r.realtimeContextLine(), mode, cwd, model)
}

func (r *REPL) compactContextStatus() string {
	if r == nil {
		return "ctx 0.0%"
	}
	current := r.estimatedContextTokens()
	if r.sessionMgr != nil {
		if sess := r.sessionMgr.Get(); sess != nil {
			current = latestInputTokens(current, sess.Messages)
		}
	}
	return fmt.Sprintf("ctx %.1f%%", percentOf(current, r.contextWindow()))
}

func (r *REPL) sessionContextLine() string {
	if r == nil || r.sessionMgr == nil {
		return "no active session"
	}
	sess := r.sessionMgr.Get()
	if sess == nil {
		return "no active session"
	}
	window := r.contextWindow()
	current := latestInputTokens(sess.TokenUsage.InputTokens, sess.Messages)
	if current == 0 {
		current = r.estimatedContextTokens()
	}
	percent := percentOf(current, window)
	prefix := ""
	if latestInputTokens(0, sess.Messages) == 0 {
		prefix = "~"
	}
	return fmt.Sprintf("%s %5.1f%%  current=%s%s / %s  session=%s",
		tokenBar(percent, 18),
		percent,
		prefix,
		formatTokenCount(current),
		formatTokenCount(window),
		r.sessionTotalTokens(),
	)
}

func (r *REPL) estimatedContextTokens() int {
	if r == nil || r.sessionMgr == nil {
		return 0
	}
	textBytes := len(r.buildSystemPrompt())
	for _, msg := range r.messagesForRequest(r.sessionMgr.GetMessagesForAPI(r.getMaxMessages()), r.buildSystemPrompt()) {
		textBytes += estimateMessageBytes(msg)
	}
	return estimateTokensFromBytes(textBytes)
}

func (r *REPL) rawEstimatedContextTokens(systemPrompt string) int {
	if r == nil || r.sessionMgr == nil {
		return 0
	}
	textBytes := len(systemPrompt)
	for _, msg := range r.sessionMgr.GetMessagesForAPI(r.getMaxMessages()) {
		textBytes += estimateMessageBytes(msg)
	}
	return estimateTokensFromBytes(textBytes)
}

func (r *REPL) autoCompactIfNeeded(systemPrompt string) bool {
	if r == nil || r.config == nil || r.sessionMgr == nil || !r.config.Session.CompressHistory {
		return false
	}
	raw := r.rawEstimatedContextTokens(systemPrompt)
	window := r.contextWindow()
	if window <= 0 || raw < window*3/4 {
		return false
	}
	sess := r.sessionMgr.Get()
	if sess == nil || len(sess.Messages) < 24 {
		return false
	}
	r.addAgentStateSnapshot()
	remaining := r.sessionMgr.CompactWithSummary(20)
	color.Yellow("context reached %s/%s; compacted history to %d messages",
		formatTokenCount(raw),
		formatTokenCount(window),
		remaining,
	)
	return true
}

func (r *REPL) compactForRetry() bool {
	if r == nil || r.config == nil || r.sessionMgr == nil || !r.config.Session.CompressHistory {
		return false
	}
	sess := r.sessionMgr.Get()
	if sess == nil || len(sess.Messages) < 4 {
		return false
	}
	r.addAgentStateSnapshot()
	r.sessionMgr.CompactWithSummary(12)
	return true
}

func (r *REPL) seedWorkModeState() {
	if r == nil || r.config == nil {
		return
	}
	switch config.NormalizeWorkMode(r.config.AI.WorkMode) {
	case "pentest":
		tools.SeedPentestTodosIfEmpty()
	case "ai-security":
		tools.SeedAISecurityTodosIfEmpty()
	case "code-audit":
		tools.SeedCodeAuditTodosIfEmpty()
	}
}

func (r *REPL) addAgentStateSnapshot() {
	summary := tools.AgentStateSummary()
	if strings.TrimSpace(summary) == "" {
		return
	}
	_ = r.sessionMgr.AddStructuredMessage("user", []api.ContentBlock{{
		Type: "text",
		Text: "Agent state snapshot for continuity before compaction:\n" + summary,
	}}, true, nil)
}

func (r *REPL) maxAPIRetries() int {
	if r == nil || r.config == nil {
		return 2
	}
	if r.config.AI.MaxRetries < 0 {
		return 0
	}
	if r.config.AI.MaxRetries > 5 {
		return 5
	}
	return r.config.AI.MaxRetries
}

func apiRetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	delay := time.Duration(1<<attempt) * time.Second
	if delay > 8*time.Second {
		return 8 * time.Second
	}
	return delay
}

func isContextLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"context length",
		"context window",
		"too many tokens",
		"maximum context",
		"request too large",
		"token limit",
		"exceeds the context",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func isRetryableAPIError(err error) bool {
	if err == nil || isContextLimitError(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"429",
		"500",
		"502",
		"503",
		"504",
		"timeout",
		"temporarily",
		"connection reset",
		"connection refused",
		"eof",
		"server error",
		"rate limit",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func parseTextToolCalls(text string) []api.ContentBlock {
	normalized := normalizeTextToolMarkup(text)
	invokeRe := regexp.MustCompile(`(?is)<\s*invoke\s+([^>]*)>(.*?)</\s*invoke\s*>`)
	paramRe := regexp.MustCompile(`(?is)<\s*parameter\s+([^>]*)>(.*?)</\s*parameter\s*>`)
	var blocks []api.ContentBlock
	matches := invokeRe.FindAllStringSubmatch(normalized, -1)
	for i, match := range matches {
		name := attrValue(match[1], "name")
		if strings.TrimSpace(name) == "" {
			continue
		}
		input := map[string]interface{}{}
		for _, param := range paramRe.FindAllStringSubmatch(match[2], -1) {
			paramName := attrValue(param[1], "name")
			if strings.TrimSpace(paramName) == "" {
				continue
			}
			raw := html.UnescapeString(strings.TrimSpace(param[2]))
			input[paramName] = parseTextToolParameter(raw, attrValue(param[1], "string"))
		}
		blocks = append(blocks, api.ContentBlock{
			Type:  "tool_use",
			ID:    fmt.Sprintf("text-tool-%d-%d", time.Now().UnixNano(), i),
			Name:  normalizeToolCallName(name),
			Input: input,
		})
	}
	return blocks
}

func normalizeTextToolMarkup(text string) string {
	open := regexp.MustCompile(`(?is)<\s*(?:\|\s*)*DSML\s*(?:\|\s*)+`)
	close := regexp.MustCompile(`(?is)</\s*(?:\|\s*)*DSML\s*(?:\|\s*)+`)
	text = open.ReplaceAllString(text, "<")
	text = close.ReplaceAllString(text, "</")
	return text
}

func stripTextToolCalls(text string) string {
	normalized := normalizeTextToolMarkup(text)
	toolCallsRe := regexp.MustCompile(`(?is)<\s*tool_calls\s*>.*?</\s*tool_calls\s*>`)
	stripped := toolCallsRe.ReplaceAllString(normalized, "")
	invokeRe := regexp.MustCompile(`(?is)<\s*invoke\s+[^>]*>.*?</\s*invoke\s*>`)
	stripped = invokeRe.ReplaceAllString(stripped, "")
	return strings.TrimSpace(stripped)
}

func attrValue(attrs string, name string) string {
	re := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(name) + `\s*=\s*"([^"]*)"`)
	match := re.FindStringSubmatch(attrs)
	if len(match) < 2 {
		return ""
	}
	return html.UnescapeString(match[1])
}

func parseTextToolParameter(value string, stringAttr string) interface{} {
	if strings.EqualFold(strings.TrimSpace(stringAttr), "true") {
		return value
	}
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		return n
	}
	switch strings.ToLower(value) {
	case "true":
		return true
	case "false":
		return false
	default:
		return value
	}
}

func normalizeToolCallName(name string) string {
	name = strings.TrimSpace(name)
	if strings.EqualFold(name, "bash") {
		return "Shell"
	}
	return name
}

func (r *REPL) messagesForRequest(messages []api.Message, systemPrompt string) []api.Message {
	out := cloneAPIMessages(messages)
	budget := r.requestInputBudget()
	if budget <= 0 {
		return out
	}
	for estimateRequestTokens(systemPrompt, out) > budget {
		if boundary := nextUserTurnBoundary(out); boundary > 0 {
			out = out[boundary:]
			continue
		}
		if shrinkLargestToolResult(out, systemPrompt, budget) {
			continue
		}
		break
	}
	return out
}

func (r *REPL) requestInputBudget() int {
	window := 200000
	maxTokens := 8192
	if r != nil && r.config != nil {
		if r.config.AI.ContextWindow > 0 {
			window = r.config.AI.ContextWindow
		}
		if r.config.AI.MaxTokens > 0 {
			maxTokens = r.config.AI.MaxTokens
		}
	}
	reserve := maxTokens + 2048
	minReserve := window / 20
	if reserve < minReserve {
		reserve = minReserve
	}
	budget := window - reserve
	if budget < 1024 {
		budget = window * 3 / 4
	}
	if budget < 256 {
		budget = 256
	}
	return budget
}

func estimateRequestTokens(systemPrompt string, messages []api.Message) int {
	textBytes := len(systemPrompt)
	for _, msg := range messages {
		textBytes += estimateMessageBytes(msg)
	}
	return estimateTokensFromBytes(textBytes)
}

func cloneAPIMessages(messages []api.Message) []api.Message {
	out := make([]api.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, api.Message{
			Role:    msg.Role,
			Content: cloneMessageContent(msg.Content),
		})
	}
	return out
}

func cloneMessageContent(content interface{}) interface{} {
	if blocks, ok := messageContentBlocks(content); ok {
		return append([]api.ContentBlock(nil), blocks...)
	}
	return content
}

func nextUserTurnBoundary(messages []api.Message) int {
	for i := 1; i < len(messages); i++ {
		if strings.EqualFold(messages[i].Role, "user") && !messageIsOnlyToolResult(messages[i]) {
			return i
		}
	}
	return -1
}

func messageIsOnlyToolResult(msg api.Message) bool {
	blocks, ok := messageContentBlocks(msg.Content)
	if !ok || len(blocks) == 0 {
		return false
	}
	for _, block := range blocks {
		if block.Type != "tool_result" {
			return false
		}
	}
	return true
}

func messageContentBlocks(content interface{}) ([]api.ContentBlock, bool) {
	switch blocks := content.(type) {
	case []api.ContentBlock:
		return blocks, true
	case []interface{}:
		data, err := json.Marshal(blocks)
		if err != nil {
			return nil, false
		}
		var parsed []api.ContentBlock
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, false
		}
		return parsed, true
	default:
		return nil, false
	}
}

func shrinkLargestToolResult(messages []api.Message, systemPrompt string, budget int) bool {
	largestMsg := -1
	largestBlock := -1
	largestLen := 0
	for i, msg := range messages {
		blocks, ok := messageContentBlocks(msg.Content)
		if !ok {
			continue
		}
		for j, block := range blocks {
			if block.Type == "tool_result" && len(block.Content) > largestLen {
				largestMsg = i
				largestBlock = j
				largestLen = len(block.Content)
			}
		}
	}
	if largestMsg < 0 || largestLen <= 0 {
		return false
	}

	blocks, _ := messageContentBlocks(messages[largestMsg].Content)
	currentTokens := estimateRequestTokens(systemPrompt, messages)
	overTokens := currentTokens - budget
	keepBytes := largestLen - (overTokens+512)*4
	if keepBytes > largestLen/2 {
		keepBytes = largestLen / 2
	}
	if keepBytes < 2048 {
		keepBytes = 2048
	}
	if keepBytes >= largestLen {
		keepBytes = largestLen / 2
	}

	original := blocks[largestBlock].Content
	blocks[largestBlock].Content = abbreviateToolResult(original, keepBytes)
	messages[largestMsg].Content = blocks
	return len(blocks[largestBlock].Content) < largestLen
}

func abbreviateToolResult(content string, keepBytes int) string {
	if keepBytes <= 0 || len(content) <= keepBytes {
		return content
	}
	head := keepBytes * 2 / 3
	tail := keepBytes - head
	if head < 0 {
		head = 0
	}
	if tail < 0 {
		tail = 0
	}
	return content[:head] +
		fmt.Sprintf("\n\n[tool result truncated for context: omitted %d bytes]\n\n", len(content)-keepBytes) +
		content[len(content)-tail:]
}

func estimateMessageBytes(msg api.Message) int {
	data, err := json.Marshal(msg.Content)
	if err != nil {
		return len(fmt.Sprint(msg.Content))
	}
	return len(data) + len(msg.Role) + 8
}

func estimateTokensFromBytes(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	tokens := bytes / 4
	if bytes%4 != 0 {
		tokens++
	}
	if tokens < 1 {
		return 1
	}
	return tokens
}

func latestInputTokens(fallback int, messages []session.SessionMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Usage != nil && messages[i].Usage.InputTokens > 0 {
			return messages[i].Usage.InputTokens
		}
	}
	return fallback
}

func (r *REPL) sessionTotalTokens() string {
	if r == nil || r.sessionMgr == nil {
		return "0"
	}
	sess := r.sessionMgr.Get()
	if sess == nil {
		return "0"
	}
	return formatTokenCount(sess.TokenUsage.InputTokens + sess.TokenUsage.OutputTokens)
}

func (r *REPL) contextWindow() int {
	if r != nil && r.config != nil && r.config.AI.ContextWindow > 0 {
		return r.config.AI.ContextWindow
	}
	return 200000
}

func percentOf(value int, total int) float64 {
	if total <= 0 {
		return 0
	}
	percent := float64(value) * 100 / float64(total)
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func tokenBar(percent float64, width int) string {
	if width <= 0 {
		width = 18
	}
	filled := int(percent * float64(width) / 100)
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", width-filled) + "]"
}

func formatTokenCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fm", float64(n)/1000000)
}

func (r *REPL) handleCommand(cmd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}

	command := parts[0]
	args := parts[1:]

	switch command {
	case "/help", "/h":
		r.showHelp()
		return false
	case "/quit", "/exit", "/q":
		color.Green("👋 再见！")
		r.cancel()
		return true
	case "/clear", "/cls":
		clearScreen()
		return false
	case "/mode":
		r.handleModeCommand(args)
		return false
	case "/agent", "/agentmode":
		r.handleAgentModeCommand(args)
		return false
	case "/plan":
		r.handlePlanCommand(args)
		return false
	case "/goal":
		r.handleGoalCommand(args)
		return false
	case "/workmode", "/wm":
		r.handleWorkModeCommand(args)
		return false
	case "/status":
		r.showStatus()
		return false
	case "/tokens":
		r.showTokenStatus()
		return false
	case "/history":
		r.showHistory()
		return false
	case "/compact":
		r.compactSession()
		return false
	case "/skills":
		r.handleSkillsCommand(args)
		return false
	case "/memory":
		r.handleMemoryCommand(args)
		return false
	case "/mcp":
		if len(args) > 0 && args[0] == "reconnect" {
			r.reconnectMCP()
		} else {
			r.showMCP()
		}
		return false
	case "/report":
		r.writeReport(args)
		return false
	case "/mission":
		r.handleMissionCommand(args)
		return false
	case "/scope":
		r.handleScopeCommand(args)
		return false
	case "/assessment", "/aplan":
		r.handleAssessmentPlanCommand(args)
		return false
	case "/finding":
		r.handleFindingCommand(args)
		return false
	case "/evidence":
		r.handleEvidenceCommand(args)
		return false
	case "/coverage":
		r.handleSecurityCoverageCommand(args)
		return false
	case "/vulns":
		r.handleSecurityCatalogCommand(args)
		return false
	case "/audit":
		r.handleCodeAuditCatalogCommand(args)
		return false
	case "/sectest":
		r.handleSecurityTestCommand(args)
		return false
	case "/test":
		r.suggestTests()
		return false
	case "/todo":
		r.showTodo()
		return false
	default:
		color.Yellow("未知命令: %s。输入 /help 查看帮助。", command)
		return false
	}
}

// showWelcome 显示欢迎信息
func (r *REPL) showWelcome() {
	fmt.Println()
	fmt.Printf("T-REX %s\n", currentVersion())
	fmt.Printf("model: %s\n", r.config.AI.Model)
	fmt.Printf("work mode: %s\n", config.NormalizeWorkMode(r.config.AI.WorkMode))
	if mode := r.agentMode(); mode != config.AgentModeNormal {
		fmt.Printf("agent mode: %s\n", mode)
	}
	if r.config.Project.Path != "" {
		fmt.Printf("cwd: %s\n", r.config.Project.Path)
	}
	fmt.Printf("mode: %s\n", r.permChecker.GetCurrentPermissionMode())
	fmt.Println("type /help for commands")
	fmt.Println()
}

// showHelp 显示帮助信息
func (r *REPL) showHelp() {
	fmt.Println(color.CyanString("\ncommands"))
	fmt.Println(color.WhiteString(`
  /help, /h                    show help
  /quit, /exit, /q             quit
  /clear, /cls                 clear screen
  /mode [default|bypass|plan]  switch permission mode
  /agent [normal|plan|goal]    switch agent workflow mode
  /plan [request]              enter plan mode (read-only, plan then approve)
  /goal [objective]            enter goal mode; see: show|status|criteria|progress|clear
  /workmode <mode>             switch agent work mode
  /status                      show session status
  /tokens                      show token and context usage
  /history                     show recent messages
  /paste [instruction]         paste multiline input; finish with /end
  /writepaste <file_path>      write multiline content to a workspace file
  /compact                     trim conversation history
  /skills [list|read|reload]   list or read project skills
  /memory [list|files|add|delete]
                               manage project memory
  /mcp                         show MCP servers and tools
  /mission [show|set|phase]    manage task mission state
  /scope [add|list|clear]      manage authorized scope
  /assessment [show|set|level]  manage assessment plan and test level
  /coverage [list|seed|gaps]   manage security coverage matrix
  /finding [list|show|clear]   manage structured findings
  /evidence [list|show|clear]  manage evidence index
  /vulns [list|search|show]    browse the security test catalog
  /audit [list|search|show]    browse secure code audit checklists
  /sectest [list|show|add]     manage AI/agent security test cases
  /report [session|pentest|ai-security|code-audit]
                               write a markdown report
  /test                        suggest project test commands
  /todo                        show current agent checklist
`))
}

// showStatus 显示当前状态
func (r *REPL) showStatus() {
	sess := r.sessionMgr.Get()
	if sess == nil {
		color.Yellow("没有活动的会话")
		return
	}

	fmt.Println(color.CyanString("\n📊 会话状态:"))
	fmt.Printf(color.WhiteString("  会话 ID: %s\n"), sess.ID)
	fmt.Printf(color.WhiteString("  创建时间: %s\n"), sess.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf(color.WhiteString("  消息数量: %d\n"), len(sess.Messages))
	fmt.Printf(color.WhiteString("  Token 使用: 输入=%d, 输出=%d\n"),
		sess.TokenUsage.InputTokens, sess.TokenUsage.OutputTokens)
	fmt.Printf(color.WhiteString("  Context: %s\n"), r.sessionContextLine())
	fmt.Printf(color.WhiteString("  Work mode: %s\n"), config.NormalizeWorkMode(r.config.AI.WorkMode))
	fmt.Printf(color.WhiteString("  Agent mode: %s\n"), r.agentMode())
	fmt.Printf(color.WhiteString("  权限模式: %s\n"), r.permChecker.GetCurrentPermissionMode())
	fmt.Println()
}

// showHistory 显示会话历史摘要
func (r *REPL) showHistory() {
	sess := r.sessionMgr.Get()
	if sess == nil || len(sess.Messages) == 0 {
		color.Yellow("会话历史为空")
		return
	}

	fmt.Println(color.CyanString("\n📜 会话历史 (最近20条):"))
	count := len(sess.Messages)
	start := 0
	if count > 20 {
		start = count - 20
	}

	for i := start; i < count; i++ {
		msg := sess.Messages[i]
		roleIcon := "👤"
		roleColor := color.WhiteString
		if msg.Role == "assistant" {
			roleIcon = "🤖"
			roleColor = color.CyanString
		}

		preview := msg.Content
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		preview = strings.ReplaceAll(preview, "\n", " ")

		fmt.Printf("  [%s] %s %s\n",
			msg.Timestamp.Format("15:04:05"),
			roleColor("%s:", roleIcon),
			preview)
	}
	fmt.Println()
}

// compactSession 压缩会话
func (r *REPL) compactSession() {
	sess := r.sessionMgr.Get()
	if sess == nil {
		color.Yellow("没有活动会话")
		return
	}

	if len(sess.Messages) > 10 {
		r.addAgentStateSnapshot()
		remaining := r.sessionMgr.CompactWithSummary(10)
		color.Green("✓ 会话已摘要压缩，保留摘要和最近 %d 条消息", remaining-1)
	} else {
		color.Yellow("会话已足够小，无需压缩")
	}
}

func (r *REPL) showSkills() {
	catalog, err := skills.Discover(r.config)
	if err != nil {
		color.Red("加载 Skills 失败: %v", err)
		return
	}
	items := catalog.List()
	if len(items) == 0 {
		color.Yellow("没有发现 Skills")
		return
	}

	fmt.Println(color.CyanString("\n🧩 Skills:"))
	for _, skill := range items {
		fmt.Printf("  %s - %s\n", skill.ID, skill.Description)
		fmt.Printf("    %s\n", skill.Path)
	}
	fmt.Println()
}

func (r *REPL) handleSkillsCommand(args []string) {
	if len(args) == 0 || args[0] == "list" {
		r.showSkillsClean()
		return
	}
	catalog, err := skills.Discover(r.config)
	if err != nil {
		color.Red("failed to load skills: %v", err)
		return
	}
	switch args[0] {
	case "read":
		if len(args) < 2 {
			color.Yellow("usage: /skills read <id>")
			return
		}
		skill, content, err := catalog.Read(args[1])
		if err != nil {
			color.Red("%v", err)
			return
		}
		fmt.Printf("\n%s - %s\n%s\n\n%s\n\n", skill.ID, skill.Description, skill.Path, content)
	case "reload":
		color.Green("skills reloaded (%d found)", len(catalog.List()))
	default:
		color.Yellow("unknown /skills command: %s", args[0])
		color.White("usage: /skills [list|read <id>|reload]")
	}
}

func (r *REPL) showSkillsClean() {
	catalog, err := skills.Discover(r.config)
	if err != nil {
		color.Red("failed to load skills: %v", err)
		return
	}
	items := catalog.List()
	if len(items) == 0 {
		color.Yellow("no skills found")
		return
	}
	fmt.Println(color.CyanString("\nSkills:"))
	for _, skill := range items {
		fmt.Printf("  %s - %s\n", skill.ID, skill.Description)
		fmt.Printf("    source: %s\n", skill.Source)
		fmt.Printf("    path: %s\n", skill.Path)
	}
	fmt.Println()
}

func (r *REPL) showMemory() {
	ctx, err := memory.Load(r.config)
	if err != nil {
		color.Red("加载记忆失败: %v", err)
		return
	}
	fmt.Println(color.CyanString("\n🧠 Memory:"))
	if len(ctx.Files) == 0 {
		color.Yellow("  没有加载到 CLAUDE.md、规则文件或自动记忆")
	} else {
		for _, file := range ctx.Files {
			preview := strings.ReplaceAll(file.Content, "\n", " ")
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			fmt.Printf("  %s\n    %s\n    %s\n", file.Label, file.Path, preview)
		}
	}
	if ctx.AutoMemoryOn {
		fmt.Printf("  Auto memory: %s\n", ctx.AutoMemoryDir)
	}
	fmt.Println()
}

func (r *REPL) handleMemoryCommand(args []string) {
	if len(args) == 0 || args[0] == "list" {
		r.listMemory()
		return
	}
	switch args[0] {
	case "add":
		category := "fact"
		noteArgs := args[1:]
		if len(noteArgs) >= 2 && isMemoryCategory(noteArgs[0]) {
			category = noteArgs[0]
			noteArgs = noteArgs[1:]
		}
		note := strings.TrimSpace(strings.Join(noteArgs, " "))
		if note == "" {
			color.Yellow("usage: /memory add [fact|preference|build|debug] <note>")
			return
		}
		path, err := memory.RememberCategory(r.config, category, note)
		if err != nil {
			color.Red("failed to save memory: %v", err)
			return
		}
		color.Green("saved memory to %s", path)
	case "delete", "del", "rm":
		if len(args) < 2 {
			color.Yellow("usage: /memory delete <index>")
			return
		}
		index, err := strconv.Atoi(args[1])
		if err != nil {
			color.Red("invalid memory index: %s", args[1])
			return
		}
		path, err := memory.Delete(r.config, index)
		if err != nil {
			color.Red("failed to delete memory: %v", err)
			return
		}
		color.Green("deleted memory %d from %s", index, path)
	case "files":
		r.showMemoryFilesClean()
	default:
		color.Yellow("unknown /memory command: %s", args[0])
		color.White("usage: /memory [list|files|add [category] <note>|delete <index>]")
	}
}

func (r *REPL) showMemoryFilesClean() {
	ctx, err := memory.Load(r.config)
	if err != nil {
		color.Red("failed to load memory files: %v", err)
		return
	}
	fmt.Println(color.CyanString("\nMemory files:"))
	if len(ctx.Files) == 0 {
		color.Yellow("  no instruction or memory files loaded")
	} else {
		for _, file := range ctx.Files {
			preview := strings.ReplaceAll(file.Content, "\n", " ")
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			fmt.Printf("  %s\n    path: %s\n    preview: %s\n", file.Label, file.Path, preview)
		}
	}
	if ctx.AutoMemoryOn {
		fmt.Printf("  auto memory: %s\n", ctx.AutoMemoryDir)
	}
	fmt.Println()
}

func (r *REPL) listMemory() {
	entries, path, err := memory.List(r.config)
	if err != nil {
		color.Red("failed to list memory: %v", err)
		return
	}
	fmt.Println(color.CyanString("\nMemory:"))
	fmt.Printf("  %s\n", path)
	if len(entries) == 0 {
		color.Yellow("  no saved memories")
		fmt.Println()
		return
	}
	for _, entry := range entries {
		fmt.Printf("  %d. [%s] %s\n", entry.Index, entry.Category, entry.Note)
	}
	fmt.Println()
}

func isMemoryCategory(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fact", "preference", "pref", "build", "command", "debug":
		return true
	default:
		return false
	}
}

func (r *REPL) showMCP() {
	fmt.Println(color.CyanString("\n🔌 MCP:"))
	if len(r.config.MCP.Servers) == 0 {
		color.Yellow("  没有配置 MCP 服务器")
		return
	}
	for _, server := range r.config.MCP.Servers {
		status := "disabled"
		if server.Enabled {
			status = "enabled"
		}
		transport := server.Type
		if transport == "" {
			if server.URL != "" {
				transport = "http"
			} else {
				transport = "stdio"
			}
		}
		target := server.Command
		if server.URL != "" {
			target = server.URL
		}
		fmt.Printf("  %s [%s/%s] %s\n", server.Name, transport, status, target)
	}
	fmt.Println("  Registered tools:")
	for _, tool := range r.toolRegistry.List() {
		if strings.HasPrefix(tool.Name(), "mcp__") {
			fmt.Printf("    %s\n", tool.Name())
		}
	}
	fmt.Println()
}

func (r *REPL) reconnectMCP() {
	if r.mcpManager == nil {
		color.Yellow("MCP manager is not available")
		return
	}
	_ = r.mcpManager.CloseAll()
	if err := r.mcpManager.StartAll(context.Background()); err != nil {
		color.Red("MCP reconnect failed: %v", err)
		return
	}
	if err := r.toolRegistry.RegisterMCPTools(context.Background(), r.mcpManager); err != nil {
		color.Red("MCP tool registration failed: %v", err)
		return
	}
	color.Green("MCP reconnected")
}

func (r *REPL) writeReport(args []string) {
	sess := r.sessionMgr.Get()
	if sess == nil {
		color.Yellow("no active session")
		return
	}
	reportType := "session"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		reportType = strings.ToLower(strings.TrimSpace(args[0]))
	}
	root := r.config.Project.Path
	if strings.TrimSpace(root) == "" {
		if cwd, err := os.Getwd(); err == nil {
			root = cwd
		}
	}
	dir := filepath.Join(root, ".cache", "reports")
	if err := os.MkdirAll(dir, 0755); err != nil {
		color.Red("failed to create report directory: %v", err)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-report-%s.md", reportType, time.Now().Format("20060102-150405")))
	var content string
	if reportType == "pentest" {
		r.warnPentestQualityGate()
		content = r.renderPentestReport(sess)
	} else if reportType == "ai-security" || reportType == "llm-security" {
		reportType = "ai-security"
		path = filepath.Join(dir, fmt.Sprintf("%s-report-%s.md", reportType, time.Now().Format("20060102-150405")))
		r.warnFindingQualityGate()
		content = r.renderAISecurityReport(sess)
	} else if reportType == "code-audit" || reportType == "audit" || reportType == "sast" {
		reportType = "code-audit"
		path = filepath.Join(dir, fmt.Sprintf("%s-report-%s.md", reportType, time.Now().Format("20060102-150405")))
		r.warnFindingQualityGate()
		content = r.renderCodeAuditReport(sess)
	} else {
		content = r.renderSessionReport(sess)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		color.Red("failed to write report: %v", err)
		return
	}
	r.syncAgentStateToSession()
	color.Green("report written: %s", path)
}

func (r *REPL) renderSessionReport(sess *session.Session) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# T-REX Session Report\n\n")
	fmt.Fprintf(&b, "- Session: `%s`\n- Model: `%s`\n- Work mode: `%s`\n- Path: `%s`\n- Messages: %d\n- Tokens: input=%d output=%d\n\n",
		sess.ID, sess.Model, config.NormalizeWorkMode(r.config.AI.WorkMode), sess.Path, len(sess.Messages), sess.TokenUsage.InputTokens, sess.TokenUsage.OutputTokens)
	b.WriteString("## Recent Activity\n\n")
	start := 0
	if len(sess.Messages) > 30 {
		start = len(sess.Messages) - 30
	}
	for _, msg := range sess.Messages[start:] {
		role := msg.Role
		if msg.Internal {
			role += " internal"
		}
		fmt.Fprintf(&b, "### %s %s\n\n%s\n\n", role, msg.Timestamp.Format("15:04:05"), truncateSingleLine(msg.Content, 1200))
		for _, tool := range msg.Tools {
			fmt.Fprintf(&b, "- tool: `%s`\n", tool.Name)
		}
		if len(msg.Tools) > 0 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (r *REPL) warnPentestQualityGate() {
	r.warnFindingQualityGate()
	var missingReason []tools.SecurityCoverageRecord
	for _, item := range tools.CurrentSecurityCoverage() {
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if (status == "planned" || status == "not-tested" || status == "blocked" || status == "") && strings.TrimSpace(item.Reason) == "" {
			missingReason = append(missingReason, item)
		}
	}
	if len(missingReason) == 0 {
		return
	}
	color.Yellow("coverage quality gate: %d unresolved coverage item(s) lack a reason", len(missingReason))
	for _, item := range missingReason {
		color.Yellow("  - %s %s %s", item.ID, firstNonEmpty(item.CatalogID, item.Area), firstNonEmpty(item.Status, "planned"))
	}
}

func (r *REPL) warnFindingQualityGate() {
	issues := tools.FindingQualityIssues(tools.CurrentFindings())
	if len(issues) == 0 {
		return
	}
	color.Yellow("finding quality gate: %d confirmed finding(s) are not report-ready", len(issues))
	for _, issue := range issues {
		color.Yellow("  - %s missing: %s", firstNonEmpty(issue.FindingID, issue.Title), strings.Join(issue.Missing, ", "))
	}
}

func (r *REPL) renderPentestReport(sess *session.Session) string {
	mission := tools.CurrentMission()
	plan := tools.CurrentAssessmentPlan()
	scope := tools.CurrentScope()
	coverage := tools.CurrentSecurityCoverage()
	findings := tools.CurrentFindings()
	evidence := tools.CurrentEvidence()
	if len(scope) == 0 {
		scope = mission.Scope
	}
	if len(scope) == 0 {
		scope = plan.InScope
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Penetration Test Report\n\n")
	fmt.Fprintf(&b, "- Session: `%s`\n- Model: `%s`\n- Work mode: `%s`\n- Project path: `%s`\n- Generated: `%s`\n\n",
		sess.ID, sess.Model, config.NormalizeWorkMode(r.config.AI.WorkMode), sess.Path, time.Now().Format(time.RFC3339))
	b.WriteString("## Executive Summary\n\n")
	if mission.Goal != "" {
		fmt.Fprintf(&b, "%s\n\n", mission.Goal)
	} else {
		b.WriteString("Assessment summary is pending. Add mission context with `/mission set <goal>` or the Mission tool.\n\n")
	}
	if mission.Deliverable != "" {
		fmt.Fprintf(&b, "- Deliverable: %s\n", mission.Deliverable)
	}
	if mission.DoneWhen != "" {
		fmt.Fprintf(&b, "- Completion condition: %s\n", mission.DoneWhen)
	}
	if mission.BlockedBy != "" {
		fmt.Fprintf(&b, "- Blocked by: %s\n", mission.BlockedBy)
	}
	if mission.Phase != "" {
		fmt.Fprintf(&b, "- Current phase: %s\n", mission.Phase)
	}
	writeFindingStatusSummary(&b, findings)
	writeCoverageStatusSummary(&b, coverage)
	b.WriteString("\n## Scope\n\n")
	if len(scope) == 0 {
		b.WriteString("- Scope not recorded.\n")
	} else {
		for _, item := range scope {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	writeAssessmentPlanSection(&b, plan)

	b.WriteString("\n## Methodology\n\n")
	b.WriteString("- Scope and rules of engagement review\n")
	b.WriteString("- Reconnaissance and fingerprinting\n")
	b.WriteString("- Authentication, session, access control, and business logic review\n")
	b.WriteString("- Vulnerability validation with evidence capture\n")
	b.WriteString("- Risk analysis, remediation guidance, and retest planning\n")

	writeSecurityCoverageSection(&b, coverage)

	b.WriteString("\n## Risk Table\n\n")
	b.WriteString("| ID | Severity | Status | Asset | Endpoint | Title |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	if len(findings) == 0 {
		b.WriteString("| - | - | - | - | - | No findings recorded |\n")
	} else {
		for _, f := range findings {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				mdCell(f.ID), mdCell(f.Severity), mdCell(f.Status), mdCell(f.Asset), mdCell(f.Endpoint), mdCell(f.Title))
		}
	}

	writeFindingQualityGateSection(&b, tools.FindingQualityIssues(findings))

	b.WriteString("\n## Findings\n\n")
	if len(findings) == 0 {
		if hasTestedCoverage(coverage) {
			b.WriteString("No confirmed vulnerabilities were recorded from the structured coverage completed in this assessment. See Coverage Gaps And Residual Risk for untested, blocked, or not-applicable areas.\n")
		} else {
			b.WriteString("No structured findings recorded. Use the Finding tool or `/finding` commands during testing.\n")
		}
	} else {
		for _, f := range findings {
			fmt.Fprintf(&b, "### %s - %s\n\n", f.ID, firstNonEmpty(f.Title, "Untitled finding"))
			fmt.Fprintf(&b, "- Severity: %s\n- Confidence: %s\n- Status: %s\n- Category: %s\n- CWE: %s\n- OWASP: %s\n- CVSS: %s\n- Asset: %s\n- Endpoint: %s\n- Parameter: %s\n\n",
				firstNonEmpty(f.Severity, "unspecified"), firstNonEmpty(f.Confidence, "unspecified"), firstNonEmpty(f.Status, "unspecified"), firstNonEmpty(f.Category, "unspecified"), firstNonEmpty(f.CWE, "-"), firstNonEmpty(f.OWASP, "-"), firstNonEmpty(f.CVSS, "-"), firstNonEmpty(f.Asset, "unspecified"), firstNonEmpty(f.Endpoint, "unspecified"), firstNonEmpty(f.Parameter, "-"))
			writeReportSection(&b, "Prerequisites", f.Prerequisites)
			writeReportSection(&b, "Reproduction", f.Reproduction)
			writeReportSection(&b, "Observed Behavior", f.Observed)
			writeReportSection(&b, "Expected Behavior", f.Expected)
			writeReportSection(&b, "Evidence", f.Evidence)
			writeReportSection(&b, "Impact", f.Impact)
			writeReportSection(&b, "Remediation", f.Remediation)
			writeReportSection(&b, "Retest Steps", f.Retest)
			writeReportSection(&b, "Notes", f.Notes)
			b.WriteString("#### Burp Request\n\n")
			writeCodeBlock(&b, firstNonEmpty(f.Request, "Not captured."))
			b.WriteString("#### Burp Response\n\n")
			writeCodeBlock(&b, firstNonEmpty(f.Response, "Not captured."))
		}
	}

	b.WriteString("\n## Evidence Files\n\n")
	if len(evidence) == 0 {
		b.WriteString("- No evidence files recorded.\n")
	} else {
		for _, e := range evidence {
			fmt.Fprintf(&b, "- `%s` %s %s `%s`\n", e.ID, e.Type, e.Summary, e.Path)
		}
	}

	b.WriteString("\n## Coverage Gaps And Residual Risk\n\n")
	writeResidualRiskSummary(&b, mission, coverage, findings)
	b.WriteString("\n## Next Steps\n\n")
	b.WriteString("- Retest remediated findings.\n")
	b.WriteString("- Close or downgrade findings only after evidence-backed validation.\n")
	return b.String()
}

func (r *REPL) renderCodeAuditReport(sess *session.Session) string {
	mission := tools.CurrentMission()
	scope := tools.CurrentScope()
	findings := tools.CurrentFindings()
	evidence := tools.CurrentEvidence()
	if len(scope) == 0 {
		scope = mission.Scope
	}

	var b strings.Builder
	b.WriteString("# Secure Code Audit Report\n\n")
	fmt.Fprintf(&b, "- Session: `%s`\n- Model: `%s`\n- Work mode: `%s`\n- Project path: `%s`\n- Generated: `%s`\n\n",
		sess.ID, sess.Model, config.NormalizeWorkMode(r.config.AI.WorkMode), sess.Path, time.Now().Format(time.RFC3339))

	b.WriteString("## Executive Summary\n\n")
	b.WriteString(firstNonEmpty(mission.Goal, "Code audit summary is pending.") + "\n\n")
	fmt.Fprintf(&b, "- Findings: %d\n", len(findings))
	writeFindingStatusSummary(&b, findings)
	fmt.Fprintf(&b, "- Evidence files: %d\n", len(evidence))
	if mission.Deliverable != "" {
		fmt.Fprintf(&b, "- Deliverable: %s\n", mission.Deliverable)
	}
	if mission.DoneWhen != "" {
		fmt.Fprintf(&b, "- Completion condition: %s\n", mission.DoneWhen)
	}
	if mission.BlockedBy != "" {
		fmt.Fprintf(&b, "- Blocked by: %s\n", mission.BlockedBy)
	}

	b.WriteString("\n## Scope\n\n")
	if len(scope) == 0 {
		b.WriteString("- Scope not recorded.\n")
	} else {
		for _, item := range scope {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}

	b.WriteString("\n## Methodology\n\n")
	b.WriteString("- Repository and framework fingerprinting\n")
	b.WriteString("- Entry point, route/controller/page, middleware, job, and trust-boundary mapping\n")
	b.WriteString("- Language-specific CodeAuditCatalog checklist selection\n")
	b.WriteString("- Source-to-sink data-flow review for user, file, config, queue, database, and network-controlled inputs\n")
	b.WriteString("- Authorization, injection, file/upload, SSRF, deserialization, template/output, crypto, secrets, and supply-chain review\n")
	b.WriteString("- Finding validation, evidence capture, remediation mapping, and retest planning\n")

	b.WriteString("\n## Finding Summary\n\n")
	b.WriteString("| ID | Severity | Confidence | Status | CWE | Asset | Component | Title |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	if len(findings) == 0 {
		b.WriteString("| - | - | - | - | - | - | - | No findings recorded |\n")
	} else {
		for _, f := range findings {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
				mdCell(f.ID), mdCell(f.Severity), mdCell(f.Confidence), mdCell(f.Status), mdCell(f.CWE), mdCell(f.Asset), mdCell(f.Endpoint), mdCell(f.Title))
		}
	}

	writeFindingQualityGateSection(&b, tools.FindingQualityIssues(findings))

	b.WriteString("\n## Findings\n\n")
	if len(findings) == 0 {
		b.WriteString("No confirmed findings recorded. Use the Finding tool or `/finding` commands during review.\n")
	} else {
		for _, f := range findings {
			fmt.Fprintf(&b, "### %s - %s\n\n", f.ID, firstNonEmpty(f.Title, "Untitled finding"))
			fmt.Fprintf(&b, "- Severity: %s\n- Confidence: %s\n- Status: %s\n- Category: %s\n- CWE: %s\n- OWASP: %s\n- Asset/File: %s\n- Component/Function/Route: %s\n- Parameter/Source: %s\n\n",
				firstNonEmpty(f.Severity, "unspecified"), firstNonEmpty(f.Confidence, "unspecified"), firstNonEmpty(f.Status, "unspecified"), firstNonEmpty(f.Category, "unspecified"), firstNonEmpty(f.CWE, "-"), firstNonEmpty(f.OWASP, "-"), firstNonEmpty(f.Asset, "unspecified"), firstNonEmpty(f.Endpoint, "unspecified"), firstNonEmpty(f.Parameter, "-"))
			writeReportSection(&b, "Prerequisites", f.Prerequisites)
			writeReportSection(&b, "Source-To-Sink Proof", f.Reproduction)
			writeReportSection(&b, "Observed Code Behavior", f.Observed)
			writeReportSection(&b, "Expected Secure Behavior", f.Expected)
			writeReportSection(&b, "Evidence", f.Evidence)
			writeReportSection(&b, "Impact", f.Impact)
			writeReportSection(&b, "Remediation", f.Remediation)
			writeReportSection(&b, "Retest Steps", f.Retest)
			writeReportSection(&b, "Notes", f.Notes)
			b.WriteString("#### POC\n\n")
			b.WriteString("Burp Request\n\n")
			writeCodeBlock(&b, firstNonEmpty(f.Request, "Not captured. For HTTP-facing code audit findings, add a captured or clearly reconstructed Burp-style raw request before marking the finding confirmed."))
			b.WriteString("Burp Response\n\n")
			writeCodeBlock(&b, firstNonEmpty(f.Response, "Not captured. For HTTP-facing code audit findings, add a captured or clearly reconstructed Burp-style raw response before marking the finding confirmed."))
		}
	}

	b.WriteString("\n## Evidence Files\n\n")
	if len(evidence) == 0 {
		b.WriteString("- No evidence files recorded.\n")
	} else {
		for _, item := range evidence {
			fmt.Fprintf(&b, "- `%s` %s %s `%s`\n", item.ID, item.Type, item.Summary, item.Path)
		}
	}

	b.WriteString("\n## Limitations And Follow-Up\n\n")
	if mission.BlockedBy != "" {
		fmt.Fprintf(&b, "- %s\n", mission.BlockedBy)
	} else {
		b.WriteString("- Record unreviewed languages, generated directories, missing runtime context, and items that need dynamic validation.\n")
	}
	b.WriteString("- Re-run focused unit/integration/security tests after remediation and re-check the affected source-to-sink path.\n")
	return b.String()
}

func (r *REPL) renderAISecurityReport(sess *session.Session) string {
	mission := tools.CurrentMission()
	scope := tools.CurrentScope()
	tests := tools.CurrentSecurityTests()
	findings := tools.CurrentFindings()
	evidence := tools.CurrentEvidence()
	if len(scope) == 0 {
		scope = mission.Scope
	}

	var b strings.Builder
	b.WriteString("# AI And Agent Security Assessment Report\n\n")
	fmt.Fprintf(&b, "- Session: `%s`\n- Model: `%s`\n- Work mode: `%s`\n- Generated: `%s`\n\n",
		sess.ID, sess.Model, config.NormalizeWorkMode(r.config.AI.WorkMode), time.Now().Format(time.RFC3339))
	b.WriteString("## Executive Summary\n\n")
	b.WriteString(firstNonEmpty(mission.Goal, "Assessment summary is pending.") + "\n\n")
	fmt.Fprintf(&b, "- Test cases: %d\n- Findings: %d\n", len(tests), len(findings))
	writeFindingStatusSummary(&b, findings)
	if mission.BlockedBy != "" {
		fmt.Fprintf(&b, "- Blocked by: %s\n", mission.BlockedBy)
	}

	b.WriteString("\n## Scope And System Profile\n\n")
	if len(scope) == 0 {
		b.WriteString("- Scope not recorded.\n")
	} else {
		for _, item := range scope {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	b.WriteString("\nRecord model/application versions, roles, trust boundaries, RAG sources, memory, tools, MCP/plugins, output sinks, and rate/cost constraints in scope or evidence.\n")

	b.WriteString("\n## Methodology\n\n")
	b.WriteString("- Baseline behavior and threat-boundary mapping\n")
	b.WriteString("- Direct injection, jailbreak, multilingual, encoding, and multi-turn mutation testing\n")
	b.WriteString("- Indirect injection through retrieval, documents, web content, tool results, and inter-agent messages\n")
	b.WriteString("- Prompt/context leakage, RAG isolation, privacy, and cross-tenant testing\n")
	b.WriteString("- Tool authorization, excessive agency, memory poisoning, MCP/plugin, and output-sink testing\n")
	b.WriteString("- Decision integrity, loop/fan-out limits, resource budgets, evidence capture, and retesting\n")

	b.WriteString("\n## Test Coverage Matrix\n\n")
	b.WriteString("| ID | Vector | Channel | Turn | Outcome | Severity | Finding | Title |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	if len(tests) == 0 {
		b.WriteString("| - | - | - | - | not-run | - | - | No structured tests recorded |\n")
	} else {
		for _, test := range tests {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
				mdCell(test.ID), mdCell(test.Vector), mdCell(test.Channel), mdCell(test.Turn), mdCell(test.Outcome), mdCell(test.Severity), mdCell(test.FindingID), mdCell(test.Title))
		}
	}

	b.WriteString("\n## Test Case Details\n\n")
	for _, test := range tests {
		fmt.Fprintf(&b, "### %s - %s\n\n", test.ID, firstNonEmpty(test.Title, "Untitled test"))
		fmt.Fprintf(&b, "- Vector: %s\n- Channel: %s\n- Turn: %s\n- Outcome: %s\n- Severity: %s\n- Evidence: %s\n- Finding: %s\n\n",
			firstNonEmpty(test.Vector, "-"), firstNonEmpty(test.Channel, "-"), firstNonEmpty(test.Turn, "-"), firstNonEmpty(test.Outcome, "not-run"), firstNonEmpty(test.Severity, "-"), firstNonEmpty(test.EvidenceID, "-"), firstNonEmpty(test.FindingID, "-"))
		writeReportSection(&b, "Objective", test.Objective)
		writeReportSection(&b, "Payload Or Transcript Reference", test.PayloadRef)
		writeReportSection(&b, "Expected Secure Behavior", test.Expected)
		writeReportSection(&b, "Actual Behavior", test.Actual)
		writeReportSection(&b, "Notes", test.Notes)
	}

	b.WriteString("\n## Findings\n\n")
	if len(findings) == 0 {
		b.WriteString("No confirmed findings recorded.\n")
	}
	writeFindingQualityGateSection(&b, tools.FindingQualityIssues(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "### %s - %s\n\n", f.ID, firstNonEmpty(f.Title, "Untitled finding"))
		fmt.Fprintf(&b, "- Severity: %s\n- Confidence: %s\n- Status: %s\n- Category: %s\n- OWASP/CWE: %s / %s\n- Asset: %s\n- Endpoint: %s\n\n",
			firstNonEmpty(f.Severity, "-"), firstNonEmpty(f.Confidence, "-"), firstNonEmpty(f.Status, "-"), firstNonEmpty(f.Category, "-"), firstNonEmpty(f.OWASP, "-"), firstNonEmpty(f.CWE, "-"), firstNonEmpty(f.Asset, "-"), firstNonEmpty(f.Endpoint, "-"))
		writeReportSection(&b, "Prerequisites", f.Prerequisites)
		writeReportSection(&b, "Reproduction", f.Reproduction)
		writeReportSection(&b, "Observed Behavior", f.Observed)
		writeReportSection(&b, "Expected Behavior", f.Expected)
		writeReportSection(&b, "Impact", f.Impact)
		writeReportSection(&b, "Evidence", f.Evidence)
		writeReportSection(&b, "Remediation", f.Remediation)
		writeReportSection(&b, "Retest Steps", f.Retest)
		if f.Request != "" || f.Response != "" {
			b.WriteString("#### Burp Request\n\n")
			writeCodeBlock(&b, firstNonEmpty(f.Request, "Not captured."))
			b.WriteString("#### Burp Response\n\n")
			writeCodeBlock(&b, firstNonEmpty(f.Response, "Not captured."))
		}
	}

	b.WriteString("\n## Evidence Files\n\n")
	if len(evidence) == 0 {
		b.WriteString("- No evidence files recorded.\n")
	} else {
		for _, item := range evidence {
			fmt.Fprintf(&b, "- `%s` %s %s `%s`\n", item.ID, item.Type, item.Summary, item.Path)
		}
	}
	b.WriteString("\n## Limitations And Residual Risk\n\n")
	b.WriteString(firstNonEmpty(mission.BlockedBy, "Limitations not recorded; untested catalog entries remain residual risk.") + "\n")
	b.WriteString("\n## Retest And Next Steps\n\n- Re-run failed cases and mutation variants after remediation.\n- Verify that fixes hold across roles, channels, sessions, tools, and model/config versions.\n")
	return b.String()
}

func (r *REPL) suggestTests() {
	root := r.config.Project.Path
	if strings.TrimSpace(root) == "" {
		root, _ = os.Getwd()
	}
	var suggestions []string
	if fileExists(filepath.Join(root, "go.mod")) {
		suggestions = append(suggestions, "go test ./...")
	}
	if fileExists(filepath.Join(root, "package.json")) {
		suggestions = append(suggestions, "npm test", "npm run lint")
	}
	if fileExists(filepath.Join(root, "pyproject.toml")) || fileExists(filepath.Join(root, "pytest.ini")) {
		suggestions = append(suggestions, "pytest")
	}
	if len(suggestions) == 0 {
		color.Yellow("no obvious test command detected")
		return
	}
	color.Cyan("Suggested tests:")
	for _, cmd := range suggestions {
		color.White("  %s", cmd)
	}
}

func writeReportSection(b *strings.Builder, title string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	fmt.Fprintf(b, "#### %s\n\n%s\n\n", title, value)
}

func writeAssessmentPlanSection(b *strings.Builder, plan tools.AssessmentPlanState) {
	if plan.Objective == "" && plan.TargetProfile == "" && len(plan.OutOfScope) == 0 && len(plan.Roles) == 0 && plan.Rules == "" {
		return
	}
	b.WriteString("\n## Assessment Plan\n\n")
	writeReportSection(b, "Objective", plan.Objective)
	writeReportSection(b, "Target Profile", plan.TargetProfile)
	writeReportSection(b, "Test Level", plan.TestLevel)
	if len(plan.Roles) > 0 {
		b.WriteString("#### Roles\n\n")
		for _, role := range plan.Roles {
			fmt.Fprintf(b, "- %s\n", role)
		}
		b.WriteString("\n")
	}
	if len(plan.OutOfScope) > 0 {
		b.WriteString("#### Explicit Exclusions\n\n")
		for _, item := range plan.OutOfScope {
			fmt.Fprintf(b, "- %s\n", item)
		}
		b.WriteString("\n")
	}
	writeReportSection(b, "Rules Of Engagement", plan.Rules)
}

func writeSecurityCoverageSection(b *strings.Builder, coverage []tools.SecurityCoverageRecord) {
	b.WriteString("\n## Security Coverage\n\n")
	b.WriteString("| ID | Catalog | Area | Target | Status | Evidence | Finding | Reason |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	if len(coverage) == 0 {
		b.WriteString("| - | - | - | - | not-recorded | - | - | No structured coverage recorded |\n")
		return
	}
	for _, item := range coverage {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			mdCell(item.ID), mdCell(item.CatalogID), mdCell(item.Area), mdCell(item.Target), mdCell(item.Status), mdCell(item.EvidenceID), mdCell(item.FindingID), mdCell(item.Reason))
	}
}

func writeFindingQualityGateSection(b *strings.Builder, issues []tools.FindingQualityIssue) {
	if len(issues) == 0 {
		return
	}
	b.WriteString("\n## Finding Quality Gate\n\n")
	b.WriteString("The following confirmed findings need evidence or report-quality cleanup before delivery:\n\n")
	b.WriteString("| Finding | Status | Missing |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, issue := range issues {
		fmt.Fprintf(b, "| %s | %s | %s |\n", mdCell(firstNonEmpty(issue.FindingID, issue.Title)), mdCell(issue.Status), mdCell(strings.Join(issue.Missing, ", ")))
	}
}

func writeCoverageStatusSummary(b *strings.Builder, coverage []tools.SecurityCoverageRecord) {
	if len(coverage) == 0 {
		return
	}
	counts := coverageStatusCounts(coverage)
	for _, status := range []string{"tested", "planned", "not-tested", "blocked", "not-applicable"} {
		if counts[status] > 0 {
			fmt.Fprintf(b, "- %s coverage items: %d\n", status, counts[status])
			delete(counts, status)
		}
	}
	var extra []string
	for status := range counts {
		extra = append(extra, status)
	}
	sort.Strings(extra)
	for _, status := range extra {
		fmt.Fprintf(b, "- %s coverage items: %d\n", status, counts[status])
	}
}

func coverageStatusCounts(coverage []tools.SecurityCoverageRecord) map[string]int {
	counts := map[string]int{}
	for _, item := range coverage {
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if status == "" {
			status = "unspecified"
		}
		counts[status]++
	}
	return counts
}

func hasTestedCoverage(coverage []tools.SecurityCoverageRecord) bool {
	for _, item := range coverage {
		if strings.EqualFold(strings.TrimSpace(item.Status), "tested") {
			return true
		}
	}
	return false
}

func writeResidualRiskSummary(b *strings.Builder, mission tools.MissionState, coverage []tools.SecurityCoverageRecord, findings []tools.FindingRecord) {
	if mission.BlockedBy != "" {
		fmt.Fprintf(b, "- Blocker/limitation: %s\n", mission.BlockedBy)
	}
	if len(coverage) == 0 {
		b.WriteString("- Structured coverage was not recorded, so residual risk cannot be bounded from this report alone.\n")
	} else {
		gaps := coverageGaps(coverage)
		if len(gaps) == 0 {
			b.WriteString("- All recorded coverage items were tested or marked not applicable.\n")
		} else {
			for _, gap := range gaps {
				label := firstNonEmpty(gap.CatalogID, gap.Area, gap.ID)
				fmt.Fprintf(b, "- %s remains %s", label, firstNonEmpty(gap.Status, "unresolved"))
				if gap.Target != "" {
					fmt.Fprintf(b, " for %s", gap.Target)
				}
				if gap.Reason != "" {
					fmt.Fprintf(b, ": %s", gap.Reason)
				}
				if gap.NotTested != "" {
					fmt.Fprintf(b, " (%s)", gap.NotTested)
				}
				b.WriteString("\n")
			}
		}
	}
	if len(findings) == 0 {
		if hasTestedCoverage(coverage) {
			b.WriteString("- No confirmed vulnerabilities were recorded in tested coverage; this should be read together with the listed coverage gaps and test constraints.\n")
		} else {
			b.WriteString("- No confirmed vulnerabilities were recorded, but no tested coverage was captured either.\n")
		}
	} else if countFindingsByStatus(findings, "confirmed") == 0 {
		b.WriteString("- Findings exist, but none are confirmed; likely and validation-needed items remain residual risk until retested.\n")
	}
}

func coverageGaps(coverage []tools.SecurityCoverageRecord) []tools.SecurityCoverageRecord {
	var gaps []tools.SecurityCoverageRecord
	for _, item := range coverage {
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "planned", "not-tested", "blocked", "":
			gaps = append(gaps, item)
		}
	}
	return gaps
}

func countFindingsByStatus(findings []tools.FindingRecord, status string) int {
	count := 0
	for _, finding := range findings {
		if strings.EqualFold(strings.TrimSpace(finding.Status), status) {
			count++
		}
	}
	return count
}

func writeFindingStatusSummary(b *strings.Builder, findings []tools.FindingRecord) {
	if len(findings) == 0 {
		return
	}
	counts := map[string]int{}
	for _, finding := range findings {
		status := strings.ToLower(strings.TrimSpace(finding.Status))
		if status == "" {
			status = "unspecified"
		}
		counts[status]++
	}
	for _, status := range []string{"confirmed", "likely", "needs-dynamic-validation", "untested", "false-positive", "duplicate", "accepted-risk", "unspecified"} {
		if counts[status] > 0 {
			fmt.Fprintf(b, "- %s findings: %d\n", status, counts[status])
			delete(counts, status)
		}
	}
	var extra []string
	for status := range counts {
		extra = append(extra, status)
	}
	sort.Strings(extra)
	for _, status := range extra {
		fmt.Fprintf(b, "- %s findings: %d\n", status, counts[status])
	}
}

func writeCodeBlock(b *strings.Builder, value string) {
	b.WriteString("```http\n")
	b.WriteString(strings.TrimRight(value, "\n"))
	b.WriteString("\n```\n\n")
}

func mdCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func (r *REPL) showTodo() {
	items := tools.CurrentTodos()
	if len(items) == 0 {
		color.Yellow("todo list is empty")
		return
	}
	color.Cyan("Todo:")
	for i, item := range items {
		color.White("  %d. %s", i+1, item)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (r *REPL) handleMissionCommand(args []string) {
	if len(args) == 0 || args[0] == "show" {
		r.printStateTool(&tools.MissionTool{}, map[string]interface{}{"action": "show"})
		return
	}
	action := strings.ToLower(args[0])
	input := map[string]interface{}{"action": action}
	switch action {
	case "set":
		input["goal"] = strings.Join(args[1:], " ")
	case "phase":
		input["action"] = "update"
		input["phase"] = strings.Join(args[1:], " ")
	case "done":
		input["action"] = "update"
		input["done_when"] = strings.Join(args[1:], " ")
	case "blocked":
		input["action"] = "update"
		input["blocked_by"] = strings.Join(args[1:], " ")
	case "deliverable":
		input["action"] = "update"
		input["deliverable"] = strings.Join(args[1:], " ")
	case "clear":
	default:
		color.Yellow("usage: /mission [show|set|phase|done|blocked|deliverable|clear] <text>")
		return
	}
	r.printStateTool(&tools.MissionTool{}, input)
}

func (r *REPL) handleScopeCommand(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	action := strings.ToLower(args[0])
	input := map[string]interface{}{"action": action}
	switch action {
	case "add":
		input["target"] = strings.Join(args[1:], " ")
	case "remove":
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil {
				input["index"] = n
			}
		}
	case "list", "clear":
	default:
		color.Yellow("usage: /scope [add|list|remove|clear] <target|index>")
		return
	}
	r.printStateTool(&tools.ScopeTool{}, input)
}

func (r *REPL) handleAssessmentPlanCommand(args []string) {
	if len(args) == 0 || args[0] == "show" {
		r.printStateTool(&tools.AssessmentPlanTool{}, map[string]interface{}{"action": "show"})
		return
	}
	action := strings.ToLower(args[0])
	input := map[string]interface{}{"action": action}
	switch action {
	case "set":
		input["objective"] = strings.Join(args[1:], " ")
	case "profile":
		input["action"] = "update"
		input["target_profile"] = strings.Join(args[1:], " ")
	case "level":
		input["action"] = "update"
		if len(args) > 1 {
			input["test_level"] = args[1]
		}
	case "rules":
		input["action"] = "update"
		input["rules"] = strings.Join(args[1:], " ")
	case "clear":
	default:
		color.Yellow("usage: /assessment [show|set|profile|level|rules|clear] <text>")
		return
	}
	r.printStateTool(&tools.AssessmentPlanTool{}, input)
}

func (r *REPL) handleSecurityCoverageCommand(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	action := strings.ToLower(args[0])
	input := map[string]interface{}{"action": action}
	switch action {
	case "seed":
		var features []interface{}
		for _, feature := range args[1:] {
			features = append(features, feature)
		}
		input["target_features"] = features
	case "show":
		if len(args) > 1 {
			input["id"] = args[1]
		}
	case "add":
		if len(args) > 1 {
			input["area"] = strings.Join(args[1:], " ")
		}
	case "gaps":
		r.printCoverageGaps()
		return
	case "list", "clear":
	default:
		color.Yellow("usage: /coverage [list|seed <features...>|gaps|show <id>|add <area>|clear]")
		return
	}
	r.printStateTool(&tools.SecurityCoverageTool{}, input)
}

func (r *REPL) printCoverageGaps() {
	coverage := tools.CurrentSecurityCoverage()
	var gaps []tools.SecurityCoverageRecord
	for _, item := range coverage {
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "", "planned", "not-tested", "blocked":
			gaps = append(gaps, item)
		}
	}
	if len(gaps) == 0 {
		color.Green("no coverage gaps recorded")
		return
	}
	for _, gap := range gaps {
		fmt.Printf("%s [%s] %s %s", gap.ID, firstNonEmpty(gap.Status, "planned"), firstNonEmpty(gap.CatalogID, gap.Area), gap.Target)
		if gap.Reason != "" {
			fmt.Printf(" - %s", gap.Reason)
		}
		fmt.Println()
	}
}

func (r *REPL) handleFindingCommand(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	action := strings.ToLower(args[0])
	input := map[string]interface{}{"action": action}
	switch action {
	case "add":
		input["title"] = strings.Join(args[1:], " ")
	case "show":
		if len(args) > 1 {
			input["id"] = args[1]
		}
	case "list", "clear":
	default:
		color.Yellow("usage: /finding [add|list|show|clear] <title|id>")
		return
	}
	r.printStateTool(&tools.FindingTool{}, input)
}

func (r *REPL) handleEvidenceCommand(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	action := strings.ToLower(args[0])
	input := map[string]interface{}{"action": action}
	switch action {
	case "show":
		if len(args) > 1 {
			input["id"] = args[1]
		}
	case "list", "clear":
	default:
		color.Yellow("usage: /evidence [list|show|clear] <id>")
		return
	}
	r.printStateTool(&tools.EvidenceTool{}, input)
}

func (r *REPL) handleSecurityCatalogCommand(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	action := strings.ToLower(args[0])
	input := map[string]interface{}{"action": action}
	switch action {
	case "list":
		if len(args) > 1 {
			input["domain"] = args[1]
		}
	case "search":
		input["query"] = strings.Join(args[1:], " ")
	case "show":
		if len(args) > 1 {
			input["id"] = args[1]
		}
	default:
		color.Yellow("usage: /vulns [list [domain]|search <query>|show <id>]")
		return
	}
	r.printStateTool(&tools.SecurityCatalogTool{}, input)
}

func (r *REPL) handleCodeAuditCatalogCommand(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	action := strings.ToLower(args[0])
	input := map[string]interface{}{"action": action}
	switch action {
	case "list":
		if len(args) > 1 {
			input["language"] = args[1]
		}
	case "search":
		input["query"] = strings.Join(args[1:], " ")
	case "show":
		if len(args) > 1 {
			input["id"] = args[1]
		}
	default:
		color.Yellow("usage: /audit [list [language]|search <query>|show <id>]")
		return
	}
	r.printStateTool(&tools.CodeAuditCatalogTool{}, input)
}

func (r *REPL) handleSecurityTestCommand(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	action := strings.ToLower(args[0])
	input := map[string]interface{}{"action": action}
	switch action {
	case "add":
		input["title"] = strings.Join(args[1:], " ")
	case "show":
		if len(args) > 1 {
			input["id"] = args[1]
		}
	case "list", "clear":
	default:
		color.Yellow("usage: /sectest [add <title>|list|show <id>|clear]")
		return
	}
	r.printStateTool(&tools.SecurityTestTool{}, input)
}

func (r *REPL) printStateTool(tool tools.Tool, input map[string]interface{}) {
	result, err := tool.Execute(r.ctx, input)
	if err != nil {
		color.Red("%v", err)
		return
	}
	if result.IsError {
		color.Red("%s", result.Content)
		return
	}
	if isAgentStateTool(tool.Name()) {
		r.syncAgentStateToSession()
	}
	fmt.Println(result.Content)
}

// handleModeCommand 处理模式切换
func (r *REPL) handleModeCommand(args []string) {
	if len(args) == 0 {
		fmt.Printf(color.WhiteString("当前权限模式: %s\n"), r.permChecker.GetCurrentPermissionMode())
		fmt.Println(color.WhiteString("可用模式: default, bypass, plan"))
		return
	}

	mode := args[0]
	switch mode {
	case "default":
		r.config.Permissions.Mode = "default"
		color.Green("✓ 切换到默认模式（需要审批敏感操作）")
	case "bypass":
		if err := r.permChecker.SetBypassMode(true); err != nil {
			color.Red("切换失败: %v", err)
		} else {
			color.Yellow("⚠️  切换到绕过模式（所有操作自动批准）")
		}
	case "plan":
		r.config.Permissions.Mode = "plan"
		color.Green("✓ 切换到计划模式（暂停所有写操作）")
	default:
		color.Red("未知模式: %s", mode)
	}
}

func (r *REPL) handleWorkModeCommand(args []string) {
	current := config.NormalizeWorkMode(r.config.AI.WorkMode)
	if len(args) == 0 {
		choices := workModeChoices()
		defaultIndex := 0
		for i, choice := range choices {
			if config.NormalizeWorkMode(choice.Value) == current {
				defaultIndex = i
				break
			}
		}
		index, err := termui.Select("选择 work mode", choices, defaultIndex)
		if err != nil {
			color.Red("work mode selection failed: %v", err)
			return
		}
		r.applyWorkMode(choices[index].Value)
		return
	}

	mode := config.NormalizeWorkMode(args[0])
	if !config.IsKnownWorkMode(mode) {
		color.Red("unknown work mode: %s", args[0])
		color.White("available work modes: coding, code-audit, pentest, ai-security, bugfix, code-review, incident-response, malware-analysis, osint, ctf")
		return
	}

	r.applyWorkMode(mode)
}

func (r *REPL) applyWorkMode(mode string) {
	mode = config.NormalizeWorkMode(mode)
	r.config.AI.WorkMode = mode
	switch mode {
	case "pentest":
		tools.SeedPentestTodosIfEmpty()
		color.Green("work mode: pentest (authorized security assessment workflow)")
	case "ai-security":
		tools.SeedAISecurityTodosIfEmpty()
		color.Green("work mode: ai-security (authorized LLM and agent security assessment)")
	case "code-audit":
		tools.SeedCodeAuditTodosIfEmpty()
		color.Green("work mode: code-audit (secure source code audit workflow)")
	default:
		color.Green("work mode: %s", mode)
	}
}

// handleAgentModeCommand shows or switches the agent workflow mode (normal,
// plan, goal). It is orthogonal to the permission mode and the work mode.
func (r *REPL) handleAgentModeCommand(args []string) {
	current := r.agentMode()
	if len(args) == 0 {
		if r.interactive {
			choices := agentModeChoices()
			defaultIndex := 0
			for i, choice := range choices {
				if choice.Value == current {
					defaultIndex = i
					break
				}
			}
			color.Cyan("current agent mode: %s", current)
			index, err := termui.SelectWithReader("选择 agent 模式", choices, defaultIndex, r.readLineInput)
			if err != nil {
				color.Red("agent mode selection failed: %v", err)
				return
			}
			if index >= 0 && index < len(choices) {
				r.applyAgentMode(choices[index].Value)
			}
			return
		}
		color.White("current agent mode: %s", current)
		color.White("available agent modes: normal, plan, goal")
		return
	}

	mode := config.NormalizeAgentMode(args[0])
	if !config.IsKnownAgentMode(mode) {
		color.Red("unknown agent mode: %s", args[0])
		color.White("available agent modes: normal, plan, goal")
		return
	}
	r.applyAgentMode(mode)
}

func (r *REPL) applyAgentMode(mode string) {
	mode = config.NormalizeAgentMode(mode)
	r.setAgentMode(mode)
	switch mode {
	case config.AgentModePlan:
		color.Green("agent mode: plan (read-only research; produces a plan for approval before any writes)")
	case config.AgentModeGoal:
		color.Green("agent mode: goal (objective + acceptance criteria; runs autonomously until done or blocked)")
	default:
		color.Green("agent mode: normal")
	}
}

func agentModeChoices() []termui.Choice {
	return []termui.Choice{
		{Label: "normal", Description: "默认交互：直接回答并按要求执行", Value: config.AgentModeNormal},
		{Label: "plan", Description: "只读调研，先产出结构化计划，批准后再执行", Value: config.AgentModePlan},
		{Label: "goal", Description: "目标+验收条件驱动，自主多轮直到达成或受阻", Value: config.AgentModeGoal},
	}
}

// handlePlanCommand enters plan mode. With a trailing request it queues that
// request so the main loop starts planning immediately.
func (r *REPL) handlePlanCommand(args []string) {
	request := strings.TrimSpace(strings.Join(args, " "))
	r.applyAgentMode(config.AgentModePlan)
	if request == "" {
		color.White("plan mode: describe the task in your next message; I will research read-only and produce a plan for approval.")
		return
	}
	r.pendingPrompt = request
}

// handleGoalCommand sets or inspects the goal objective and enters goal mode.
func (r *REPL) handleGoalCommand(args []string) {
	if len(args) == 0 {
		r.applyAgentMode(config.AgentModeGoal)
		if tools.CurrentGoal().Objective == "" {
			color.White("usage: /goal <objective> [criteria...]")
			color.White("       /goal show | status <status> | criteria <c...> | progress <note> | clear")
			return
		}
		r.printStateTool(&tools.GoalTool{}, map[string]interface{}{"action": "show"})
		return
	}

	switch strings.ToLower(args[0]) {
	case "show":
		r.printStateTool(&tools.GoalTool{}, map[string]interface{}{"action": "show"})
		return
	case "clear":
		r.printStateTool(&tools.GoalTool{}, map[string]interface{}{"action": "clear"})
		return
	case "status":
		input := map[string]interface{}{"action": "status"}
		if len(args) > 1 {
			input["status"] = args[1]
		}
		r.printStateTool(&tools.GoalTool{}, input)
		return
	case "criteria":
		var criteria []interface{}
		for _, item := range args[1:] {
			criteria = append(criteria, item)
		}
		r.printStateTool(&tools.GoalTool{}, map[string]interface{}{"action": "add_criteria", "done_when": criteria})
		return
	case "progress":
		r.printStateTool(&tools.GoalTool{}, map[string]interface{}{"action": "progress", "note": strings.Join(args[1:], " ")})
		return
	}

	objective := strings.TrimSpace(strings.Join(args, " "))
	r.setAgentMode(config.AgentModeGoal)
	tools.SetGoal(objective, nil)
	r.syncAgentStateToSession()
	color.Green("agent mode: goal")
	color.White("objective: %s", objective)
	color.White("add acceptance criteria with /goal criteria <item>, or just send a message to begin.")
	r.pendingPrompt = objective
}

func workModeChoices() []termui.Choice {
	return []termui.Choice{
		{Label: "coding", Description: "默认代码协作、排障、文档和自动化", Value: "coding"},
		{Label: "code-audit", Description: "源码安全审计，含 Burp POC 报告", Value: "code-audit"},
		{Label: "pentest", Description: "授权渗透测试/红队打点", Value: "pentest"},
		{Label: "ai-security", Description: "LLM/RAG/MCP/Agent 安全评估", Value: "ai-security"},
		{Label: "bugfix", Description: "复现、定位、最小修复和验证", Value: "bugfix"},
		{Label: "code-review", Description: "正确性、安全、回归和测试缺口审查", Value: "code-review"},
		{Label: "incident-response", Description: "事件响应、时间线、IOC、遏制和恢复", Value: "incident-response"},
		{Label: "malware-analysis", Description: "恶意样本静态优先分析和 IOC", Value: "malware-analysis"},
		{Label: "osint", Description: "来源、线索、置信度和不确定性跟踪", Value: "osint"},
		{Label: "ctf", Description: "CTF 题目分流、求解和 writeup", Value: "ctf"},
	}
}

// cleanup 清理资源
func (r *REPL) cleanup(showMessage bool) {
	if r.closed {
		return
	}
	r.closed = true
	r.runHook(hooks.SessionEnd, hooks.Payload{"reason": "exit"})
	_ = r.sessionMgr.Save()
	if showMessage {
		color.Green("\n💾 会话已保存")
	}
}

// Close runs SessionEnd hooks and persists the session. It is safe to call more
// than once and is used by non-interactive mode.
func (r *REPL) Close() { r.cleanup(false) }

// getMaxMessages 获取最大消息数
func (r *REPL) getMaxMessages() int {
	return 50 // 默认保留最近50条消息
}

func (r *REPL) ensureHooksStarted() {
	if r.hooksStarted {
		return
	}
	r.hooksStarted = true
	r.runHook(hooks.SessionStart, hooks.Payload{"source": "startup"})
}

func (r *REPL) runHook(event string, payload hooks.Payload) hooks.Result {
	if r.hookRunner == nil {
		return hooks.Result{}
	}
	ctx := r.ctx
	if event == hooks.SessionEnd {
		ctx = context.Background()
	}
	result, err := r.hookRunner.Run(ctx, event, payload)
	if err != nil {
		color.Yellow("hook warning (%s): %v", event, err)
	}
	r.hookContext = append(r.hookContext, result.AdditionalContext...)
	for _, message := range result.SystemMessages {
		color.Yellow("hook: %s", message)
	}
	return result
}

func (r *REPL) buildSystemPrompt() string {
	prompt := buildSystemPrompt(r.config)
	if len(r.hookContext) == 0 {
		return prompt
	}
	return prompt + "\n\nContext added by hooks:\n" + strings.Join(r.hookContext, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// 辅助函数和常量
var (
	ErrInterrupted = fmt.Errorf("interrupted")
	ErrEOF         = fmt.Errorf("EOF")
)

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func formatToolInputShort(input map[string]interface{}) string {
	if input == nil {
		return "{}"
	}
	data, _ := json.Marshal(input)
	str := string(data)
	if len(str) > 200 {
		return str[:200] + "..."
	}
	return str
}

func toolChangePreview(name string, input map[string]interface{}) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write":
		filePath, _ := input["file_path"].(string)
		content, _ := input["content"].(string)
		if filePath == "" {
			return ""
		}
		return fmt.Sprintf("    write %s (%d bytes)\n    + %s\n", filePath, len(content), firstPreviewLine(content, 180))
	case "edit":
		filePath, _ := input["file_path"].(string)
		oldString, _ := input["old_string"].(string)
		newString, _ := input["new_string"].(string)
		if filePath == "" {
			return ""
		}
		return fmt.Sprintf("    edit %s\n    - %s\n    + %s\n", filePath, firstPreviewLine(oldString, 180), firstPreviewLine(newString, 180))
	default:
		return ""
	}
}

func firstPreviewLine(value string, max int) string {
	value = strings.TrimSpace(strings.Split(value, "\n")[0])
	if max > 0 && len(value) > max {
		return value[:max] + "..."
	}
	return value
}

func buildSystemPrompt(cfg *config.Config) string {
	return promptpkg.BuildSystemPrompt(cfg)
}
