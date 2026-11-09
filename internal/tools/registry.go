package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/t-rex-ai/t-rex/internal/config"
)

// Tool 工具接口
type Tool interface {
	// Name 返回工具名称
	Name() string
	// Description 返回工具描述
	Description() string
	// InputSchema 返回输入参数的 JSON Schema
	InputSchema() map[string]interface{}
	// Execute 执行工具
	Execute(ctx context.Context, input map[string]interface{}) (*ToolResult, error)
}

// ToolResult 工具执行结果
type ToolResult struct {
	Content string      `json:"content"`
	Output  interface{} `json:"output,omitempty"`
	IsError bool        `json:"is_error"`
}

// ToolDef 工具定义（与 API 格式兼容）
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// Registry 工具注册表
type Registry struct {
	tools      map[string]Tool
	mu         sync.RWMutex
	config     *config.Config
	permission PermissionChecker
}

// PermissionChecker 权限检查器接口
type PermissionChecker interface {
	CheckPermission(toolName string, input map[string]interface{}) (bool, string)
	RequestPermission(toolName string, input map[string]interface{}) (bool, error)
}

type PermissionDecision struct {
	Result  *ToolResult
	Allowed bool
	Reason  string
}

// NewRegistry 创建新的工具注册表
func NewRegistry(cfg *config.Config, permChecker PermissionChecker) *Registry {
	r := &Registry{
		tools:      make(map[string]Tool),
		config:     cfg,
		permission: permChecker,
	}
	r.registerBuiltinTools()
	return r
}

// registerBuiltinTools 注册内置工具
func (r *Registry) registerBuiltinTools() {
	// Bash 工具
	r.Register(&BashTool{config: r.config})
	r.Register(&ShellTool{BashTool{config: r.config}})

	// Read 工具
	r.Register(&ReadTool{config: r.config})

	// Write 工具
	r.Register(&WriteTool{config: r.config})

	// Edit 工具
	r.Register(&EditTool{config: r.config})

	// Glob 工具
	r.Register(&GlobTool{config: r.config})

	// Grep 工具
	r.Register(&GrepTool{config: r.config})

	r.Register(&TodoTool{})
	r.Register(&MissionTool{})
	r.Register(&GoalTool{})
	r.Register(&ScopeTool{})
	r.Register(&FindingTool{})
	r.Register(&EvidenceTool{config: r.config})
	r.Register(&AssessmentPlanTool{})
	r.Register(&SecurityCoverageTool{})
	r.Register(&SecurityCatalogTool{})
	r.Register(&SecurityTestTool{})
	r.Register(&CodeAuditCatalogTool{})
	registerInstallTools(r)
	registerMemoryAndSkillTools(r)
}

// Register 注册一个新工具
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Get 获取指定名称的工具
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, exists := r.tools[name]
	return tool, exists
}

// List 列出所有已注册的工具
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// ToAPIDefinitions 将所有工具转换为 API 定义格式（返回 api.ToolDef 切片）
func (r *Registry) ToAPIDefinitions() []ToolDef {
	toolList := r.List()
	sort.SliceStable(toolList, func(i, j int) bool {
		if toolList[i].Name() == "Bash" {
			return false
		}
		if toolList[j].Name() == "Bash" {
			return true
		}
		return toolList[i].Name() < toolList[j].Name()
	})
	definitions := make([]ToolDef, len(toolList))
	for i, tool := range toolList {
		definitions[i] = ToolDef{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		}
	}
	return definitions
}

