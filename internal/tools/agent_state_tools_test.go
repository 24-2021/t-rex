package tools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/config"
)

func resetAgentStateForTest() {
	agentState.Lock()
	defer agentState.Unlock()
	agentState.Mission = MissionState{}
	agentState.Scopes = nil
	agentState.Findings = nil
	agentState.Evidence = nil
	agentState.AssessmentPlan = AssessmentPlanState{}
	agentState.Coverage = nil
	agentState.SecurityTests = nil
}

func TestSecurityCatalogAndTestTools(t *testing.T) {
	resetAgentStateForTest()
	catalog, err := (&SecurityCatalogTool{}).Execute(context.Background(), map[string]interface{}{
		"action": "list",
		"domain": "ai-agent",
	})
	if err != nil || catalog.IsError {
		t.Fatalf("SecurityCatalog.Execute failed: result=%#v err=%v", catalog, err)
	}
	entries, ok := catalog.Output.([]SecurityCatalogEntry)
	if !ok || len(entries) < 10 {
		t.Fatalf("AI security catalog is too small: %#v", catalog.Output)
	}
	if len(entries[0].EvidenceRequirements) == 0 || len(entries[0].AppliesTo) == 0 {
		t.Fatalf("catalog entries should include evidence requirements and applicability: %#v", entries[0])
	}

	filtered, err := (&SecurityCatalogTool{}).Execute(context.Background(), map[string]interface{}{
		"action":          "list",
		"domain":          "web-api",
		"target_features": []interface{}{"webhook"},
	})
	if err != nil || filtered.IsError {
		t.Fatalf("SecurityCatalog filtered Execute failed: result=%#v err=%v", filtered, err)
	}
	filteredEntries, ok := filtered.Output.([]SecurityCatalogEntry)
	if !ok || len(filteredEntries) == 0 {
		t.Fatalf("expected feature-filtered catalog entries: %#v", filtered.Output)
	}
	foundWebhook := false
	for _, entry := range filteredEntries {
		if entry.ID == "API-WEBHOOK" {
			foundWebhook = true
			break
		}
	}
	if !foundWebhook {
		t.Fatalf("webhook feature filter did not return API-WEBHOOK: %#v", filteredEntries)
	}

	for _, tt := range []struct {
		feature string
		wantID  string
	}{
		{"actuator", "RECON-DOCS"},
		{"source-leak", "RECON-LEAKS"},
		{"sso", "AUTH-SSO"},
		{"race", "WEB-BIZ-RACE"},
		{"k8s", "CLOUD-K8S"},
		{"cve", "MODERN-COMPONENTS"},
	} {
		t.Run(tt.feature, func(t *testing.T) {
			result, err := (&SecurityCatalogTool{}).Execute(context.Background(), map[string]interface{}{
				"action":          "list",
				"target_features": []interface{}{tt.feature},
			})
			if err != nil || result.IsError {
				t.Fatalf("SecurityCatalog feature Execute failed: result=%#v err=%v", result, err)
			}
			entries, ok := result.Output.([]SecurityCatalogEntry)
			if !ok || len(entries) == 0 {
				t.Fatalf("expected entries for feature %q: %#v", tt.feature, result.Output)
			}
			for _, entry := range entries {
				if entry.ID == tt.wantID {
					return
				}
			}
			t.Fatalf("feature %q did not return %s: %#v", tt.feature, tt.wantID, entries)
		})
	}

	result, err := (&SecurityTestTool{}).Execute(context.Background(), map[string]interface{}{
		"action":   "add",
		"title":    "Indirect injection in retrieved document",
		"vector":   "LLM-INDIRECT",
		"channel":  "rag-document",
		"expected": "Treat retrieved instructions as untrusted data",
		"actual":   "Tool call was blocked",
		"outcome":  "pass",
	})
	if err != nil || result.IsError {
		t.Fatalf("SecurityTest.Execute failed: result=%#v err=%v", result, err)
	}
	tests := CurrentSecurityTests()
	if len(tests) != 1 || tests[0].Vector != "LLM-INDIRECT" || tests[0].Outcome != "pass" {
		t.Fatalf("security tests = %#v", tests)
	}
}

