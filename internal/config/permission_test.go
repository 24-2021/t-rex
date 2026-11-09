package config

import "testing"

func TestPermissionRulesMatchToolInputs(t *testing.T) {
	cfg := PermissionConfig{
		Deny:  []string{"Bash(command:rm -rf*)", "Write(file_path:*.env)"},
		Allow: []string{"Bash(command:git status*)", "Read"},
		Ask:   []string{"Bash"},
		Mode:  "default",
	}
	tests := []struct {
		name   string
		tool   string
		input  map[string]interface{}
		allow  bool
		reason string
	}{
		{"safe git", "Bash", map[string]interface{}{"command": "git status --short"}, true, "allowed"},
		{"destructive", "Bash", map[string]interface{}{"command": "rm -rf build"}, false, "denied"},
		{"secret file", "Write", map[string]interface{}{"file_path": ".env"}, false, "denied"},
		{"other bash", "Bash", map[string]interface{}{"command": "go test ./..."}, false, "ask"},
		{"read", "Read", map[string]interface{}{"file_path": "main.go"}, true, "allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allow, reason := cfg.IsToolAllowedInput(tt.tool, tt.input)
			if allow != tt.allow || reason != tt.reason {
				t.Fatalf("got (%v, %q), want (%v, %q)", allow, reason, tt.allow, tt.reason)
			}
		})
	}
}

func TestPlanModeBlocksMutatingToolsEvenWhenAllowed(t *testing.T) {
	cfg := PermissionConfig{Mode: "plan", Allow: []string{"Read", "Write", "Bash", "mcp__*", "CodeAuditCatalog"}}
	for _, tool := range []string{"Write", "Bash", "mcp__server__update"} {
		allowed, reason := cfg.IsToolAllowedInput(tool, map[string]interface{}{})
		if allowed || reason != "plan mode" {
			t.Fatalf("%s got (%v, %q), want blocked by plan mode", tool, allowed, reason)
		}
	}
	if allowed, reason := cfg.IsToolAllowedInput("Read", nil); !allowed || reason != "allowed" {
		t.Fatalf("Read got (%v, %q), want allowed", allowed, reason)
	}
	if allowed, reason := cfg.IsToolAllowedInput("CodeAuditCatalog", nil); !allowed || reason != "allowed" {
		t.Fatalf("CodeAuditCatalog got (%v, %q), want allowed", allowed, reason)
	}
}

func TestBypassModeSkipsAskAndDenyRules(t *testing.T) {
	cfg := PermissionConfig{
		Mode:  "bypass",
		Deny:  []string{"Write", "Bash(command:rm -rf*)"},
		Ask:   []string{"Bash", "Edit"},
		Allow: []string{"Read"},
	}

	for _, tt := range []struct {
		tool  string
		input map[string]interface{}
	}{
		{"Write", map[string]interface{}{"file_path": "main.go"}},
		{"Bash", map[string]interface{}{"command": "rm -rf build"}},
		{"Edit", map[string]interface{}{"file_path": "main.go"}},
	} {
		allowed, reason := cfg.IsToolAllowedInput(tt.tool, tt.input)
		if !allowed || reason != "allowed (bypass mode)" {
			t.Fatalf("%s got (%v, %q), want bypass allowed", tt.tool, allowed, reason)
		}
	}
}

func TestShellRiskProfilesAndHighRiskConfirmation(t *testing.T) {
	cfg := PermissionConfig{Mode: "default", Ask: []string{"Bash"}}
	if allowed, reason := cfg.IsToolAllowedInput("Bash", map[string]interface{}{"command": "go test ./..."}); allowed || reason != "ask" {
		t.Fatalf("ordinary shell command got (%v, %q), want ask", allowed, reason)
	}
	if allowed, reason := cfg.IsToolAllowedInput("Bash", map[string]interface{}{"command": "curl https://example.test"}); allowed || reason != "ask-high-risk" {
		t.Fatalf("network shell command got (%v, %q), want ask-high-risk", allowed, reason)
	}

	cfg.ShellRiskProfile = "strict"
	if allowed, reason := cfg.IsToolAllowedInput("Bash", map[string]interface{}{"command": "rm -rf build"}); allowed || reason != "denied" {
		t.Fatalf("strict shell command got (%v, %q), want denied", allowed, reason)
	}
	cfg.ShellAllowlist = []string{"rm -rf build"}
	if allowed, reason := cfg.IsToolAllowedInput("Bash", map[string]interface{}{"command": "rm -rf build"}); allowed || reason != "ask-high-risk" {
		t.Fatalf("allowlisted high-risk command got (%v, %q), want ask-high-risk", allowed, reason)
	}
}

func TestManagedRulesDisableBypass(t *testing.T) {
	for _, cfg := range []PermissionConfig{
		{Mode: "bypass", ManagedRulesOnly: true},
		{Mode: "bypass", DisableBypassMode: true},
	} {
		if allowed, reason := cfg.IsToolAllowedInput("Write", nil); allowed || reason != "denied" {
			t.Fatalf("managed bypass got (%v, %q), want denied", allowed, reason)
		}
	}
}