// Execute 执行指定工具（带权限检查）
func (r *Registry) Execute(ctx context.Context, name string, input map[string]interface{}) (*ToolResult, error) {
	tool, exists := r.Get(name)
	if !exists {
		return nil, fmt.Errorf("未知工具: %s", name)
	}

	// 权限检查
	allowed, reason := r.permission.CheckPermission(name, input)
	if !allowed {
		if reason == "ask" || reason == "ask-high-risk" {
			granted, err := r.permission.RequestPermission(name, input)
			if err != nil {
				return nil, fmt.Errorf("权限请求错误: %w", err)
			}
			if !granted {
				return &ToolResult{
					Content: "用户拒绝了此操作",
					IsError: true,
				}, nil
			}
		} else if reason == "denied" {
			return &ToolResult{
				Content: "操作被拒绝（权限策略禁止）",
				IsError: true,
			}, nil
		} else if reason == "plan mode" {
			return &ToolResult{
				Content: "操作被暂停（计划模式）",
				IsError: false,
			}, nil
		}
	}

	// 设置超时上下文（默认5分钟）
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// 执行工具
	result, err := tool.Execute(ctx, input)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("执行失败: %v", err),
			IsError: true,
		}, nil
	}

	return result, nil
}

func (r *Registry) EnsureAllowed(name string, input map[string]interface{}) (PermissionDecision, error) {
	if _, exists := r.Get(name); !exists {
		return PermissionDecision{}, fmt.Errorf("unknown tool: %s", name)
	}
	allowed, reason := r.permission.CheckPermission(name, input)
	if allowed {
		return PermissionDecision{Allowed: true, Reason: reason}, nil
	}
	switch reason {
	case "ask", "ask-high-risk":
		granted, err := r.permission.RequestPermission(name, input)
		if err != nil {
			return PermissionDecision{}, fmt.Errorf("permission request failed: %w", err)
		}
		if granted {
			return PermissionDecision{Allowed: true, Reason: "granted"}, nil
		}
		return PermissionDecision{Result: &ToolResult{Content: "user denied this operation", IsError: true}, Reason: "denied by user"}, nil
	case "denied":
		return PermissionDecision{Result: &ToolResult{Content: "operation denied by permission policy", IsError: true}, Reason: reason}, nil
	case "plan mode":
		return PermissionDecision{Result: &ToolResult{Content: "operation paused by plan mode", IsError: false}, Reason: reason}, nil
	default:
		return PermissionDecision{Result: &ToolResult{Content: "operation not allowed: " + reason, IsError: true}, Reason: reason}, nil
	}
}

func (r *Registry) ExecuteAllowed(ctx context.Context, name string, input map[string]interface{}) (*ToolResult, error) {
	tool, exists := r.Get(name)
	if !exists {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	result, err := tool.Execute(ctx, input)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("execution failed: %v", err),
			IsError: true,
		}, nil
	}
	return result, nil
}

// ========== 内置工具实现 ==========

// BashTool Bash 命令执行工具
type BashTool struct {
	config *config.Config
}

func (t *BashTool) Name() string { return "Bash" }
func (t *BashTool) Description() string {
	return "Compatibility alias for Shell. Executes a command with the native shell for the current OS; prefer the Shell tool in new calls."
}
func (t *BashTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Command to execute in the native shell for the current OS.",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in milliseconds. Default is 120000.",
			},
			"cwd": map[string]interface{}{
				"type":        "string",
				"description": "Working directory.",
			},
			"max_output_bytes": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum command output bytes returned to the model. Full output is saved under .cache/tool-runs when truncated.",
			},
		},
		"required": []string{"command"},
	}
}

type ShellTool struct {
	BashTool
}

func (t *ShellTool) Name() string { return "Shell" }
func (t *ShellTool) Description() string {
	return "Execute a command with the native shell for the current OS. Windows uses PowerShell/cmd, Linux uses bash/sh, and macOS uses bash/zsh/sh. Output streams live and the complete result is captured."
}
func (t *ShellTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Command to execute in the native shell for the current OS.",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in milliseconds. Default is 120000.",
			},
			"cwd": map[string]interface{}{
				"type":        "string",
				"description": "Working directory.",
			},
		},
		"required": []string{"command"},
	}
}

