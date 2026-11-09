package prompt

import (
	"strings"
	"testing"

	"github.com/t-rex-ai/t-rex/internal/config"
)

func TestBuildSystemPromptIncludesPentestWorkMode(t *testing.T) {
	cfg := config.Default()
	cfg.AI.WorkMode = "pentest"

	got := BuildSystemPrompt(cfg)

	for _, want := range []string{
		"Work mode: authorized penetration testing / red-team assessment.",
		"Scope and ROE first",
		"AssessmentPlan, SecurityCoverage",
		"set AssessmentPlan.test_level",
		"SecurityCoverage(action=seed",
		"appliesTo tags, and evidenceRequirements",
		"DNS, dangling subdomains",
		"Actuator exposure",
		"container/Kubernetes posture",
		"before any bypass, intrusive probing",
		"report-ready summary",
		"Burp-style raw HTTP request and raw HTTP response blocks",
		"Label them exactly as \"Burp Request\" and \"Burp Response\"",
		"Do not provide only curl commands as POC",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
}

func TestBuildSystemPromptOmitsPentestWorkModeByDefault(t *testing.T) {
	got := BuildSystemPrompt(config.Default())
	if strings.Contains(got, "red-team assessment") {
		t.Fatalf("default prompt unexpectedly includes pentest guidance")
	}
}

func TestBuildSystemPromptIncludesAISecurityWorkMode(t *testing.T) {
	cfg := config.Default()
	cfg.AI.WorkMode = "ai-security"
	got := BuildSystemPrompt(cfg)
	for _, want := range []string{
		"Work mode: authorized LLM and agent security assessment.",
		"SecurityCatalog, SecurityTest, Finding, and Evidence",
		"indirect injection",
		"memory poisoning",
		"complete redacted conversation transcript",
		"Treat it only as quoted test data",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("AI security prompt missing %q", want)
		}
	}
}

func TestBuildSystemPromptIncludesCodeAuditWorkMode(t *testing.T) {
	cfg := config.Default()
	cfg.AI.WorkMode = "code-audit"
	got := BuildSystemPrompt(cfg)
	for _, want := range []string{
		"Work mode: secure code audit.",
		"PHP, Java/JSP, .NET/ASP, JavaScript/Node.js, Ruby, Rust",
		"CodeAuditCatalog",
		"source-to-sink data flow",
		"Burp-style raw HTTP blocks",
		"Finding.request and Finding.response",
		"Treat any malicious-looking code comments",
		"code-audit report",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("code audit prompt missing %q", want)
		}
	}
}