func TestCodeAuditCatalogTool(t *testing.T) {
	result, err := (&CodeAuditCatalogTool{}).Execute(context.Background(), map[string]interface{}{
		"action":   "list",
		"language": "nodejs",
	})
	if err != nil || result.IsError {
		t.Fatalf("CodeAuditCatalog.Execute failed: result=%#v err=%v", result, err)
	}
	entries, ok := result.Output.([]CodeAuditCatalogEntry)
	if !ok || len(entries) == 0 {
		t.Fatalf("Node.js catalog is empty: %#v", result.Output)
	}
	foundNodeServer := false
	for _, entry := range entries {
		if entry.ID == "AUD-NODE-SERVER" {
			foundNodeServer = true
			break
		}
	}
	if !foundNodeServer {
		t.Fatalf("Node.js catalog missing server-side entry: %#v", entries)
	}

	search, err := (&CodeAuditCatalogTool{}).Execute(context.Background(), map[string]interface{}{
		"action": "search",
		"query":  "unserialize",
	})
	if err != nil || search.IsError {
		t.Fatalf("CodeAuditCatalog search failed: result=%#v err=%v", search, err)
	}
	searchEntries, ok := search.Output.([]CodeAuditCatalogEntry)
	if !ok || len(searchEntries) == 0 {
		t.Fatalf("expected unserialize search results: %#v", search.Output)
	}
}