func (t *BashTool) Execute(ctx context.Context, input map[string]interface{}) (*ToolResult, error) {
	cmdStr, _ := input["command"].(string)
	if cmdStr == "" {
		return &ToolResult{Content: "missing command parameter", IsError: true}, nil
	}

	// 获取工作目录
	cwd, _ := input["cwd"].(string)
	resolvedCwd, err := validateShellWorkspace(t.config, cwd, cmdStr)
	if err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}

	// 获取超时
	timeout := 120 * time.Second
	if to, ok := input["timeout"].(float64); ok {
		timeout = time.Duration(int(to)) * time.Millisecond
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell, shellArgs := shellCommand(cmdStr)
	cmd := exec.CommandContext(execCtx, shell, shellArgs...)
	cmd.Dir = resolvedCwd

	var output bytes.Buffer
	if shouldStreamCommandOutput(t.config) {
		fmt.Fprintln(os.Stdout, "\n--- command output ---")
		cmd.Stdout = &lockedWriter{w: io.MultiWriter(&output, newLinePrefixWriter(os.Stdout, "  ", nil))}
		cmd.Stderr = &lockedWriter{w: io.MultiWriter(&output, newLinePrefixWriter(os.Stderr, "  ", color.New(color.FgRed)))}
	} else {
		cmd.Stdout = &output
		cmd.Stderr = &output
	}

	err = cmd.Run()
	if shouldStreamCommandOutput(t.config) {
		fmt.Fprintln(os.Stdout, "--- end command output ---")
	}
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if execCtx.Err() == context.DeadlineExceeded {
			return &ToolResult{
				Content: fmt.Sprintf("command timed out after %v\ncommand: %s", timeout, cmdStr),
				IsError: true,
			}, nil
		} else {
			return &ToolResult{
				Content: fmt.Sprintf("command execution failed: %v\ncommand: %s", err, cmdStr),
				IsError: true,
			}, nil
		}
	}

	fullOutput := output.String()
	modelOutput := fullOutput
	fullOutputPath := ""
	maxOutputBytes := toolOutputMaxBytes(t.config, input)
	if len(fullOutput) > maxOutputBytes {
		if path, writeErr := writeFullToolOutput(t.config, "shell", fullOutput); writeErr == nil {
			fullOutputPath = path
		}
		modelOutput = abbreviateToolOutput(fullOutput, maxOutputBytes)
	}

	result := map[string]interface{}{
		"output":              modelOutput,
		"exitCode":            exitCode,
		"outputBytes":         len(fullOutput),
		"outputTruncated":     len(modelOutput) < len(fullOutput),
		"fullOutputPath":      fullOutputPath,
		"maxOutputBytesShown": maxOutputBytes,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	toolResult := &ToolResult{
		Content: string(resultJSON),
		Output:  result,
	}
	writeAuditEvent(t.Name(), t.config, input, toolResult, resolvedCwd)
	return toolResult, nil
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

// ReadTool 文件读取工具
type linePrefixWriter struct {
	w           io.Writer
	prefix      string
	color       *color.Color
	atLineStart bool
}

func newLinePrefixWriter(w io.Writer, prefix string, c *color.Color) *linePrefixWriter {
	return &linePrefixWriter{w: w, prefix: prefix, color: c, atLineStart: true}
}

func (w *linePrefixWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if w.atLineStart {
			if _, err := io.WriteString(w.w, w.prefix); err != nil {
				return 0, err
			}
			w.atLineStart = false
		}
		if w.color != nil {
			if _, err := w.color.Fprintf(w.w, "%c", b); err != nil {
				return 0, err
			}
		} else if _, err := w.w.Write([]byte{b}); err != nil {
			return 0, err
		}
		if b == '\n' {
			w.atLineStart = true
		}
	}
	return len(p), nil
}

func shellCommand(command string) (string, []string) {
	return shellCommandForOS(runtime.GOOS, command, exec.LookPath)
}

func shouldStreamCommandOutput(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.UI.ToolOutput), "full")
}

func toolOutputMaxBytes(cfg *config.Config, input map[string]interface{}) int {
	defaultMax := 64 * 1024
	if cfg != nil && cfg.Project.ToolOutputMaxBytes > 0 {
		defaultMax = cfg.Project.ToolOutputMaxBytes
	}
	if input != nil {
		if v, ok := input["max_output_bytes"].(float64); ok && v > 0 {
			return int(v)
		}
		if v, ok := input["max_output_bytes"].(int); ok && v > 0 {
			return v
		}
	}
	if defaultMax < 1024 {
		return 1024
	}
	return defaultMax
}

