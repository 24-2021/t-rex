package config

import "testing"

func TestNormalizeWorkMode(t *testing.T) {
	tests := map[string]string{
		"":              "coding",
		"default":       "coding",
		"code":          "coding",
		"fix":           "bugfix",
		"review":        "code-review",
		"audit":         "code-audit",
		"sast":          "code-audit",
		"代码审计":          "code-audit",
		"ir":            "incident-response",
		"malware":       "malware-analysis",
		"osint":         "osint",
		"ctf":           "ctf",
		"llm-security":  "ai-security",
		"agent-redteam": "ai-security",
		"pentest":       "pentest",
		"Pen-Test":      "pentest",
		"redteam":       "pentest",
		"red-team":      "pentest",
		"security":      "pentest",
		"渗透测试":          "pentest",
		"红队打点":          "pentest",
		"custom-mode":   "custom-mode",
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := NormalizeWorkMode(input); got != want {
				t.Fatalf("NormalizeWorkMode(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestIsKnownWorkMode(t *testing.T) {
	if !IsKnownWorkMode("redteam") {
		t.Fatalf("redteam alias should be known")
	}
	if !IsKnownWorkMode("incident-response") {
		t.Fatalf("incident-response should be known")
	}
	if !IsKnownWorkMode("ai-security") {
		t.Fatalf("ai-security should be known")
	}
	if !IsKnownWorkMode("代码审计") {
		t.Fatalf("code audit alias should be known")
	}
	if IsKnownWorkMode("custom-mode") {
		t.Fatalf("custom-mode should not be known")
	}
}

func TestDetectWorkModeFromPrompt(t *testing.T) {
	tests := map[string]string{
		"对 jizhicms 进行代码审计并出报告":                      "code-audit",
		"run a secure code review for this repo":     "code-audit",
		"对这个站点做授权渗透测试":                               "pentest",
		"perform recon and vulnerability assessment": "pentest",
		"帮我写代码实现上下键选择":                               "coding",
		"fix the failing tests":                      "coding",
		"你好":                                         "",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := DetectWorkModeFromPrompt(input); got != want {
				t.Fatalf("DetectWorkModeFromPrompt(%q) = %q, want %q", input, got, want)
			}
		})
	}
}
