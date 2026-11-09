package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================================
// Provider 类型定义
// ============================================================================

// ProviderType 支持的 provider 类型
type ProviderType string

const (
	ProviderAnthropic        ProviderType = "anthropic"
	ProviderOpenAICompatible ProviderType = "openai-compatible"
)

// ParseProvider 解析 provider 字符串
func ParseProvider(s string) ProviderType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "anthropic", "claude":
		return ProviderAnthropic
	default:
		return ProviderOpenAICompatible
	}
}

// ============================================================================
// OpenAI 兼容格式的结构体
// ============================================================================

// OAIRequest OpenAI 兼容请求
type OAIRequest struct {
	Model         string       `json:"model"`
	MaxTokens     int          `json:"max_tokens"`
	Messages      []OAIMessage `json:"messages"`
	Temperature   float64      `json:"temperature,omitempty"`
	TopP          float64      `json:"top_p,omitempty"`
	Tools         []OAIToolDef `json:"tools,omitempty"`
	ToolChoice    interface{}  `json:"tool_choice,omitempty"`
	StopSequences []string     `json:"stop,omitempty"`
	Stream        bool         `json:"stream,omitempty"`
	StreamOptions interface{}  `json:"stream_options,omitempty"`
}

// OAIMessage OpenAI 消息
type OAIMessage struct {
	Role       string        `json:"role"`
	Content    interface{}   `json:"content,omitempty"`      // string or []OAIContentPart
	ToolCalls  []OAIToolCall `json:"tool_calls,omitempty"`   // assistant tool calls
	ToolCallID string        `json:"tool_call_id,omitempty"` // tool result
}

// OAIContentPart OpenAI 多内容块
type OAIContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// OAIToolCall OpenAI 工具调用
type OAIToolCall struct {
	Index    int         `json:"index,omitempty"`
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function OAIFunction `json:"function"`
}

// OAIFunction OpenAI 函数定义
type OAIFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串
}

// OAIToolDef OpenAI 工具定义
type OAIToolDef struct {
	Type     string         `json:"type"`
	Function OAIFunctionDef `json:"function"`
}

// OAIFunctionDef OpenAI 函数定义元数据
type OAIFunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// OAIResponse OpenAI 响应
type OAIResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Choices []OAIChoice `json:"choices"`
	Usage   OAIUsage    `json:"usage"`
	Model   string      `json:"model"`
}

// OAIChoice OpenAI 选择
type OAIChoice struct {
	Index        int        `json:"index"`
	Message      OAIMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
	Delta        *OAIDelta  `json:"delta,omitempty"` // 流式
}

// OAIDelta OpenAI 增量 (流式)
type OAIDelta struct {
	Role      string        `json:"role,omitempty"`
	Content   string        `json:"content,omitempty"`
	ToolCalls []OAIToolCall `json:"tool_calls,omitempty"`
}

// OAIUsage OpenAI 用量
type OAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OAIStreamChunk OpenAI 流式块
type OAIStreamChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Model   string      `json:"model"`
	Choices []OAIChoice `json:"choices"`
	Usage   *OAIUsage   `json:"usage,omitempty"`
}

// ============================================================================
// 请求转换: Anthropic → OpenAI
// ============================================================================

// ConvertRequestToOpenAI 将 Anthropic 格式请求转换为 OpenAI 兼容格式
func ConvertRequestToOpenAI(req *APIRequest, stream bool) (*OAIRequest, error) {
	oai := &OAIRequest{
		Model:         req.Model,
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.StopSequences,
		Stream:        stream,
	}
	if stream {
		oai.StreamOptions = map[string]bool{"include_usage": true}
	}

	// 转换 messages
	for _, msg := range req.Messages {
		oai.Messages = append(oai.Messages, convertMessagesToOpenAI(&msg)...)
	}

	// 将 system prompt 转为第一条 system 消息
	if req.System != "" {
		sysMsg := OAIMessage{
			Role:    "system",
			Content: req.System,
		}
		oai.Messages = append([]OAIMessage{sysMsg}, oai.Messages...)
	}

	// 转换 tools
	if req.Tools != nil {
		tools := convertToolsToOpenAI(req.Tools)
		oai.Tools = tools
	}

	// 转换 tool_choice
	if req.ToolChoice != nil {
		oai.ToolChoice = convertToolChoiceToOpenAI(req.ToolChoice)
	}

	return oai, nil
}

// convertMessageToOpenAI 转换单条消息
func convertMessageToOpenAI(msg *Message) OAIMessage {
	messages := convertMessagesToOpenAI(msg)
	if len(messages) == 0 {
		return OAIMessage{Role: msg.Role}
	}
	return messages[0]
}

func convertMessagesToOpenAI(msg *Message) []OAIMessage {
	if blocks, ok := contentBlocks(msg.Content); ok {
		return convertBlockMessagesToOpenAI(msg.Role, blocks)
	}

	oaiMsg := OAIMessage{Role: msg.Role}
	switch content := msg.Content.(type) {
	case string:
		oaiMsg.Content = content
	default:
		oaiMsg.Content = stringifyContent(content)
	}
	return []OAIMessage{oaiMsg}
}