func abbreviateToolOutput(content string, maxBytes int) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}
	if maxBytes < 512 {
		maxBytes = 512
	}
	marker := fmt.Sprintf("\n\n[output truncated: omitted %d bytes]\n\n", len(content)-maxBytes)
	keep := maxBytes - len(marker)
	if keep <= 0 {
		return marker
	}
	head := keep * 2 / 3
	tail := keep - head
	return content[:head] + marker + content[len(content)-tail:]
}

func writeFullToolOutput(cfg *config.Config, toolName string, content string) (string, error) {
	root, err := workspaceRoot(cfg)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, ".cache", "tool-runs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		name = "tool"
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-output.txt", time.Now().Format("20060102-150405.000"), name))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func shellCommandForOS(goos string, command string, lookPath func(string) (string, error)) (string, []string) {
	switch goos {
	case "windows":
		if path, err := lookPath("pwsh"); err == nil {
			return path, []string{"-NoProfile", "-NonInteractive", "-Command", command}
		}
		if path, err := lookPath("powershell"); err == nil {
			return path, []string{"-NoProfile", "-NonInteractive", "-Command", command}
		}
		if path, err := lookPath("cmd"); err == nil {
			return path, []string{"/C", command}
		}
		return "cmd", []string{"/C", command}
	case "darwin":
		if path, err := lookPath("bash"); err == nil {
			return path, []string{"-lc", command}
		}
		if path, err := lookPath("zsh"); err == nil {
			return path, []string{"-lc", command}
		}
		return "/bin/sh", []string{"-c", command}
	default:
		if path, err := lookPath("bash"); err == nil {
			return path, []string{"-lc", command}
		}
		return "/bin/sh", []string{"-c", command}
	}
}

type ReadTool struct {
	config *config.Config
}

func (t *ReadTool) Name() string { return "Read" }
func (t *ReadTool) Description() string {
	return "读取文件的内容。可以指定偏移量和限制来读取部分内容。返回原始文件内容，包含行号。用于查看源代码、配置文件等。"
}
func (t *ReadTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "要读取的文件的绝对或相对路径",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "从第几行开始读取（1-indexed），默认从第一行开始",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "要读取的行数，默认读取全部",
			},
		},
		"required": []string{"file_path"},
	}
}

func (t *ReadTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	filePath, _ := input["file_path"].(string)
	if filePath == "" {
		return &ToolResult{Content: "缺少 file_path 参数", IsError: true}, nil
	}

	// 如果提供了相对路径且存在项目路径，则拼接
	filePath, err := resolveWorkspacePath(t.config, filePath)
	if err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}
	data, meta, err := readFileWithLineNumbers(t.config, filePath, input)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("读取文件失败: %v", err),
			IsError: true,
		}, nil
	}

	output := map[string]interface{}{"content": data}
	for k, v := range meta {
		output[k] = v
	}
	return &ToolResult{Content: data, Output: output}, nil
}

// readFileWithLineNumbers reads text with line numbers. Oversized files return
// a bounded partial view instead of failing, so agents can continue by paging.
func readFileWithLineNumbers(cfg *config.Config, filePath string, input map[string]interface{}) (string, map[string]interface{}, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", nil, fmt.Errorf("无法检查文件大小 '%s': %w", filePath, err)
	}

	maxSize := maxFileSize(cfg)
	oversized := maxSize > 0 && info.Size() > maxSize
	offset := positiveIntInput(input["offset"])
	if offset == 0 {
		offset = 1
	}
	limit := positiveIntInput(input["limit"])
	limitProvided := limit > 0
	if oversized && !limitProvided {
		limit = 400
	}

	if oversized {
		return readPartialFileWithLineNumbers(filePath, offset, limit, info.Size(), maxSize, limitProvided)
	}

	data, err := readFileContent(filePath)
	if err != nil {
		return "", nil, err
	}
	return formatLines(string(data), offset, limit, false, info.Size(), maxSize, limitProvided)
}