func TestMissionScopeAndFindingTools(t *testing.T) {
	resetAgentStateForTest()
	ctx := context.Background()

	if result, err := (&MissionTool{}).Execute(ctx, map[string]interface{}{
		"action":    "set",
		"goal":      "Assess authorized target",
		"phase":     "recon",
		"done_when": "report is drafted",
	}); err != nil || result.IsError {
		t.Fatalf("Mission.Execute failed: result=%#v err=%v", result, err)
	}
	if mission := CurrentMission(); mission.Goal != "Assess authorized target" || mission.Phase != "recon" {
		t.Fatalf("mission not updated: %#v", mission)
	}

	if result, err := (&ScopeTool{}).Execute(ctx, map[string]interface{}{"action": "add", "target": "https://example.test"}); err != nil || result.IsError {
		t.Fatalf("Scope.Execute failed: result=%#v err=%v", result, err)
	}
	if got := CurrentScope(); len(got) != 1 || got[0] != "https://example.test" {
		t.Fatalf("scope = %#v", got)
	}

	if result, err := (&FindingTool{}).Execute(ctx, map[string]interface{}{
		"action":      "add",
		"title":       "IDOR",
		"severity":    "High",
		"request":     "GET /user?id=1 HTTP/1.1\r\nHost: example.test\r\n\r\n",
		"response":    "HTTP/1.1 200 OK\r\n\r\n{}",
		"remediation": "Enforce object-level authorization.",
	}); err != nil || result.IsError {
		t.Fatalf("Finding.Execute failed: result=%#v err=%v", result, err)
	}
	findings := CurrentFindings()
	if len(findings) != 1 || findings[0].Title != "IDOR" || findings[0].Severity != "High" || findings[0].Status != "confirmed" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestFindingToolDefaultsUnprovenFindingsToLikely(t *testing.T) {
	resetAgentStateForTest()
	ctx := context.Background()

	result, err := (&FindingTool{}).Execute(ctx, map[string]interface{}{
		"action":     "add",
		"title":      "Possible access control bypass",
		"severity":   "critical",
		"confidence": "med",
	})
	if err != nil || result.IsError {
		t.Fatalf("Finding.Execute failed: result=%#v err=%v", result, err)
	}
	findings := CurrentFindings()
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[0].Status != "likely" {
		t.Fatalf("status = %q, want likely", findings[0].Status)
	}
	if findings[0].Severity != "Critical" || findings[0].Confidence != "Medium" {
		t.Fatalf("severity/confidence not normalized: %#v", findings[0])
	}
}

func TestEvidenceToolWritesFile(t *testing.T) {
	resetAgentStateForTest()
	cfg := config.Default()
	cfg.Project.Path = t.TempDir()

	result, err := (&EvidenceTool{config: cfg}).Execute(context.Background(), map[string]interface{}{
		"action":  "add",
		"type":    "http",
		"title":   "raw packet",
		"content": "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n",
	})
	if err != nil || result.IsError {
		t.Fatalf("Evidence.Execute failed: result=%#v err=%v", result, err)
	}
	evidence := CurrentEvidence()
	if len(evidence) != 1 {
		t.Fatalf("evidence = %#v", evidence)
	}
	data, err := os.ReadFile(evidence[0].Path)
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	if string(data) != "GET / HTTP/1.1\r\nHost: example.test\r\n\r\n" {
		t.Fatalf("evidence content = %q", string(data))
	}
}

func TestAssessmentPlanAndSecurityCoverageTools(t *testing.T) {
	resetAgentStateForTest()
	ctx := context.Background()

	if result, err := (&AssessmentPlanTool{}).Execute(ctx, map[string]interface{}{
		"action":         "set",
		"objective":      "Assess production-like API",
		"target_profile": "authenticated multi-tenant API with webhooks",
		"test_level":     "low noise",
		"in_scope":       []interface{}{"https://api.example.test"},
		"out_of_scope":   []interface{}{"DoS testing"},
		"roles":          []interface{}{"user", "admin"},
		"rules":          "low-noise validation only",
	}); err != nil || result.IsError {
		t.Fatalf("AssessmentPlan.Execute failed: result=%#v err=%v", result, err)
	}
	plan := CurrentAssessmentPlan()
	if plan.Objective != "Assess production-like API" || plan.TestLevel != "low-noise" || len(plan.InScope) != 1 || len(plan.Roles) != 2 {
		t.Fatalf("assessment plan = %#v", plan)
	}

	if result, err := (&SecurityCoverageTool{}).Execute(ctx, map[string]interface{}{
		"action":     "add",
		"catalog_id": "WEB-AC",
		"area":       "Access control",
		"target":     "/api/users/{id}",
		"status":     "not tested",
		"reason":     "second tenant account unavailable",
	}); err != nil || result.IsError {
		t.Fatalf("SecurityCoverage.Execute failed: result=%#v err=%v", result, err)
	}
	coverage := CurrentSecurityCoverage()
	if len(coverage) != 1 || coverage[0].Status != "not-tested" || coverage[0].CatalogID != "WEB-AC" {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func TestSecurityCoverageSeedFromFeatures(t *testing.T) {
	resetAgentStateForTest()
	ctx := context.Background()
	_, _ = (&AssessmentPlanTool{}).Execute(ctx, map[string]interface{}{
		"action":   "set",
		"in_scope": []interface{}{"https://api.example.test"},
	})
	result, err := (&SecurityCoverageTool{}).Execute(ctx, map[string]interface{}{
		"action":          "seed",
		"target_features": []interface{}{"swagger", "login", "websocket"},
	})
	if err != nil || result.IsError {
		t.Fatalf("SecurityCoverage seed failed: result=%#v err=%v", result, err)
	}
	coverage := CurrentSecurityCoverage()
	if len(coverage) == 0 {
		t.Fatalf("expected seeded coverage")
	}
	for _, want := range []string{"RECON-DOCS", "WEB-AUTH", "API-WS"} {
		found := false
		for _, item := range coverage {
			if item.CatalogID == want && item.Status == "planned" && strings.Contains(item.Notes, "Evidence required:") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("seeded coverage missing %s: %#v", want, coverage)
		}
	}
}

func TestAgentStateSnapshotRoundTrip(t *testing.T) {
	resetAgentStateForTest()
	ctx := context.Background()
	_, _ = (&MissionTool{}).Execute(ctx, map[string]interface{}{"action": "set", "goal": "Assess app"})
	_, _ = (&FindingTool{}).Execute(ctx, map[string]interface{}{"action": "add", "title": "Possible IDOR"})
	_, _ = (&SecurityCoverageTool{}).Execute(ctx, map[string]interface{}{"action": "add", "catalog_id": "WEB-AC", "status": "planned"})
	_, _ = (&TodoTool{}).Execute(ctx, map[string]interface{}{"action": "add", "item": "Map endpoints"})

	data, err := ExportAgentStateJSON()
	if err != nil {
		t.Fatalf("export state: %v", err)
	}
	resetAgentStateForTest()
	if err := RestoreAgentStateJSON(data); err != nil {
		t.Fatalf("restore state: %v", err)
	}
	if CurrentMission().Goal != "Assess app" || len(CurrentFindings()) != 1 || len(CurrentSecurityCoverage()) != 1 || len(CurrentTodos()) != 1 {
		t.Fatalf("restored state mismatch: mission=%#v findings=%#v coverage=%#v todos=%#v", CurrentMission(), CurrentFindings(), CurrentSecurityCoverage(), CurrentTodos())
	}
}

func TestFindingQualityIssues(t *testing.T) {
	issues := FindingQualityIssues([]FindingRecord{{
		ID:     "F-1",
		Title:  "Confirmed without report evidence",
		Status: "confirmed",
	}})
	if len(issues) != 1 {
		t.Fatalf("issues = %#v", issues)
	}
	for _, want := range []string{"Burp Request/Response POC", "impact", "remediation", "retest method"} {
		found := false
		for _, got := range issues[0].Missing {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing quality issue %q in %#v", want, issues[0].Missing)
		}
	}
}
