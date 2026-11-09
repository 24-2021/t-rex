package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/t-rex-ai/t-rex/internal/config"
)

type auditEvent struct {
	Time    time.Time              `json:"time"`
	Tool    string                 `json:"tool"`
	Input   map[string]interface{} `json:"input,omitempty"`
	Result  string                 `json:"result,omitempty"`
	Error   bool                   `json:"error,omitempty"`
	Workdir string                 `json:"workdir,omitempty"`
}

func writeAuditEvent(tool string, cfg *config.Config, input map[string]interface{}, result *ToolResult, workdir string) {
	if cfg == nil || !cfg.Audit.Enabled {
		return
	}
	path := cfg.Audit.Path
	if path == "" {
		path = ".cache/audit.jsonl"
	}
	if !filepath.IsAbs(path) {
		root, err := workspaceRoot(cfg)
		if err != nil {
			return
		}
		path = filepath.Join(root, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	event := auditEvent{Time: time.Now(), Tool: tool, Input: scrubAuditInput(input), Workdir: workdir}
	if result != nil {
		event.Result = truncateAuditValue(result.Content, 500)
		event.Error = result.IsError
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

func scrubAuditInput(input map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(input))
	for k, v := range input {
		key := strings.ToLower(k)
		if strings.Contains(key, "key") || strings.Contains(key, "token") || strings.Contains(key, "password") || strings.Contains(key, "secret") {
			out[k] = "[redacted]"
			continue
		}
		out[k] = truncateAuditValue(fmt.Sprint(v), 1000)
	}
	return out
}

func truncateAuditValue(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "...(truncated)"
}
