package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/t-rex-ai/t-rex/internal/config"
)

// Client API 客户端 (支持多 provider)
type Client struct {
	config     *config.AIConfig
	httpClient *http.Client
	baseURL    string
	provider   ProviderType
}

// Message 消息结构
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content,omitempty"`
}

// ToolUse 工具使用结果
type ToolUse struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

// ToolResult 工具执行结果
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// ContentBlock 内容块（支持文本和工具调用）
type ContentBlock struct {
	Type      string      `json:"type"`
	Text      string      `json:"text,omitempty"`
	ID        string      `json:"id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Input     interface{} `json:"input,omitempty"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   string      `json:"content,omitempty"`
	IsError   bool        `json:"is_error,omitempty"`
}

// APIRequest API 请求结构
type APIRequest struct {
	Model         string      `json:"model"`
	MaxTokens     int         `json:"max_tokens"`
	Messages      []Message   `json:"messages"`
	System        string      `json:"system,omitempty"`
	Temperature   float64     `json:"temperature,omitempty"`
	TopP          float64     `json:"top_p,omitempty"`
	Tools         interface{} `json:"tools,omitempty"` // 支持多种工具定义格式
	ToolChoice    interface{} `json:"tool_choice,omitempty"`
	StopSequences []string    `json:"stop_sequences,omitempty"`
	Metadata      *Metadata   `json:"metadata,omitempty"`
}

// ToolDef 工具定义
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// Metadata 元数据
type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

// APIResponse API 响应结构
type APIResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"Type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

