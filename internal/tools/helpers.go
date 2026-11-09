package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/t-rex-ai/t-rex/internal/config"
)

// readFileContent 读取文件原始内容
func readFileContent(filePath string) ([]byte, error) {
	// 安全检查：防止路径遍历
	cleanPath := filepath.Clean(filePath)
	if strings.Contains(cleanPath, "..") {
		// 解析为绝对路径
		absPath, err := filepath.Abs(cleanPath)
		if err != nil {
			return nil, fmt.Errorf("路径解析失败: %w", err)
		}
		cleanPath = absPath
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("无法读取文件 '%s': %w", cleanPath, err)
	}
	return data, nil
}

func validateFileSize(cfg *config.Config, filePath string) error {
	maxSize := maxFileSize(cfg)
	if maxSize <= 0 {
		return nil
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("无法检查文件大小 '%s': %w", filePath, err)
	}
	if info.Size() > maxSize {
		return fmt.Errorf("文件大小 %d bytes 超过 MaxFileSize %d bytes", info.Size(), maxSize)
	}
	return nil
}

func maxFileSize(cfg *config.Config) int64 {
	if cfg == nil {
		return 0
	}
	return cfg.Project.MaxFileSize
}

// writeFileContent 写入内容到文件
func writeFileContent(filePath string, content string) error {
	// 确保目录存在
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// globSearch 执行 glob 模式搜索
func globSearch(rootDir, pattern string) ([]string, error) {
	return globSearchWithPolicy(nil, rootDir, pattern)
}

func globSearchWithPolicy(cfg *config.Config, rootDir, pattern string) ([]string, error) {
	if rootDir == "" {
		var err error
		rootDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	var results []string
	policyRoot := rootDir
	if cfg != nil {
		var policyErr error
		policyRoot, policyErr = workspaceRoot(cfg)
		if policyErr != nil {
			return nil, policyErr
		}
	}

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过无法访问的
		}
		if cfg != nil {
			allowed, policyErr := projectPathAllowed(cfg, path, policyRoot)
			if policyErr != nil || !allowed {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		// 统一路径分隔符为正斜杠
		relPath = filepath.ToSlash(relPath)

		matched, err := globPatternMatch(pattern, relPath)
		if err != nil {
			return nil // 无效 pattern，跳过
		}
		if matched {
			results = append(results, relPath)
		}

		return nil
	})

	return results, err
}

// globPatternMatch implements the ** semantics expected by coding-agent glob
// tools. filepath.Match alone cannot match nested paths with **.
func globPatternMatch(pattern, path string) (bool, error) {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)
	var expression strings.Builder
	expression.WriteString("^")
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i += 2
				if i < len(pattern) && pattern[i] == '/' {
					expression.WriteString("(?:.*/)?")
					i++
				} else {
					expression.WriteString(".*")
				}
			} else {
				expression.WriteString("[^/]*")
				i++
			}
		case '?':
			expression.WriteString("[^/]")
			i++
		default:
			expression.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		}
	}
	expression.WriteString("$")
	return regexp.MatchString(expression.String(), path)
}

// GrepResult grep 搜索结果
type GrepResult struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content,omitempty"`
}

// grepSearch 执行正则表达式搜索
func grepSearch(rootDir string, re *regexp.Regexp, globPattern string, includeContent bool) ([]GrepResult, error) {
	return grepSearchWithPolicy(nil, rootDir, re, globPattern, includeContent)
}

func grepSearchWithPolicy(cfg *config.Config, rootDir string, re *regexp.Regexp, globPattern string, includeContent bool) ([]GrepResult, error) {
	if rootDir == "" {
		var err error
		rootDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	var results []GrepResult
	policyRoot := rootDir
	if cfg != nil {
		var policyErr error
		policyRoot, policyErr = workspaceRoot(cfg)
		if policyErr != nil {
			return nil, policyErr
		}
	}

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if cfg != nil {
			allowed, policyErr := projectPathAllowed(cfg, path, policyRoot)
			if policyErr != nil || !allowed {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if maxSize := cfg.Project.MaxFileSize; maxSize > 0 && info.Size() > maxSize {
				return nil
			}
		}
		if info.IsDir() || info.Size() > 10*1024*1024 { // 限制10MB
			return nil
		}

		// 应用 glob 过滤
		if globPattern != "" {
			relPath, _ := filepath.Rel(rootDir, path)
			matched, _ := globPatternMatch(globPattern, filepath.ToSlash(relPath))
			if !matched {
				return nil
			}
		}

		// 跳过二进制文件和常见非文本目录
		if shouldSkipFile(path) {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				result := GrepResult{
					File: path,
					Line: lineNum,
				}
				if includeContent {
					result.Content = strings.TrimSpace(line)
				}
				results = append(results, result)
			}
		}

		return nil
	})

	return results, err
}

// shouldSkipFile 判断是否应跳过该文件
func shouldSkipFile(path string) bool {
	// 常见需要跳过的目录/文件
	skipPatterns := []string{
		".git/", "node_modules/", "__pycache__/", ".cache/",
		"vendor/", "dist/", ".next/", "build/",
		".exe", ".dll", ".so", ".dylib", ".bin",
		".png", ".jpg", ".jpeg", ".gif", ".ico",
		".zip", ".tar", ".gz", ".rar",
		".pdf", ".doc", ".xls",
	}

	lowerPath := strings.ToLower(path)
	for _, pattern := range skipPatterns {
		if strings.Contains(lowerPath, pattern) {
			return true
		}
	}
	return false
}

// ToolDefinition 用于 MCP 工具定义导出
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ToJSON 将工具定义转换为 JSON 字符串
func (t *ToolDefinition) ToJSON() (string, error) {
	data, err := json.MarshalIndent(t, "", "  ")
	return string(data), err
}

// GetToolDefinitions 获取所有工具定义的列表（用于 MCP 导出）
func GetToolDefinitions(registry *Registry) []ToolDefinition {
	tools := registry.List()
	definitions := make([]ToolDefinition, len(tools))
	for i, tool := range tools {
		definitions[i] = ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		}
	}
	return definitions
}

// ConfigGetter 配置获取接口（用于工具获取配置）
type ConfigGetter interface {
	GetConfig() *config.Config
}
