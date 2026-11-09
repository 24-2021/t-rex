package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

var todoState = struct {
	sync.Mutex
	Items []string
}{}

type TodoTool struct{}

func (t *TodoTool) Name() string { return "Todo" }
func (t *TodoTool) Description() string {
	return "Maintain a short in-session checklist for long tasks. Use set to replace items, add to append one item, done to mark an item complete, list to view."
}
func (t *TodoTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"set", "add", "done", "list"}},
			"items":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"item":   map[string]interface{}{"type": "string"},
			"index":  map[string]interface{}{"type": "integer"},
		},
		"required": []string{"action"},
	}
}
func (t *TodoTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action, _ := input["action"].(string)
	todoState.Lock()
	defer todoState.Unlock()
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "set":
		todoState.Items = nil
		if raw, ok := input["items"].([]interface{}); ok {
			for _, item := range raw {
				if text := strings.TrimSpace(toString(item)); text != "" {
					todoState.Items = append(todoState.Items, "[ ] "+text)
				}
			}
		}
	case "add":
		if item := strings.TrimSpace(toString(input["item"])); item != "" {
			todoState.Items = append(todoState.Items, "[ ] "+item)
		}
	case "done":
		idx := intNumber(input["index"]) - 1
		if idx >= 0 && idx < len(todoState.Items) {
			todoState.Items[idx] = strings.Replace(todoState.Items[idx], "[ ]", "[x]", 1)
		}
	}
	data, _ := json.MarshalIndent(todoState.Items, "", "  ")
	return &ToolResult{Content: string(data), Output: map[string]interface{}{"items": append([]string(nil), todoState.Items...)}}, nil
}

func CurrentTodos() []string {
	todoState.Lock()
	defer todoState.Unlock()
	return append([]string(nil), todoState.Items...)
}

func SeedPentestTodosIfEmpty() {
	todoState.Lock()
	defer todoState.Unlock()
	if len(todoState.Items) > 0 {
		return
	}
	for _, item := range []string{
		"Confirm scope and rules of engagement",
		"Build asset, identity, role, endpoint, and trust-boundary inventory",
		"Perform low-noise recon and fingerprinting",
		"Build target-specific coverage from SecurityCatalog",
		"Review identity, session, access control, input handling, and business logic",
		"Review protocol, cloud, supply-chain, client, and AI/agent surfaces where applicable",
		"Validate findings and capture Burp Request/Response evidence",
		"Record findings, impact, remediation, and retest steps",
		"Generate pentest report draft",
	} {
		todoState.Items = append(todoState.Items, "[ ] "+item)
	}
}

func SeedAISecurityTodosIfEmpty() {
	todoState.Lock()
	defer todoState.Unlock()
	if len(todoState.Items) > 0 {
		return
	}
	for _, item := range []string{
		"Confirm AI security scope, ROE, rate/cost limits, and prohibited actions",
		"Inventory models, roles, trust boundaries, RAG sources, memory, tools, MCP, and output sinks",
		"Establish benign baselines and build the ai-agent SecurityCatalog test matrix",
		"Test direct injection, jailbreak, multilingual, encoding, and multi-turn mutations",
		"Test indirect injection, prompt leakage, RAG isolation, and sensitive data exposure",
		"Test tool authorization, excessive agency, memory poisoning, and multi-agent trust",
		"Test output handling, decision integrity, loop/fan-out limits, and resource exhaustion controls",
		"Record every case with SecurityTest and link failures to Evidence and Finding",
		"Retest confirmed issues and generate the AI security report",
	} {
		todoState.Items = append(todoState.Items, "[ ] "+item)
	}
}

func SeedCodeAuditTodosIfEmpty() {
	todoState.Lock()
	defer todoState.Unlock()
	if len(todoState.Items) > 0 {
		return
	}
	for _, item := range []string{
		"Confirm code audit scope, excluded directories, target languages, and review objective",
		"Map architecture, routes/controllers/pages, auth/session, data stores, jobs, and trust boundaries",
		"Detect frameworks and query CodeAuditCatalog for each in-scope language",
		"Build source-to-sink review paths for request, file, config, queue, and database-controlled inputs",
		"Review authz, injection, file/upload, SSRF, deserialization, template/output, crypto, secrets, and supply-chain risks",
		"Record confirmed findings with file/function/route, source, sink, impact, remediation, and evidence",
		"Capture or reconstruct Burp Request/Response POC for each vulnerability finding",
		"Mark likely/false-positive/needs-dynamic-validation candidates separately",
		"Generate code audit report with coverage, limitations, findings, and retest guidance",
	} {
		todoState.Items = append(todoState.Items, "[ ] "+item)
	}
}

func toString(value interface{}) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	data, _ := json.Marshal(value)
	return string(data)
}

func intNumber(value interface{}) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
