package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/t-rex-ai/t-rex/internal/api"
	"github.com/t-rex-ai/t-rex/internal/config"
)

// Session 会话结构
type Session struct {
	ID         string            `json:"id"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
	Title      string            `json:"title,omitempty"`
	Path       string            `json:"path,omitempty"`
	Model      string            `json:"model"`
	Messages   []SessionMessage  `json:"messages"`
	TokenUsage TokenUsage        `json:"tokenUsage"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	AgentState json.RawMessage   `json:"agentState,omitempty"`
}

// SessionMessage 会话消息
type SessionMessage struct {
	ID        string             `json:"id"`
	Role      string             `json:"role"` // "user", "assistant"
	Content   string             `json:"content"`
	Blocks    []api.ContentBlock `json:"blocks,omitempty"`
	Internal  bool               `json:"internal,omitempty"`
	Timestamp time.Time          `json:"timestamp"`
	Tools     []api.ToolUse      `json:"tools,omitempty"`
	Usage     *api.Usage         `json:"usage,omitempty"`
}

// TokenUsage Token 使用统计
type TokenUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// Manager 会话管理器
type Manager struct {
	sessions   map[string]*Session
	current    *Session
	mu         sync.RWMutex
	config     *config.SessionConfig
	storageDir string
	autoSaveCh chan struct{}
}

// NewManager 创建会话管理器
func NewManager(cfg *config.SessionConfig) (*Manager, error) {
	storageDir := config.GetSessionDir()
	if cfg.StoragePath != "" {
		storageDir = cfg.StoragePath
	}

	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("创建会话存储目录失败: %w", err)
	}

	m := &Manager{
		sessions:   make(map[string]*Session),
		config:     cfg,
		storageDir: storageDir,
		autoSaveCh: make(chan struct{}, 1),
	}

	// 启动自动保存协程
	go m.autoSaveLoop()

	return m, nil
}

// Create 创建新会话
func (m *Manager) Create(path string, model string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &Session{
		ID:         uuid.New().String(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Path:       path,
		Model:      model,
		Messages:   make([]SessionMessage, 0),
		TokenUsage: TokenUsage{},
		Metadata:   make(map[string]string),
	}

	m.sessions[session.ID] = session
	m.current = session

	// 自动保存
	triggerAutoSave(m.autoSaveCh)

	return session, nil
}

// Get 获取当前会话
func (m *Manager) Get() *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// GetByID 根据 ID 获取会话
func (m *Manager) GetByID(id string) (*Session, error) {
	m.mu.RLock()
	session, exists := m.sessions[id]
	m.mu.RUnlock()

	if exists {
		return session, nil
	}

	// 尝试从磁盘加载
	return m.loadFromDisk(id)
}

// SetCurrent 设置当前活动会话
func (m *Manager) SetCurrent(session *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = session
}

func (m *Manager) SetAgentStateJSON(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return
	}
	if len(data) == 0 || string(data) == "null" {
		m.current.AgentState = nil
	} else {
		m.current.AgentState = append([]byte(nil), data...)
	}
	m.current.UpdatedAt = time.Now()
	triggerAutoSave(m.autoSaveCh)
}

func (m *Manager) AgentStateJSON() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil || len(m.current.AgentState) == 0 {
		return nil
	}
	return append([]byte(nil), m.current.AgentState...)
}

// AddMessage 向当前会话添加消息
func (m *Manager) AddMessage(role, content string, tools []api.ToolUse, usage *api.Usage) error {
	return m.addMessage(SessionMessage{
		Role:    role,
		Content: content,
		Tools:   tools,
		Usage:   usage,
	})
}

// AddStructuredMessage stores the complete API content blocks. This preserves
// tool_use/tool_result history across resume instead of reducing a turn to text.
func (m *Manager) AddStructuredMessage(role string, blocks []api.ContentBlock, internal bool, usage *api.Usage) error {
	var text strings.Builder
	var toolUses []api.ToolUse
	for _, block := range blocks {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			input, _ := block.Input.(map[string]interface{})
			toolUses = append(toolUses, api.ToolUse{ID: block.ID, Name: block.Name, Input: input})
			if text.Len() == 0 {
				fmt.Fprintf(&text, "[tool: %s]", block.Name)
			}
		case "tool_result":
			if text.Len() == 0 {
				text.WriteString("[tool result]")
			}
		}
	}

	return m.addMessage(SessionMessage{
		Role:     role,
		Content:  text.String(),
		Blocks:   append([]api.ContentBlock(nil), blocks...),
		Internal: internal,
		Tools:    toolUses,
		Usage:    usage,
	})
}