func formatLines(content string, offset int, limit int, oversized bool, fileSize int64, maxSize int64, limitProvided bool) (string, map[string]interface{}, error) {
	lines := strings.Split(content, "\n")
	if offset > len(lines) {
		return "", nil, fmt.Errorf("偏移量超出文件范围（文件共 %d 行）", len(lines))
	}
	end := len(lines)
	if limit > 0 && offset-1+limit < end {
		end = offset - 1 + limit
	}

	var result strings.Builder
	if oversized {
		writePartialReadNotice(&result, fileSize, maxSize, offset, end, limitProvided)
	}
	for i := offset - 1; i < end; i++ {
		fmt.Fprintf(&result, "%6d:%s\n", i+1, lines[i])
	}

	return result.String(), readMeta(fileSize, maxSize, oversized, offset, end, end < len(lines)), nil
}

func readPartialFileWithLineNumbers(filePath string, offset int, limit int, fileSize int64, maxSize int64, limitProvided bool) (string, map[string]interface{}, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", nil, fmt.Errorf("无法读取文件 '%s': %w", filePath, err)
	}
	defer file.Close()

	var result strings.Builder
	writePartialReadNotice(&result, fileSize, maxSize, offset, offset+limit-1, limitProvided)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNum := 0
	shown := 0
	outputBudget := int(maxSize)
	if outputBudget < 8*1024 {
		outputBudget = 8 * 1024
	}
	if outputBudget <= 0 {
		outputBudget = 100 * 1024
	}
	truncated := false
	lastLine := offset - 1
	for scanner.Scan() {
		lineNum++
		if lineNum < offset {
			continue
		}
		if limit > 0 && shown >= limit {
			truncated = true
			break
		}
		line := fmt.Sprintf("%6d:%s\n", lineNum, scanner.Text())
		if result.Len()+len(line) > outputBudget {
			truncated = true
			break
		}
		result.WriteString(line)
		shown++
		lastLine = lineNum
	}
	if err := scanner.Err(); err != nil {
		return "", nil, err
	}
	if shown == 0 && lineNum < offset {
		return "", nil, fmt.Errorf("偏移量超出文件范围（文件共 %d 行）", lineNum)
	}
	if truncated {
		fmt.Fprintf(&result, "\n[partial read stopped at line %d; continue with offset=%d and limit=%d]\n", lastLine, lastLine+1, limit)
	}
	return result.String(), readMeta(fileSize, maxSize, true, offset, lastLine, truncated), nil
}

func writePartialReadNotice(result *strings.Builder, fileSize int64, maxSize int64, offset int, end int, limitProvided bool) {
	reason := "default partial view"
	if limitProvided {
		reason = "requested range"
	}
	fmt.Fprintf(result, "[partial read: file size %d bytes exceeds MaxFileSize %d bytes; showing %s from line %d", fileSize, maxSize, reason, offset)
	if end >= offset {
		fmt.Fprintf(result, " to %d", end)
	}
	result.WriteString(". Use offset and limit to page through the file.]\n\n")
}

func readMeta(fileSize int64, maxSize int64, partial bool, startLine int, endLine int, truncated bool) map[string]interface{} {
	return map[string]interface{}{
		"fileSize":     fileSize,
		"maxFileSize":  maxSize,
		"partial":      partial,
		"startLine":    startLine,
		"endLine":      endLine,
		"truncated":    truncated,
		"nextOffset":   endLine + 1,
		"nextPageHint": fmt.Sprintf("Read again with offset=%d and limit=<lines>", endLine+1),
	}
}

func positiveIntInput(value interface{}) int {
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &parsed); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

// WriteTool 文件写入工具
type WriteTool struct {
	config *config.Config
}

