package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/t-rex-ai/t-rex/internal/config"
)

// Client MCP 客户端接口
type Client interface {
	// Initialize 初始化 MCP 连接
	Initialize(ctx context.Context) (*InitializeResult, error)
	// ListTools 列出可用工具
	ListTools(ctx context.Context) ([]Tool, error)
	// CallTool 调用工具
	CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error)
	// Close 关闭连接
	Close() error
}

// InitializeResult 初始化结果
type InitializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ServerInfo      ServerInfo             `json:"serverInfo"`
}

// ServerInfo 服务器信息
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// Tool MCP 工具定义
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// CallToolResult 工具调用结果
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock 内容块
type ContentBlock struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
}

// JSONRPCRequest JSON-RPC 请求
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse JSON-RPC 响应
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError JSON-RPC 错误
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// Manager MCP 服务器管理器
type Manager struct {
	clients map[string]Client
	mu      sync.RWMutex
	config  *config.MCPConfig
}

// NewManager 创建 MCP 管理器
func NewManager(mcpCfg *config.MCPConfig) *Manager {
	return &Manager{
		clients: make(map[string]Client),
		config:  mcpCfg,
	}
}

// StartAll 启动所有已配置的 MCP 服务器
func (m *Manager) StartAll(ctx context.Context) error {
	for _, serverCfg := range m.config.Servers {
		if !serverCfg.Enabled {
			continue
		}

		client, err := createClient(serverCfg)
		if err != nil {
			fmt.Printf("⚠️ 启动 MCP 服务器 '%s' 失败: %v\n", serverCfg.Name, err)
			continue
		}

		// 初始化连接
		result, err := client.Initialize(ctx)
		if err != nil {
			fmt.Printf("⚠️ 初始化 MCP 服务器 '%s' 失败: %v\n", serverCfg.Name, err)
			continue
		}

		m.mu.Lock()
		m.clients[serverCfg.Name] = client
		m.mu.Unlock()

		fmt.Printf("✅ MCP 服务器 '%s' 已连接 (v%s)\n",
			serverCfg.Name, result.ServerInfo.Version)
	}

	return nil
}

// GetClient 获取指定名称的客户端
func (m *Manager) GetClient(name string) (Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, exists := m.clients[name]
	return client, exists
}

// GetAllTools 获取所有 MCP 服务器的工具列表
func (m *Manager) GetAllTools(ctx context.Context) (map[string][]Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allTools := make(map[string][]Tool)
	for name, client := range m.clients {
		tools, err := client.ListTools(ctx)
		if err != nil {
			continue
		}
		allTools[name] = tools
	}
	return allTools, nil
}

// CloseAll 关闭所有连接
func (m *Manager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			lastErr = fmt.Errorf("关闭 '%s' 失败: %w", name, err)
		}
		delete(m.clients, name)
	}
	return lastErr
}

// createClient 根据配置创建客户端
func createClient(cfg config.MCPServerConfig) (Client, error) {
	transport := strings.ToLower(strings.TrimSpace(cfg.Type))
	if transport == "streamable-http" {
		transport = "http"
	}
	if cfg.URL != "" || transport == "http" || transport == "sse" {
		if transport == "sse" || isSSEURL(cfg.URL) {
			return newSSEClient(cfg), nil
		}
		return newHTTPClient(cfg), nil
	}

	// stdio 模式
	return newStdioClient(cfg), nil
}

// ========== Stdio 客户端实现 ==========

// StdioClient 通过标准输入输出通信的客户端
type StdioClient struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	serverCfg config.MCPServerConfig
	requestID int64
	mu        sync.Mutex
}

// newStdioClient 创建 stdio 客户端
func newStdioClient(serverCfg config.MCPServerConfig) *StdioClient {
	return &StdioClient{serverCfg: serverCfg}
}

func (c *StdioClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	c.cmd = exec.CommandContext(ctx, c.serverCfg.Command, c.serverCfg.Args...)

	// 设置环境变量
	env := os.Environ()
	for k, v := range c.serverCfg.Env {
		env = append(env, k+"="+v)
	}
	c.cmd.Env = env

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := c.cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动进程失败: %w", err)
	}

	// 发送 initialize 请求
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"clientInfo": map[string]interface{}{
			"name":    "t-rex",
			"version": "1.0.0",
		},
	}

	var result InitializeResult
	if _, err := c.sendRequest(ctx, "initialize", params, &result); err != nil {
		return nil, err
	}

	// 发送 initialized 通知
	c.sendNotification("notifications/initialized", nil)

	return &result, nil
}