func contentBlocks(content interface{}) ([]ContentBlock, bool) {
	switch blocks := content.(type) {
	case []ContentBlock:
		return blocks, true
	case []interface{}:
		data, err := json.Marshal(blocks)
		if err != nil {
			return nil, false
		}
		var parsed []ContentBlock
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, false
		}
		return parsed, true
	default:
		return nil, false
	}
}

func convertBlockMessagesToOpenAI(role string, blocks []ContentBlock) []OAIMessage {
	msg := OAIMessage{Role: role}
	var textParts []string
	var out []OAIMessage
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			msg.ToolCalls = append(msg.ToolCalls, OAIToolCall{
				ID:   block.ID,
				Type: "function",
				Function: OAIFunction{
					Name:      block.Name,
					Arguments: string(args),
				},
			})
		case "tool_result":
			if len(textParts) > 0 {
				msg.Content = strings.Join(textParts, "\n")
			}
			if msg.Content != nil || len(msg.ToolCalls) > 0 {
				out = append(out, msg)
			}
			out = append(out, OAIMessage{
				Role:       "tool",
				ToolCallID: block.ToolUseID,
				Content:    block.Content,
			})
			msg = OAIMessage{Role: role}
			textParts = nil
		}
	}
	if len(textParts) > 0 {
		msg.Content = strings.Join(textParts, "\n")
	}
	if msg.Content != nil || len(msg.ToolCalls) > 0 {
		out = append(out, msg)
	}
	return out
}

func stringifyContent(content interface{}) string {
	data, err := json.Marshal(content)
	if err != nil {
		return fmt.Sprintf("%v", content)
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return string(data)
	}
	var parts []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "tool_use":
			parts = append(parts, fmt.Sprintf("[tool_use %s input=%v]", block.Name, block.Input))
		case "tool_result":
			parts = append(parts, fmt.Sprintf("[tool_result %s] %s", block.ToolUseID, block.Content))
		}
	}
	return strings.Join(parts, "\n")
}