func (t *WriteTool) Name() string { return "Write" }
func (t *WriteTool) Description() string {
	return "将完整内容写入到文件。如果文件不存在则创建新文件，如果存在则完全覆盖原内容。适用于创建新文件或完全重写文件。"
}
func (t *WriteTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "要写入的文件的绝对或相对路径",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "要写入的完整内容",
			},
		},
		"required": []string{"file_path", "content"},
	}
}

func (t *WriteTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	filePath, _ := input["file_path"].(string)
	content, _ := input["content"].(string)

	if filePath == "" {
		return &ToolResult{Content: missingToolInputMessage("Write", "file_path", input), IsError: true}, nil
	}
	if content == "" {
		return &ToolResult{Content: missingToolInputMessage("Write", "content", input), IsError: true}, nil
	}

	// 路径处理
	filePath, err := resolveWorkspacePath(t.config, filePath)
	if err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if maxSize := maxFileSize(t.config); maxSize > 0 && int64(len(content)) > maxSize {
		return &ToolResult{Content: fmt.Sprintf("content size %d bytes exceeds MaxFileSize %d bytes", len(content), maxSize), IsError: true}, nil
	}

	// 写入文件
	if err := writeFileContent(filePath, content); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("写入文件失败: %v", err),
			IsError: true,
		}, nil
	}

	toolResult := &ToolResult{
		Content: fmt.Sprintf("成功写入文件: %s (%d 字节)", filePath, len(content)),
		Output:  map[string]string{"file_path": filePath, "size": fmt.Sprintf("%d", len(content))},
	}
	writeAuditEvent(t.Name(), t.config, input, toolResult, "")
	return toolResult, nil
}

func missingToolInputMessage(toolName, field string, input map[string]interface{}) string {
	message := fmt.Sprintf("%s 缺少 %s 参数", toolName, field)
	if input == nil || len(input) == 0 {
		return message + "；模型发起了空工具调用。请重试，或把大文件/长报告拆成多个较小片段写入。"
	}
	if parseErr := strings.TrimSpace(fmt.Sprint(input["parse_error"])); parseErr != "" && parseErr != "<nil>" {
		return message + "；工具参数 JSON 解析失败，可能是长内容在流式输出中被截断: " + parseErr
	}
	if raw := strings.TrimSpace(fmt.Sprint(input["raw"])); raw != "" && raw != "<nil>" {
		if len(raw) > 160 {
			raw = raw[:160] + "..."
		}
		return message + "；收到未解析的 raw 参数: " + raw
	}
	return message
}

// EditTool 编辑工具（基于搜索替换）
type EditTool struct {
	config *config.Config
}

func (t *EditTool) Name() string { return "Edit" }
func (t *EditTool) Description() string {
	return "对现有文件进行精确编辑。通过 old_string 和 new_string 进行替换操作。保留原有格式和未修改的部分。适用于小范围、精确的代码修改。"
}
func (t *EditTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "要编辑的文件的绝对或相对路径",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "要被替换的旧字符串（必须精确匹配）",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "替换后的新字符串",
			},
			"replace_all": map[string]interface{}{
				"type":        "boolean",
				"description": "Replace all matches; defaults to false and requires a unique old_string.",
			},
		},
		"required": []string{"file_path", "old_string", "new_string"},
	}
}

