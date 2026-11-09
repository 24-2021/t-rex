package api

import (
	"strings"
	"testing"
)

func TestParseProviderTreatsUnknownNamesAsOpenAICompatible(t *testing.T) {
	for _, name := range []string{"openai", "deepseek", "qwen", "moonshot", "minimax", "siliconflow", "openrouter", "custom-gateway"} {
		if got := ParseProvider(name); got != ProviderOpenAICompatible {
			t.Fatalf("ParseProvider(%q) = %q, want openai-compatible", name, got)
		}
	}
	for _, name := range []string{"", "anthropic", "claude"} {
		if got := ParseProvider(name); got != ProviderAnthropic {
			t.Fatalf("ParseProvider(%q) = %q, want anthropic", name, got)
		}
	}
}

func TestNormalizeOpenAICompatibleBaseURL(t *testing.T) {
	tests := map[string]string{
		"":                        "https://api.openai.com/v1",
		"https://api.minimax.io":  "https://api.minimax.io/v1",
		"https://api.minimax.io/": "https://api.minimax.io/v1",
		"https://dashscope.aliyuncs.com/compatible-mode/v1/":     "https://dashscope.aliyuncs.com/compatible-mode/v1",
		"https://gateway.example.test/custom/openai-compatible/": "https://gateway.example.test/custom/openai-compatible",
	}
	for input, want := range tests {
		if got := normalizeAPIBaseURL(ProviderOpenAICompatible, input); got != want {
			t.Fatalf("normalizeAPIBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEndpointURLAvoidsDuplicateChatCompletions(t *testing.T) {
	if got := endpointURL("https://api.minimax.io/v1", "chat/completions"); got != "https://api.minimax.io/v1/chat/completions" {
		t.Fatalf("endpointURL root = %q", got)
	}
	if got := endpointURL("https://api.minimax.io/v1/chat/completions", "chat/completions"); got != "https://api.minimax.io/v1/chat/completions" {
		t.Fatalf("endpointURL full path = %q", got)
	}
}

func TestAPIStatusErrorMentionsURLForHTML404(t *testing.T) {
	err := apiStatusError(404, "https://api.minimax.io/messages", "<html><body>nginx</body></html>")
	if err == nil || !strings.Contains(err.Error(), "https://api.minimax.io/messages") || !strings.Contains(err.Error(), "OpenAI-compatible") {
		t.Fatalf("unexpected error: %v", err)
	}
}