// convertToolsToOpenAI 转换工具定义: Anthropic input_schema → OpenAI parameters
func convertToolsToOpenAI(tools interface{}) []OAIToolDef {
	// tools 是 []ToolDef (Anthropic 格式)
	toolBytes, err := json.Marshal(tools)
	if err != nil {
		return nil
	}

	var anthropicTools []ToolDef
	if err := json.Unmarshal(toolBytes, &anthropicTools); err != nil {
		return nil
	}

	var oaiTools []OAIToolDef
	for _, t := range anthropicTools {
		oaiTools = append(oaiTools, OAIToolDef{
			Type: "function",
			Function: OAIFunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return oaiTools
}

// convertToolChoiceToOpenAI 转换 tool_choice: {"type":"auto"} → "auto" 等
func convertToolChoiceToOpenAI(tc interface{}) interface{} {
	if tcMap, ok := tc.(map[string]interface{}); ok {
		if typ, ok := tcMap["type"]; ok {
			if tStr, ok := typ.(string); ok {
				// Anthropic: auto, any, tool
				return tStr
			}
		}
	}
	return "auto"
}

// ============================================================================
// 响应转换: OpenAI → Anthropic
// ============================================================================

// ConvertResponseFromOpenAI 将 OpenAI 兼容格式响应转换为 Anthropic 格式
func ConvertResponseFromOpenAI(oaiResp *OAIResponse) (*APIResponse, error) {
	resp := &APIResponse{
		ID:    oaiResp.ID,
		Model: oaiResp.Model,
		Usage: Usage{
			InputTokens:  oaiResp.Usage.PromptTokens,
			OutputTokens: oaiResp.Usage.CompletionTokens,
		},
	}

	if len(oaiResp.Choices) == 0 {
		return resp, fmt.Errorf("无响应内容")
	}

	choice := oaiResp.Choices[0]
	resp.StopReason = convertFinishReason(choice.FinishReason)

	msg := choice.Message

	// 构建 content blocks
	var blocks []ContentBlock

	// 文本内容
	if msg.Content != nil {
		switch c := msg.Content.(type) {
		case string:
			if c != "" {
				blocks = append(blocks, ContentBlock{Type: "text", Text: c})
			}
		default:
			contentStr := fmt.Sprintf("%v", c)
			if contentStr != "" {
				blocks = append(blocks, ContentBlock{Type: "text", Text: contentStr})
			}
		}
	}

	// 工具调用 → Anthropic content blocks
	for _, tc := range msg.ToolCalls {
		var input map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = map[string]interface{}{"raw": tc.Function.Arguments}
		}
		blocks = append(blocks, ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	resp.Content = blocks
	return resp, nil
}

// convertFinishReason 转换结束原因
func convertFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

// ============================================================================
// OpenAI SSE 流式处理
// ============================================================================

// OAIStreamState 流式处理状态
type OAIStreamState struct {
	ID       string
	Model    string
	FullText strings.Builder

	ToolCalls   []OAIToolCall
	toolCallMap map[int]int
}

// NewOAIStreamState 创建流式状态
func NewOAIStreamState() *OAIStreamState {
	return &OAIStreamState{
		toolCallMap: make(map[int]int),
	}
}

func (s *OAIStreamState) AppendTextDelta(content string) string {
	if content == "" {
		return ""
	}
	current := s.FullText.String()
	appendText := normalizeOAITextDelta(current, content)
	if appendText != "" {
		s.FullText.WriteString(appendText)
	}
	return appendText
}

func normalizeOAITextDelta(current string, incoming string) string {
	if incoming == "" {
		return ""
	}
	if current == "" {
		return incoming
	}
	if incoming == current {
		return ""
	}
	if strings.HasPrefix(incoming, current) {
		return strings.TrimPrefix(incoming, current)
	}
	overlap := textOverlapSuffixPrefix(current, incoming)
	if overlap > 0 {
		return string([]rune(incoming)[overlap:])
	}
	return incoming
}

func textOverlapSuffixPrefix(current string, incoming string) int {
	incomingRunes := []rune(incoming)
	if len(incomingRunes) <= 1 {
		return 0
	}
	currentRunes := []rune(current)
	max := len(incomingRunes) - 1
	if len(currentRunes) < max {
		max = len(currentRunes)
	}
	for n := max; n > 0; n-- {
		if !meaningfulTextOverlap(incomingRunes[:n]) {
			continue
		}
		if string(currentRunes[len(currentRunes)-n:]) == string(incomingRunes[:n]) {
			return n
		}
	}
	return 0
}

func meaningfulTextOverlap(overlap []rune) bool {
	if len(overlap) >= 2 {
		return true
	}
	return len(overlap) == 1 && isCJKRune(overlap[0])
}

func isCJKRune(r rune) bool {
	return (r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0x2A700 && r <= 0x2B73F) ||
		(r >= 0x2B740 && r <= 0x2B81F) ||
		(r >= 0x2B820 && r <= 0x2CEAF)
}

func (s *OAIStreamState) ApplyToolCallDelta(tc OAIToolCall) *ContentBlock {
	if s.toolCallMap == nil {
		s.toolCallMap = make(map[int]int)
	}

	idx := tc.Index
	if existingIndex, ok := s.toolCallMap[idx]; ok {
		existing := &s.ToolCalls[existingIndex]
		if tc.ID != "" {
			existing.ID = tc.ID
		}
		if tc.Type != "" {
			existing.Type = tc.Type
		}
		if tc.Function.Name != "" {
			existing.Function.Name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			existing.Function.Arguments += tc.Function.Arguments
		}
		return nil
	}

	s.ToolCalls = append(s.ToolCalls, OAIToolCall{
		Index:    idx,
		ID:       tc.ID,
		Type:     tc.Type,
		Function: tc.Function,
	})
	s.toolCallMap[idx] = len(s.ToolCalls) - 1
	if tc.ID == "" {
		return nil
	}
	return &ContentBlock{
		Type: "tool_use",
		ID:   tc.ID,
		Name: tc.Function.Name,
	}
}

func (s *OAIStreamState) ContentBlocks() []ContentBlock {
	var blocks []ContentBlock
	if s.FullText.Len() > 0 {
		blocks = append(blocks, ContentBlock{Type: "text", Text: s.FullText.String()})
	}
	for _, tc := range s.ToolCalls {
		blocks = append(blocks, ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: parseOAIToolInput(tc.Function.Arguments),
		})
	}
	return blocks
}

func parseOAIToolInput(arguments string) map[string]interface{} {
	if strings.TrimSpace(arguments) == "" {
		return map[string]interface{}{}
	}
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return map[string]interface{}{"raw": arguments}
	}
	return input
}

// ProcessOAIDelta 处理单个 delta 块
type OAIProcessResult struct {
	TextDelta string
	ToolStart *ContentBlock
	ToolEnd   *ContentBlock
	FinalResp *APIResponse
}

// ProcessOAIDeltaChunk 处理 OpenAI 流式 delta
func (s *OAIStreamState) ProcessOAIDeltaChunk(
	delta *OAIDelta,
	model string,
	finishReason string,
	onText func(string),
	onToolStart func(*ContentBlock),
	onToolEnd func(*ContentBlock),
) (*APIResponse, bool) {

	if s.ID == "" && model != "" {
		s.Model = model
	}

	// 文本增量
	if delta.Content != "" {
		textDelta := s.AppendTextDelta(delta.Content)
		if textDelta != "" && onText != nil {
			onText(textDelta)
		}
	}

	// 工具调用增量
	for _, tc := range delta.ToolCalls {
		if block := s.ApplyToolCallDelta(tc); block != nil && onToolStart != nil {
			onToolStart(block)
		}
	}

	// 检查是否完成
	isDone := finishReason != "" && finishReason != "null"
	if isDone {
		blocks := s.ContentBlocks()
		for _, block := range blocks {
			if block.Type == "tool_use" && onToolEnd != nil {
				blockCopy := block
				onToolEnd(&blockCopy)
			}
		}

		resp := &APIResponse{
			ID:         s.ID,
			Model:      s.Model,
			Content:    blocks,
			StopReason: convertFinishReason(finishReason),
		}
		return resp, true
	}

	return nil, false
}