func (m *Manager) addMessage(msg SessionMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current == nil {
		return fmt.Errorf("没有活动的会话")
	}

	msg.ID = uuid.New().String()
	msg.Timestamp = time.Now()

	m.current.Messages = append(m.current.Messages, msg)
	m.current.UpdatedAt = time.Now()

	// 更新 token 使用统计
	if msg.Usage != nil {
		m.current.TokenUsage.InputTokens += msg.Usage.InputTokens
		m.current.TokenUsage.OutputTokens += msg.Usage.OutputTokens
	}

	// 限制历史大小
	if m.config.MaxHistorySize > 0 && len(m.current.Messages) > m.config.MaxHistorySize {
		m.current.Messages = trimAtTurnBoundary(m.current.Messages, m.config.MaxHistorySize)
	}

	// 触发自动保存
	triggerAutoSave(m.autoSaveCh)

	return nil
}

// GetMessagesForAPI 获取适合发送给 API 的消息格式
func (m *Manager) GetMessagesForAPI(limit int) []api.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.current == nil {
		return []api.Message{}
	}

	messages := make([]api.Message, 0, len(m.current.Messages))

	start := 0
	if limit > 0 && len(m.current.Messages) > limit {
		start = len(m.current.Messages) - limit
		start = nearestTurnBoundary(m.current.Messages, start)
	}

	for i := start; i < len(m.current.Messages); i++ {
		msg := m.current.Messages[i]
		content := interface{}(msg.Content)
		if len(msg.Blocks) > 0 {
			content = msg.Blocks
		}
		apiMsg := api.Message{
			Role:    msg.Role,
			Content: content,
		}
		messages = append(messages, apiMsg)
	}

	return messages
}

// Compact trims history without leaving a dangling tool_result at the start.
func (m *Manager) Compact(maxMessages int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || maxMessages <= 0 || len(m.current.Messages) <= maxMessages {
		if m.current == nil {
			return 0
		}
		return len(m.current.Messages)
	}
	m.current.Messages = trimAtTurnBoundary(m.current.Messages, maxMessages)
	m.current.UpdatedAt = time.Now()
	triggerAutoSave(m.autoSaveCh)
	return len(m.current.Messages)
}

func (m *Manager) CompactWithSummary(maxMessages int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || maxMessages <= 0 || len(m.current.Messages) <= maxMessages {
		if m.current == nil {
			return 0
		}
		return len(m.current.Messages)
	}
	summary := summarizeMessagesForCompact(m.current.Messages)
	kept := trimAtTurnBoundary(m.current.Messages, maxMessages)
	m.current.Messages = append([]SessionMessage{{
		ID:        uuid.New().String(),
		Role:      "user",
		Content:   summary,
		Internal:  true,
		Timestamp: time.Now(),
	}}, kept...)
	m.current.UpdatedAt = time.Now()
	triggerAutoSave(m.autoSaveCh)
	return len(m.current.Messages)
}

func summarizeMessagesForCompact(messages []SessionMessage) string {
	var goals []string
	var completed []string
	var tools []string
	var files []string
	var failures []string
	toolNames := make(map[string]string)
	structured := make(map[string]string)
	var recentResults []string

	for _, msg := range messages {
		for _, block := range msg.Blocks {
			if block.Type == "tool_use" && block.ID != "" {
				toolNames[block.ID] = block.Name
			}
		}
	}
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		if text != "" && msg.Role == "user" && !msg.Internal && len(goals) < 5 {
			goals = append(goals, oneLine(text, 180))
		}
		if text != "" && msg.Role == "assistant" && len(completed) < 6 {
			completed = append(completed, oneLine(text, 180))
		}
		for _, block := range msg.Blocks {
			switch block.Type {
			case "tool_use":
				tools = appendUniqueLimit(tools, block.Name, 12)
				if input, ok := block.Input.(map[string]interface{}); ok {
					for _, key := range []string{"file_path", "path", "cwd"} {
						if v, ok := input[key]; ok {
							files = appendUniqueLimit(files, fmt.Sprint(v), 12)
						}
					}
				}
			case "tool_result":
				toolName := toolNames[block.ToolUseID]
				result := oneLine(block.Content, 320)
				if result != "" && toolName != "" {
					recentResults = append(recentResults, fmt.Sprintf("%s: %s", toolName, result))
				}
				if isStructuredStateTool(toolName) && result != "" {
					structured[strings.ToLower(toolName)] = oneLine(block.Content, 1000)
				}
				if block.IsError {
					failures = appendUniqueLimit(failures, oneLine(block.Content, 180), 8)
				}
			}
		}
	}
	if len(recentResults) > 8 {
		recentResults = recentResults[len(recentResults)-8:]
	}
	var b strings.Builder
	b.WriteString("Compressed conversation summary for continuity.\n")
	if len(structured) > 0 {
		b.WriteString("\nStructured state (authoritative):\n")
		for _, item := range []struct {
			key   string
			label string
		}{
			{key: "mission", label: "Mission"},
			{key: "todo", label: "Todo"},
			{key: "finding", label: "Finding"},
			{key: "evidence", label: "Evidence"},
			{key: "assessmentplan", label: "AssessmentPlan"},
			{key: "securitycoverage", label: "SecurityCoverage"},
			{key: "securitytest", label: "SecurityTest"},
		} {
			if value := structured[item.key]; value != "" {
				fmt.Fprintf(&b, "%s: %s\n", item.label, value)
			}
		}
	}
	writeSummaryList(&b, "User goals", goals)
	writeSummaryList(&b, "Completed/observed", completed)
	writeSummaryList(&b, "Tools used", tools)
	writeSummaryList(&b, "Key files/paths", files)
	writeSummaryList(&b, "Recent key tool results", recentResults)
	writeSummaryList(&b, "Failures/errors", failures)
	b.WriteString("Continue from the remaining recent messages and this summary.")
	return b.String()
}

func isStructuredStateTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "mission", "todo", "finding", "evidence", "assessmentplan", "securitycoverage", "securitytest":
		return true
	default:
		return false
	}
}

func writeSummaryList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
}

func appendUniqueLimit(items []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" || len(items) >= limit {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func oneLine(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if max > 0 && len(text) > max {
		return text[:max] + "..."
	}
	return text
}

func trimAtTurnBoundary(messages []SessionMessage, maxMessages int) []SessionMessage {
	start := len(messages) - maxMessages
	boundary := nearestTurnBoundary(messages, start)
	if boundary < 0 || boundary >= len(messages) {
		// A single active tool turn may exceed the limit. Keep it intact rather
		// than persisting an invalid API transcript.
		return messages
	}
	return messages[boundary:]
}

func nearestTurnBoundary(messages []SessionMessage, start int) int {
	for i := start; i < len(messages); i++ {
		if messages[i].Role == "user" && !messages[i].Internal {
			return i
		}
	}
	for i := start - 1; i >= 0; i-- {
		if messages[i].Role == "user" && !messages[i].Internal {
			return i
		}
	}
	return -1
}

// ListSessions 列出所有历史会话
func (m *Manager) ListSessions() ([]*SessionSummary, error) {
	entries, err := os.ReadDir(m.storageDir)
	if err != nil {
		return nil, err
	}

	var summaries []*SessionSummary
	for _, entry := range entries {
		if entry.IsDir() || !HasExtension(entry.Name(), ".json") {
			continue
		}

		session, err := m.loadFromDisk(stripExtension(entry.Name()))
		if err != nil {
			continue
		}

		summaries = append(summaries, &SessionSummary{
			ID:           session.ID,
			CreatedAt:    session.CreatedAt,
			UpdatedAt:    session.UpdatedAt,
			MessageCount: len(session.Messages),
			Title:        session.Title,
			Path:         session.Path,
		})
	}

	// 按更新时间倒序排序
	sortSummaries(summaries)

	return summaries, nil
}

// DeleteSession 删除会话
func (m *Manager) DeleteSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, id)

	// 删除磁盘文件
	filePath := filepath.Join(m.storageDir, id+".json")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// Save 手动保存当前会话
func (m *Manager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.current == nil {
		return nil
	}

	return saveToDisk(m.storageDir, m.current)
}

// Close 关闭会话管理器
func (m *Manager) Close() error {
	// 最终保存
	return m.Save()
}

// autoSaveLoop 自动保存循环
func (m *Manager) autoSaveLoop() {
	ticker := time.NewTicker(time.Duration(m.config.AutoSaveInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_ = m.Save()
		case <-m.autoSaveCh:
			time.Sleep(100 * time.Millisecond) // 防抖动
			_ = m.Save()
		}
	}
}

// SessionSummary 会话摘要（用于列表显示）
type SessionSummary struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	MessageCount int       `json:"messageCount"`
	Title        string    `json:"title,omitempty"`
	Path         string    `json:"path,omitempty"`
}

// 辅助函数
func triggerAutoSave(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func HasExtension(name string, ext string) bool {
	return len(name) >= len(ext) && name[len(name)-len(ext):] == ext
}

func stripExtension(name string) string {
	if idx := len(name) - len(".json"); idx > 0 {
		return name[:idx]
	}
	return name
}

func sortSummaries(s []*SessionSummary) {
	// 简单的冒泡排序按更新时间倒序
	for i := 0; i < len(s)-1; i++ {
		for j := 0; j < len(s)-i-1; j++ {
			if s[j].UpdatedAt.Before(s[j+1].UpdatedAt) {
				s[j], s[j+1] = s[j+1], s[j]
			}
		}
	}
}
