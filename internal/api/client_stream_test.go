package api

import (
	"strings"
	"testing"
)

func TestProcessAnthropicStreamTextToolUsage(t *testing.T) {
	client := &Client{}
	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","role":"assistant","model":"claude-test","usage":{"input_tokens":10,"output_tokens":1}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_"}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"path\":\"main.go\"}"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var text strings.Builder
	var starts, ends []ContentBlock
	var completes []*APIResponse
	err := client.processSSEStream(
		strings.NewReader(stream),
		func(delta string) { text.WriteString(delta) },
		func(block *ContentBlock) { starts = append(starts, *block) },
		func(block *ContentBlock) { ends = append(ends, *block) },
		func(resp *APIResponse) { completes = append(completes, resp) },
		nil,
	)
	if err != nil {
		t.Fatalf("processSSEStream returned error: %v", err)
	}
	if text.String() != "Hello world" {
		t.Fatalf("text = %q, want Hello world", text.String())
	}
	if len(starts) != 1 || starts[0].Name != "Read" {
		t.Fatalf("tool starts = %#v, want one Read start", starts)
	}
	if len(ends) != 1 || ends[0].Name != "Read" {
		t.Fatalf("tool ends = %#v, want one Read end", ends)
	}
	input, ok := ends[0].Input.(map[string]interface{})
	if !ok || input["file_path"] != "main.go" {
		t.Fatalf("tool input = %#v, want file_path main.go", ends[0].Input)
	}
	if len(completes) != 1 {
		t.Fatalf("complete count = %d, want 1", len(completes))
	}
	resp := completes[0]
	if resp.StopReason != "tool_use" {
		t.Fatalf("stop reason = %q, want tool_use", resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 8 {
		t.Fatalf("usage = %#v, want input=10 output=8", resp.Usage)
	}
	if len(resp.Content) != 2 || resp.Content[0].Text != "Hello world" || resp.Content[1].Name != "Read" {
		t.Fatalf("content = %#v, want text plus Read tool_use", resp.Content)
	}
}

func TestProcessAnthropicStreamError(t *testing.T) {
	client := &Client{}
	var callbackErr error
	err := client.processSSEStream(
		strings.NewReader(`data: {"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`+"\n"),
		nil,
		nil,
		nil,
		nil,
		func(err error) { callbackErr = err },
	)
	if err == nil {
		t.Fatal("expected stream error")
	}
	if callbackErr == nil {
		t.Fatal("expected onError callback")
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("error = %q, want bad request", err.Error())
	}
}

func TestProcessOAIStreamTextMultiToolUsage(t *testing.T) {
	client := &Client{}
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"Hi "}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"there"}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Read","arguments":""}},{"index":1,"id":"call_2","type":"function","function":{"name":"Grep","arguments":""}}]}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"file_"}},{"index":1,"function":{"arguments":"{\"pattern\":\"foo\""}}]}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"path\":\"main.go\"}"}},{"index":1,"function":{"arguments":",\"path\":\".\"}"}}]}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	var text strings.Builder
	var starts, ends []ContentBlock
	var completes []*APIResponse
	err := client.processOAIStream(
		strings.NewReader(stream),
		func(delta string) { text.WriteString(delta) },
		func(block *ContentBlock) { starts = append(starts, *block) },
		func(block *ContentBlock) { ends = append(ends, *block) },
		func(resp *APIResponse) { completes = append(completes, resp) },
		nil,
	)
	if err != nil {
		t.Fatalf("processOAIStream returned error: %v", err)
	}
	if text.String() != "Hi there" {
		t.Fatalf("text = %q, want Hi there", text.String())
	}
	if len(starts) != 2 || starts[0].Name != "Read" || starts[1].Name != "Grep" {
		t.Fatalf("tool starts = %#v, want Read and Grep", starts)
	}
	if len(ends) != 2 || ends[0].Name != "Read" || ends[1].Name != "Grep" {
		t.Fatalf("tool ends = %#v, want Read and Grep", ends)
	}
	readInput, ok := ends[0].Input.(map[string]interface{})
	if !ok || readInput["file_path"] != "main.go" {
		t.Fatalf("Read input = %#v, want file_path main.go", ends[0].Input)
	}
	grepInput, ok := ends[1].Input.(map[string]interface{})
	if !ok || grepInput["pattern"] != "foo" || grepInput["path"] != "." {
		t.Fatalf("Grep input = %#v, want pattern foo path .", ends[1].Input)
	}
	if len(completes) != 1 {
		t.Fatalf("complete count = %d, want 1", len(completes))
	}
	resp := completes[0]
	if resp.StopReason != "tool_use" {
		t.Fatalf("stop reason = %q, want tool_use", resp.StopReason)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("usage = %#v, want input=12 output=5", resp.Usage)
	}
	if len(resp.Content) != 3 || resp.Content[0].Text != "Hi there" || resp.Content[1].Name != "Read" || resp.Content[2].Name != "Grep" {
		t.Fatalf("content = %#v, want text plus two tool_use blocks", resp.Content)
	}
}

func TestProcessOAIStreamCumulativeTextDelta(t *testing.T) {
	client := &Client{}
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"开"}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"开始"}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"开始信息收集"}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	var text strings.Builder
	var completes []*APIResponse
	err := client.processOAIStream(
		strings.NewReader(stream),
		func(delta string) { text.WriteString(delta) },
		nil,
		nil,
		func(resp *APIResponse) { completes = append(completes, resp) },
		nil,
	)
	if err != nil {
		t.Fatalf("processOAIStream returned error: %v", err)
	}
	if text.String() != "开始信息收集" {
		t.Fatalf("text = %q, want 开始信息收集", text.String())
	}
	if len(completes) != 1 || len(completes[0].Content) != 1 || completes[0].Content[0].Text != "开始信息收集" {
		t.Fatalf("complete content = %#v", completes)
	}
}

func TestProcessOAIStreamOverlappingCJKTextDelta(t *testing.T) {
	client := &Client{}
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"开始"}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"始信息"}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{"content":"息收集"}}]}`,
		`data: {"id":"chatcmpl_1","model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	var text strings.Builder
	err := client.processOAIStream(
		strings.NewReader(stream),
		func(delta string) { text.WriteString(delta) },
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("processOAIStream returned error: %v", err)
	}
	if text.String() != "开始信息收集" {
		t.Fatalf("text = %q, want 开始信息收集", text.String())
	}
}

func TestProcessOAIStreamError(t *testing.T) {
	client := &Client{}
	var callbackErr error
	err := client.processOAIStream(
		strings.NewReader(`data: {"error":{"message":"quota exceeded","type":"rate_limit_error"}}`+"\n"),
		nil,
		nil,
		nil,
		nil,
		func(err error) { callbackErr = err },
	)
	if err == nil {
		t.Fatal("expected stream error")
	}
	if callbackErr == nil {
		t.Fatal("expected onError callback")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("error = %q, want quota exceeded", err.Error())
	}
}
