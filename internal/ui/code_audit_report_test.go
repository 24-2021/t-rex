package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/t-rex-ai/t-rex/internal/config"
	"github.com/t-rex-ai/t-rex/internal/session"
	"github.com/t-rex-ai/t-rex/internal/tools"
)

func TestRenderCodeAuditReportIncludesBurpPOC(t *testing.T) {
	ctx := context.Background()
	_, _ = (&tools.FindingTool{}).Execute(ctx, map[string]interface{}{"action": "clear"})
	t.Cleanup(func() {
		_, _ = (&tools.FindingTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
	})

	result, err := (&tools.FindingTool{}).Execute(ctx, map[string]interface{}{
		"action":       "add",
		"id":           "F-AUDIT-1",
		"title":        "IDOR in user export",
		"severity":     "High",
		"confidence":   "High",
		"status":       "confirmed",
		"asset":        "internal/export/users.php",
		"endpoint":     "GET /export/users",
		"reproduction": "user_id flows from query string into export authorization without owner check",
		"request":      "GET /export/users?user_id=1002 HTTP/1.1\r\nHost: example.test\r\nCookie: session=REDACTED\r\n\r\n",
		"response":     "HTTP/1.1 200 OK\r\nContent-Type: text/csv\r\n\r\nid,email\n1002,redacted@example.test\n",
	})
	if err != nil || result.IsError {
		t.Fatalf("add finding failed: result=%#v err=%v", result, err)
	}

	cfg := config.Default()
	cfg.AI.WorkMode = "code-audit"
	repl := &REPL{config: cfg}
	report := repl.renderCodeAuditReport(&session.Session{
		ID:        "s1",
		Model:     "test-model",
		Path:      "repo",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	for _, want := range []string{
		"#### POC",
		"Burp Request",
		"GET /export/users?user_id=1002 HTTP/1.1",
		"Burp Response",
		"HTTP/1.1 200 OK",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestRenderCodeAuditReportIncludesPOCPlaceholder(t *testing.T) {
	ctx := context.Background()
	_, _ = (&tools.FindingTool{}).Execute(ctx, map[string]interface{}{"action": "clear"})
	t.Cleanup(func() {
		_, _ = (&tools.FindingTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
	})

	result, err := (&tools.FindingTool{}).Execute(ctx, map[string]interface{}{
		"action": "add",
		"id":     "F-AUDIT-2",
		"title":  "Potential unsafe deserialization",
		"status": "needs-dynamic-validation",
	})
	if err != nil || result.IsError {
		t.Fatalf("add finding failed: result=%#v err=%v", result, err)
	}

	cfg := config.Default()
	cfg.AI.WorkMode = "code-audit"
	repl := &REPL{config: cfg}
	report := repl.renderCodeAuditReport(&session.Session{ID: "s1", Model: "test-model"})

	for _, want := range []string{
		"#### POC",
		"Burp Request",
		"Burp Response",
		"Not captured. For HTTP-facing code audit findings",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestRenderPentestReportSummarizesFindingStatuses(t *testing.T) {
	ctx := context.Background()
	for _, tool := range []interface {
		Execute(context.Context, map[string]interface{}) (*tools.ToolResult, error)
	}{
		&tools.MissionTool{},
		&tools.ScopeTool{},
		&tools.FindingTool{},
	} {
		_, _ = tool.Execute(ctx, map[string]interface{}{"action": "clear"})
	}
	t.Cleanup(func() {
		_, _ = (&tools.FindingTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
		_, _ = (&tools.MissionTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
		_, _ = (&tools.ScopeTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
	})

	if result, err := (&tools.FindingTool{}).Execute(ctx, map[string]interface{}{
		"action":   "add",
		"title":    "Confirmed IDOR",
		"severity": "high",
		"request":  "GET /api/users/2 HTTP/1.1\r\nHost: example.test\r\n\r\n",
		"response": "HTTP/1.1 200 OK\r\n\r\n{}",
	}); err != nil || result.IsError {
		t.Fatalf("add confirmed finding failed: result=%#v err=%v", result, err)
	}
	if result, err := (&tools.FindingTool{}).Execute(ctx, map[string]interface{}{
		"action": "add",
		"title":  "Possible missing rate limit",
		"status": "needs-validation",
	}); err != nil || result.IsError {
		t.Fatalf("add likely finding failed: result=%#v err=%v", result, err)
	}

	cfg := config.Default()
	cfg.AI.WorkMode = "pentest"
	repl := &REPL{config: cfg}
	report := repl.renderPentestReport(&session.Session{ID: "s1", Model: "test-model"})

	for _, want := range []string{
		"- confirmed findings: 1",
		"- needs-dynamic-validation findings: 1",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestRenderPentestReportIncludesCoverageGapsAndQualityGate(t *testing.T) {
	ctx := context.Background()
	for _, tool := range []interface {
		Execute(context.Context, map[string]interface{}) (*tools.ToolResult, error)
	}{
		&tools.MissionTool{},
		&tools.ScopeTool{},
		&tools.FindingTool{},
		&tools.AssessmentPlanTool{},
		&tools.SecurityCoverageTool{},
	} {
		_, _ = tool.Execute(ctx, map[string]interface{}{"action": "clear"})
	}
	t.Cleanup(func() {
		_, _ = (&tools.FindingTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
		_, _ = (&tools.AssessmentPlanTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
		_, _ = (&tools.SecurityCoverageTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
	})

	_, _ = (&tools.AssessmentPlanTool{}).Execute(ctx, map[string]interface{}{
		"action":         "set",
		"objective":      "Assess tenant API",
		"target_profile": "authenticated API with two roles",
		"in_scope":       []interface{}{"https://api.example.test"},
		"out_of_scope":   []interface{}{"DoS"},
		"roles":          []interface{}{"user", "admin"},
		"rules":          "low-noise checks",
	})
	_, _ = (&tools.SecurityCoverageTool{}).Execute(ctx, map[string]interface{}{
		"action":     "add",
		"catalog_id": "WEB-SESS",
		"area":       "Session management",
		"target":     "authenticated API",
		"status":     "tested",
		"tested":     "cookie attributes and logout invalidation sampled",
	})
	_, _ = (&tools.SecurityCoverageTool{}).Execute(ctx, map[string]interface{}{
		"action":     "add",
		"catalog_id": "WEB-AC",
		"area":       "Access control",
		"target":     "tenant resources",
		"status":     "blocked",
		"reason":     "second tenant account unavailable",
	})
	if result, err := (&tools.FindingTool{}).Execute(ctx, map[string]interface{}{
		"action": "add",
		"id":     "F-QUALITY",
		"title":  "Confirmed but incomplete",
		"status": "confirmed",
	}); err != nil || result.IsError {
		t.Fatalf("add incomplete finding failed: result=%#v err=%v", result, err)
	}

	cfg := config.Default()
	cfg.AI.WorkMode = "pentest"
	repl := &REPL{config: cfg}
	report := repl.renderPentestReport(&session.Session{ID: "s1", Model: "test-model"})

	for _, want := range []string{
		"## Assessment Plan",
		"authenticated API with two roles",
		"## Security Coverage",
		"- tested coverage items: 1",
		"- blocked coverage items: 1",
		"## Coverage Gaps And Residual Risk",
		"WEB-AC remains blocked for tenant resources: second tenant account unavailable",
		"## Finding Quality Gate",
		"Burp Request/Response POC, impact, remediation, retest method",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestRenderPentestReportNoFindingsUsesCoverageResidualRisk(t *testing.T) {
	ctx := context.Background()
	_, _ = (&tools.FindingTool{}).Execute(ctx, map[string]interface{}{"action": "clear"})
	_, _ = (&tools.SecurityCoverageTool{}).Execute(ctx, map[string]interface{}{"action": "clear"})
	t.Cleanup(func() {
		_, _ = (&tools.FindingTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
		_, _ = (&tools.SecurityCoverageTool{}).Execute(context.Background(), map[string]interface{}{"action": "clear"})
	})
	_, _ = (&tools.SecurityCoverageTool{}).Execute(ctx, map[string]interface{}{
		"action":     "add",
		"catalog_id": "WEB-AUTH",
		"area":       "Authentication",
		"status":     "tested",
	})
	_, _ = (&tools.SecurityCoverageTool{}).Execute(ctx, map[string]interface{}{
		"action":     "add",
		"catalog_id": "API-GQL",
		"area":       "GraphQL",
		"status":     "not-tested",
		"reason":     "GraphQL endpoint was not exposed in provided scope",
	})

	cfg := config.Default()
	cfg.AI.WorkMode = "pentest"
	repl := &REPL{config: cfg}
	report := repl.renderPentestReport(&session.Session{ID: "s1", Model: "test-model"})

	for _, want := range []string{
		"No confirmed vulnerabilities were recorded from the structured coverage completed in this assessment.",
		"API-GQL remains not-tested: GraphQL endpoint was not exposed in provided scope",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}