// Usage Token 使用统计
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamEvent 流式事件
type StreamEvent struct {
	Type  string      `json:"type"`
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

// StreamDelta 增量内容
type StreamDelta struct {
	Type         string        `json:"type"`
	Delta        *DeltaText    `json:"delta,omitempty"`
	PartialJSON  string        `json:"partial_json,omitempty"`
	ContentBlock *ContentBlock `json:"content_block,omitempty"`
	Message      *APIResponse  `json:"message,omitempty"`
}

// DeltaText 文本增量
type DeltaText struct {
	Type       string `json:"type"`
	Text       string `json:"text"`
	StopReason string `json:"stop_reason,omitempty"`
}

// NewClient 创建新的 API 客户端
func NewClient(cfg *config.AIConfig) *Client {
	provider := ParseProvider(cfg.Provider)

	baseURL := normalizeAPIBaseURL(provider, cfg.BaseURL)

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second

	return &Client{
		config:     cfg,
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    baseURL,
		provider:   provider,
	}
}

// SendMessage 发送消息并获取响应（非流式）
func (c *Client) SendMessage(ctx context.Context, req *APIRequest) (*APIResponse, error) {
	req.Model = c.config.Model
	if req.MaxTokens == 0 {
		req.MaxTokens = c.config.MaxTokens
	}

	// 获取 endpoint 和请求体
	endpoint, body, err := c.prepareRequestBody(req, false)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, endpoint, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 根据 provider 解析响应
	return c.parseResponseBody(resp.Body)
}

// SendMessageStream 发送消息并获取流式响应
func (c *Client) SendMessageStream(
	ctx context.Context,
	req *APIRequest,
	onText func(text string),
	onToolStart func(tool *ContentBlock),
	onToolEnd func(content *ContentBlock),
	onComplete func(resp *APIResponse),
	onError func(err error),
) error {
	req.Model = c.config.Model
	if req.MaxTokens == 0 {
		req.MaxTokens = c.config.MaxTokens
	}

	// 获取 endpoint 和请求体
	endpoint, body, err := c.prepareRequestBody(req, true)
	if err != nil {
		return err
	}

	resp, err := c.doStreamRequest(ctx, endpoint, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 根据 provider 路由流式处理
	switch c.provider {
	case ProviderOpenAICompatible:
		return c.processOAIStream(resp.Body, onText, onToolStart, onToolEnd, onComplete, onError)
	default:
		return c.processSSEStream(resp.Body, onText, onToolStart, onToolEnd, onComplete, onError)
	}
}

// processSSEStream 处理 SSE 流
func (c *Client) processSSEStream(
	body io.Reader,
	onText func(text string),
	onToolStart func(tool *ContentBlock),
	onToolEnd func(content *ContentBlock),
	onComplete func(resp *APIResponse),
	onError func(err error),
) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var currentBlock *ContentBlock
	var toolInputBuilder strings.Builder
	var finalResponse *APIResponse
	var contentBlocks []ContentBlock
	completed := false

	appendText := func(text string) {
		if currentBlock != nil && currentBlock.Type == "text" {
			currentBlock.Text += text
			return
		}
		last := len(contentBlocks) - 1
		if last >= 0 && contentBlocks[last].Type == "text" {
			contentBlocks[last].Text += text
			return
		}
		contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: text})
	}

	complete := func() {
		if completed {
			return
		}
		completed = true
		if finalResponse == nil {
			finalResponse = &APIResponse{}
		}
		finalResponse.Content = append([]ContentBlock(nil), contentBlocks...)
		if onComplete != nil {
			onComplete(finalResponse)
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			complete()
			break
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			color.Yellow("⚠️ 解析事件失败: %v", err)
			continue
		}

		var eventType string
		_ = json.Unmarshal(raw["type"], &eventType)

		switch eventType {
		case "message_start":
			finalResponse = &APIResponse{}
			if msgBytes, ok := raw["message"]; ok {
				_ = json.Unmarshal(msgBytes, finalResponse)
			}

		case "content_block_start":
			block := &ContentBlock{}
			if blockBytes, ok := raw["content_block"]; ok {
				_ = json.Unmarshal(blockBytes, block)
			}
			currentBlock = block
			if block.Type == "tool_use" {
				toolInputBuilder.Reset()
				if onToolStart != nil {
					onToolStart(block)
				}
			}

		case "content_block_delta":
			var delta struct {
				Type        string `json:"type"`
				Text        string `json:"text,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
			}
			if deltaBytes, ok := raw["delta"]; ok {
				_ = json.Unmarshal(deltaBytes, &delta)
			}

			if delta.Text != "" {
				appendText(delta.Text)
				if onText != nil {
					onText(delta.Text)
				}
			}

			if delta.PartialJSON != "" && currentBlock != nil && currentBlock.Type == "tool_use" {
				toolInputBuilder.WriteString(delta.PartialJSON)
			}

		case "content_block_stop":
			if currentBlock != nil {
				block := *currentBlock
				if toolInputBuilder.Len() > 0 {
					var input map[string]interface{}
					if err := json.Unmarshal([]byte(toolInputBuilder.String()), &input); err == nil {
						block.Input = input
					} else {
						block.Input = map[string]interface{}{
							"raw":         toolInputBuilder.String(),
							"parse_error": err.Error(),
						}
					}
				}
				if block.Type != "text" || block.Text != "" {
					contentBlocks = append(contentBlocks, block)
				}
				if block.Type == "tool_use" && onToolEnd != nil {
					onToolEnd(&block)
				}
				currentBlock = nil
				toolInputBuilder.Reset()
			}

		case "message_delta":
			if finalResponse == nil {
				finalResponse = &APIResponse{}
			}
			var delta struct {
				StopReason string `json:"stop_reason,omitempty"`
			}
			if deltaBytes, ok := raw["delta"]; ok {
				_ = json.Unmarshal(deltaBytes, &delta)
				if delta.StopReason != "" {
					finalResponse.StopReason = delta.StopReason
				}
			}
			if usageBytes, ok := raw["usage"]; ok {
				var usage Usage
				if err := json.Unmarshal(usageBytes, &usage); err == nil {
					mergeUsage(&finalResponse.Usage, usage)
				}
			}

		case "message_stop":
			complete()

		case "error":
			err := streamErrorFromJSON(data)
			if err == nil {
				err = fmt.Errorf("API error: unknown error")
			}
			if onError != nil {
				onError(err)
			}
			return err

		case "ping":
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取流失败: %w", err)
	}

	return nil
}

func mergeUsage(dst *Usage, src Usage) {
	if src.InputTokens != 0 {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens != 0 {
		dst.OutputTokens = src.OutputTokens
	}
}

func streamErrorFromJSON(data string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil
	}
	errBytes, ok := raw["error"]
	if !ok {
		return nil
	}

	var errObj struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	if err := json.Unmarshal(errBytes, &errObj); err == nil && errObj.Message != "" {
		if errObj.Type != "" {
			return fmt.Errorf("API error (%s): %s", errObj.Type, errObj.Message)
		}
		return fmt.Errorf("API error: %s", errObj.Message)
	}

	var errText string
	if err := json.Unmarshal(errBytes, &errText); err == nil && errText != "" {
		return fmt.Errorf("API error: %s", errText)
	}
	return fmt.Errorf("API error: unknown error")
}

// doRequest 发送 HTTP POST 请求
func (c *Client) doRequest(ctx context.Context, endpoint string, body []byte) (*http.Response, error) {
	url := endpointURL(c.baseURL, endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, apiStatusError(resp.StatusCode, url, string(errBody))
	}

	return resp, nil
}

// doStreamRequest 发送流式请求
func (c *Client) doStreamRequest(ctx context.Context, endpoint string, body []byte) (*http.Response, error) {
	url := endpointURL(c.baseURL, endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	c.setHeaders(req)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, apiStatusError(resp.StatusCode, url, string(errBody))
	}

	return resp, nil
}

func normalizeAPIBaseURL(provider ProviderType, raw string) string {
	baseURL := strings.TrimSpace(raw)
	if baseURL == "" {
		if provider == ProviderOpenAICompatible {
			baseURL = "https://api.openai.com/v1"
		} else {
			baseURL = "https://api.anthropic.com/v1"
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if provider != ProviderOpenAICompatible {
		return baseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL
	}
	if strings.Trim(parsed.Path, "/") == "" {
		parsed.Path = "/v1"
	}
	return strings.TrimRight(parsed.String(), "/")
}

func endpointURL(baseURL string, endpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	ep := strings.Trim(strings.TrimSpace(endpoint), "/")
	if ep == "" {
		return base
	}
	if strings.HasSuffix(strings.ToLower(base), "/"+strings.ToLower(ep)) {
		return base
	}
	return base + "/" + ep
}

func apiStatusError(status int, requestURL string, body string) error {
	body = strings.TrimSpace(body)
	if len(body) > 2048 {
		body = body[:2048] + "...(truncated)"
	}
	if status == http.StatusNotFound && looksLikeHTML(body) {
		return fmt.Errorf("API error (404) at %s: endpoint not found. For OpenAI-compatible providers, set provider to the platform name or openai-compatible and set baseURL to the API root or /v1 root, for example https://api.minimax.io/v1. Response body: %s", requestURL, body)
	}
	return fmt.Errorf("API error (%d) at %s: %s", status, requestURL, body)
}

func looksLikeHTML(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(lower, "<!doctype html") ||
		strings.HasPrefix(lower, "<html") ||
		strings.Contains(lower, "<body")
}

// setHeaders 设置请求头（按 provider 切换认证方式）
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "T-Rex/1.0")

	switch c.provider {
	case ProviderOpenAICompatible:
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	default:
		req.Header.Set("x-api-key", c.config.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	for key, value := range c.config.Headers {
		req.Header.Set(key, value)
	}
}

// ============================================================================
// Provider 路由辅助方法
// ============================================================================

// prepareRequestBody 根据 provider 构造请求体，返回 (endpoint, body, error)
func (c *Client) prepareRequestBody(req *APIRequest, stream bool) (string, []byte, error) {
	var body []byte
	var err error
	var endpoint string

	switch c.provider {
	case ProviderOpenAICompatible:
		endpoint = "chat/completions"
		oaiReq, convertErr := ConvertRequestToOpenAI(req, stream)
		if convertErr != nil {
			return "", nil, fmt.Errorf("请求转换失败: %w", convertErr)
		}
		body, err = json.Marshal(oaiReq)
	default:
		endpoint = "messages"
		body, err = json.Marshal(req)
	}

	if err != nil {
		return "", nil, fmt.Errorf("序列化请求失败: %w", err)
	}
	return endpoint, body, nil
}

// parseResponseBody 根据 provider 解析响应体
func (c *Client) parseResponseBody(body io.Reader) (*APIResponse, error) {
	switch c.provider {
	case ProviderOpenAICompatible:
		var oaiResp OAIResponse
		if err := json.NewDecoder(body).Decode(&oaiResp); err != nil {
			return nil, fmt.Errorf("解析响应失败: %w", err)
		}
		return ConvertResponseFromOpenAI(&oaiResp)
	default:
		var apiResp APIResponse
		if err := json.NewDecoder(body).Decode(&apiResp); err != nil {
			return nil, fmt.Errorf("解析响应失败: %w", err)
		}
		return &apiResp, nil
	}
}

// processOAIStream 处理 OpenAI 兼容格式的 SSE 流
func (c *Client) processOAIStream(
	body io.Reader,
	onText func(text string),
	onToolStart func(tool *ContentBlock),
	onToolEnd func(content *ContentBlock),
	onComplete func(resp *APIResponse),
	onError func(err error),
) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	state := NewOAIStreamState()
	var lastUsage Usage
	completed := false
	toolsEnded := false
	pendingStopReason := ""

	endTools := func() {
		if toolsEnded {
			return
		}
		toolsEnded = true
		if onToolEnd == nil {
			return
		}
		for _, block := range state.ContentBlocks() {
			if block.Type == "tool_use" {
				blockCopy := block
				onToolEnd(&blockCopy)
			}
		}
	}

	complete := func(stopReason string) {
		if completed {
			return
		}
		if stopReason == "" {
			stopReason = "end_turn"
		}
		completed = true
		if onComplete != nil {
			onComplete(&APIResponse{
				ID:         state.ID,
				Model:      state.Model,
				Content:    state.ContentBlocks(),
				StopReason: stopReason,
				Usage:      lastUsage,
			})
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			if len(state.ToolCalls) > 0 {
				endTools()
			}
			complete(pendingStopReason)
			break
		}
		if err := streamErrorFromJSON(data); err != nil {
			if onError != nil {
				onError(err)
			}
			return err
		}

		var chunk OAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			color.Yellow("⚠️ 解析流事件失败: %v", err)
			continue
		}

		if state.ID == "" && chunk.ID != "" {
			state.ID = chunk.ID
		}
		if state.Model == "" && chunk.Model != "" {
			state.Model = chunk.Model
		}
		if chunk.Usage != nil {
			mergeUsage(&lastUsage, Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			})
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		if choice.Delta != nil {
			// 文本增量
			if choice.Delta.Content != "" {
				textDelta := state.AppendTextDelta(choice.Delta.Content)
				if textDelta != "" && onText != nil {
					onText(textDelta)
				}
			}

			// 工具调用增量
			for _, tc := range choice.Delta.ToolCalls {
				if block := state.ApplyToolCallDelta(tc); block != nil && onToolStart != nil {
					onToolStart(block)
				}
			}
		}

		// 检查 finish_reason
		if choice.FinishReason != "" && choice.FinishReason != "null" {
			pendingStopReason = convertFinishReason(choice.FinishReason)
			if len(state.ToolCalls) > 0 {
				endTools()
			}
			continue
		}

	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取流失败: %w", err)
	}
	if !completed && (pendingStopReason != "" || state.FullText.Len() > 0 || len(state.ToolCalls) > 0) {
		if len(state.ToolCalls) > 0 {
			endTools()
		}
		complete(pendingStopReason)
	}

	return nil
}