func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	var response struct {
		Tools []Tool `json:"tools"`
	}

	if _, err := c.sendRequest(ctx, "tools/list", nil, &response); err != nil {
		return nil, err
	}

	return response.Tools, nil
}

func (c *StdioClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}

	var result CallToolResult
	if _, err := c.sendRequest(ctx, "tools/call", params, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *StdioClient) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	return nil
}

func (c *StdioClient) sendRequest(ctx context.Context, method string, params interface{}, result interface{}) (int64, error) {
	c.mu.Lock()
	c.requestID++
	reqID := c.requestID
	c.mu.Unlock()

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return reqID, err
	}

	// 发送请求
	if _, err := c.stdin.Write(data); err != nil {
		return reqID, err
	}
	if _, err := c.stdin.Write([]byte("\n")); err != nil {
		return reqID, err
	}

	// 读取响应
	response, err := c.readResponse(ctx)
	if err != nil {
		return reqID, err
	}

	if response.Error != nil {
		return reqID, fmt.Errorf("MCP 错误 [%d]: %s", response.Error.Code, response.Error.Message)
	}

	if result != nil {
		if err := json.Unmarshal(response.Result, result); err != nil {
			return reqID, err
		}
	}

	return reqID, nil
}

func (c *StdioClient) sendNotification(method string, params interface{}) error {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	_, err = c.stdin.Write(data)
	if err != nil {
		return err
	}
	_, err = c.stdin.Write([]byte("\n"))
	return err
}

func (c *StdioClient) readResponse(ctx context.Context) (*JSONRPCResponse, error) {
	type responseWrapper struct {
		Response JSONRPCResponse
		Error    error
	}

	respCh := make(chan responseWrapper, 1)

	go func() {
		reader := bufio.NewReader(c.stdout)
		line, err := reader.ReadBytes('\n')

		wrapper := responseWrapper{}
		if err != nil {
			wrapper.Error = err
		} else {
			if err := json.Unmarshal(line, &wrapper.Response); err != nil {
				wrapper.Error = err
			}
		}
		respCh <- wrapper
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case wrapper := <-respCh:
		if wrapper.Error != nil {
			return nil, wrapper.Error
		}
		return &wrapper.Response, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("MCP 响应超时")
	}
}

// ========== HTTP 客户端实现 ==========

// HTTPClient HTTP 模式的客户端
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
	serverCfg  config.MCPServerConfig
	requestID  int64
	mu         sync.Mutex
}

// newHTTPClient 创建 HTTP 客户端
func newHTTPClient(serverCfg config.MCPServerConfig) *HTTPClient {
	return &HTTPClient{
		baseURL:    serverCfg.URL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		serverCfg:  serverCfg,
	}
}

func (h *HTTPClient) Initialize(ctx context.Context) (*InitializeResult, error) {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "t-rex", "version": "1.0.0"},
	}

	var result InitializeResult
	if _, err := h.post(ctx, "initialize", params, &result); err != nil {
		return nil, err
	}
	h.post(ctx, "notifications/initialized", nil, nil)
	return &result, nil
}

func (h *HTTPClient) ListTools(ctx context.Context) ([]Tool, error) {
	var resp struct {
		Tools []Tool `json:"tools"`
	}
	if _, err := h.post(ctx, "tools/list", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Tools, nil
}

func (h *HTTPClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	params := map[string]interface{}{"name": name, "arguments": arguments}
	var result CallToolResult
	if _, err := h.post(ctx, "tools/call", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (h *HTTPClient) Close() error { return nil }

func (h *HTTPClient) post(ctx context.Context, method string, params, result interface{}) (int64, error) {
	h.mu.Lock()
	h.requestID++
	id := h.requestID
	h.mu.Unlock()

	req := JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL, bytes.NewReader(body))
	if err != nil {
		return id, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range h.serverCfg.Headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return id, err
	}
	defer resp.Body.Close()

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return id, err
	}

	if rpcResp.Error != nil {
		return id, fmt.Errorf("MCP Error [%d]: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if result != nil {
		json.Unmarshal(rpcResp.Result, result)
	}
	return id, nil
}

// SSEClient SSE 模式的客户端（简化实现）
type SSEClient struct {
	*HTTPClient
}

func newSSEClient(serverCfg config.MCPServerConfig) *SSEClient {
	return &SSEClient{HTTPClient: newHTTPClient(serverCfg)}
}

func isSSEURL(url string) bool {
	return strings.Contains(strings.ToLower(url), "/sse")
}