func (t *EditTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	filePath, _ := input["file_path"].(string)
	oldStr, _ := input["old_string"].(string)
	newStr, _ := input["new_string"].(string)

	if filePath == "" || oldStr == "" {
		return &ToolResult{Content: "缺少必要参数", IsError: true}, nil
	}

	// 路径处理
	filePath, err := resolveWorkspacePath(t.config, filePath)
	if err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}

	// 读取文件
	if err := validateFileSize(t.config, filePath); err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}
	content, err := readFileContent(filePath)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("读取文件失败: %v", err),
			IsError: true,
		}, nil
	}

	// Replace exactly one occurrence by default. Bulk replacement must be explicit.
	contentString := string(content)
	occurences := strings.Count(contentString, oldStr)
	if occurences == 0 {
		return &ToolResult{
			Content: fmt.Sprintf("未找到匹配的字符串进行替换"),
			IsError: true,
		}, nil
	}

	replaceAll := boolInput(input["replace_all"])
	if occurences > 1 && !replaceAll {
		return &ToolResult{
			Content: fmt.Sprintf("old_string matched %d occurrences; set replace_all=true to replace all", occurences),
			IsError: true,
		}, nil
	}

	newContent := strings.Replace(contentString, oldStr, newStr, 1)
	if replaceAll {
		newContent = strings.ReplaceAll(contentString, oldStr, newStr)
	}

	// 写回文件
	if err := writeFileContent(filePath, newContent); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("写入文件失败: %v", err),
			IsError: true,
		}, nil
	}

	toolResult := &ToolResult{
		Content: fmt.Sprintf("成功编辑文件: %s", filePath),
		Output: map[string]string{
			"file_path": filePath,
			"changes":   fmt.Sprintf("%d replacement(s) made", replacementCount(occurences, replaceAll)),
		},
	}
	writeAuditEvent(t.Name(), t.config, input, toolResult, "")
	return toolResult, nil
}

func replacementCount(matches int, replaceAll bool) int {
	if replaceAll {
		return matches
	}
	return 1
}

func boolInput(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

// GlobTool 文件名模式匹配工具
type GlobTool struct {
	config *config.Config
}

func (t *GlobTool) Name() string { return "Glob" }
func (t *GlobTool) Description() string {
	return "使用 glob 模式查找匹配的文件路径。支持 **/* 递归匹配、*.go 等通配符。用于发现项目中的特定类型文件。"
}
func (t *GlobTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "glob 模式，如 '**/*.go', 'src/**/*.ts'",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "搜索根目录，默认为项目目录",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *GlobTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return &ToolResult{Content: "缺少 pattern 参数", IsError: true}, nil
	}

	searchPath := t.config.Project.Path
	if p, ok := input["path"].(string); ok && p != "" {
		searchPath = p
	}
	if strings.TrimSpace(searchPath) == "" {
		searchPath = "."
	}
	searchPath, err := resolveWorkspacePath(t.config, searchPath)
	if err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}

	matches, err := globSearchWithPolicy(t.config, searchPath, pattern)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("搜索失败: %v", err),
			IsError: true,
		}, nil
	}

	result := map[string]interface{}{
		"pattern": pattern,
		"files":   matches,
		"count":   len(matches),
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: string(resultJSON), Output: result}, nil
}

// GrepTool 正则搜索工具
type GrepTool struct {
	config *config.Config
}

func (t *GrepTool) Name() string { return "Grep" }
func (t *GrepTool) Description() string {
	return "使用正则表达式在文件中搜索内容。返回匹配的文件路径、行号和匹配行内容。用于代码搜索、查找函数定义、定位错误等。"
}
func (t *GrepTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "正则表达式模式",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "搜索目录，默认为项目目录",
			},
			"glob": map[string]interface{}{
				"type":        "string",
				"description": "文件过滤 glob 模式（可选）",
			},
			"include_content": map[string]interface{}{
				"type":        "boolean",
				"description": "是否在结果中包含匹配行的内容",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *GrepTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return &ToolResult{Content: "缺少 pattern 参数", IsError: true}, nil
	}

	searchPath := t.config.Project.Path
	if p, ok := input["path"].(string); ok && p != "" {
		searchPath = p
	}
	if strings.TrimSpace(searchPath) == "" {
		searchPath = "."
	}
	searchPath, err := resolveWorkspacePath(t.config, searchPath)
	if err != nil {
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}

	globPattern, _ := input["glob"].(string)
	includeContent := true
	if ic, ok := input["include_content"].(bool); ok {
		includeContent = ic
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("无效的正则表达式: %v", err),
			IsError: true,
		}, nil
	}

	results, err := grepSearchWithPolicy(t.config, searchPath, re, globPattern, includeContent)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("搜索失败: %v", err),
			IsError: true,
		}, nil
	}

	result := map[string]interface{}{
		"pattern": pattern,
		"results": results,
		"count":   len(results),
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: string(resultJSON), Output: result}, nil
}
