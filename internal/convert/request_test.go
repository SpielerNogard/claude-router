package convert

import (
	"encoding/json"
	"strings"
	"testing"
)

func parse(t *testing.T, body string) *AnthropicRequest {
	t.Helper()
	var a AnthropicRequest
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("unparsable input: %v", err)
	}
	return &a
}

func TestSystemBecomesFirstMessage(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,"system":"You are helpful.",
	  "messages":[{"role":"user","content":"hello"}]}`)
	o, err := ToOpenAIRequest(a, "deepseek-chat")
	if err != nil {
		t.Fatal(err)
	}
	if o.Model != "deepseek-chat" {
		t.Errorf("Model = %q, want deepseek-chat", o.Model)
	}
	if len(o.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(o.Messages))
	}
	if o.Messages[0].Role != "system" || o.Messages[0].Content != "You are helpful." {
		t.Errorf("first message = %+v", o.Messages[0])
	}
}

func TestSystemAsBlockArrayIsJoined(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,
	  "system":[{"type":"text","text":"Part one."},{"type":"text","text":"Part two."}],
	  "messages":[{"role":"user","content":"hello"}]}`)
	o, err := ToOpenAIRequest(a, "m")
	if err != nil {
		t.Fatal(err)
	}
	if o.Messages[0].Content != "Part one.\n\nPart two." {
		t.Errorf("system blocks joined incorrectly: %q", o.Messages[0].Content)
	}
}

func TestToolUseBecomesToolCalls(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,"messages":[
	  {"role":"user","content":"what time is it"},
	  {"role":"assistant","content":[
	    {"type":"text","text":"Let me check."},
	    {"type":"tool_use","id":"toolu_1","name":"clock","input":{"tz":"UTC"}}]}]}`)
	o, err := ToOpenAIRequest(a, "m")
	if err != nil {
		t.Fatal(err)
	}
	last := o.Messages[len(o.Messages)-1]
	if last.Role != "assistant" || last.Content != "Let me check." {
		t.Errorf("assistant message = %+v", last)
	}
	if len(last.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(last.ToolCalls))
	}
	if last.ToolCalls[0].ID != "toolu_1" || last.ToolCalls[0].Function.Name != "clock" {
		t.Errorf("ToolCall = %+v", last.ToolCalls[0])
	}
	if last.ToolCalls[0].Function.Arguments != `{"tz":"UTC"}` {
		t.Errorf("Arguments = %q, must be a JSON string", last.ToolCalls[0].Function.Arguments)
	}
}

func TestToolResultBecomesToolRole(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,"messages":[
	  {"role":"user","content":[
	    {"type":"tool_result","tool_use_id":"toolu_1","content":"12:00"}]}]}`)
	o, err := ToOpenAIRequest(a, "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(o.Messages))
	}
	m := o.Messages[0]
	if m.Role != "tool" || m.ToolCallID != "toolu_1" || m.Content != "12:00" {
		t.Errorf("tool_result translated incorrectly: %+v", m)
	}
}

func TestMultipleToolResultsBecomeSeparateMessages(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,"messages":[
	  {"role":"user","content":[
	    {"type":"tool_result","tool_use_id":"t1","content":"a"},
	    {"type":"tool_result","tool_use_id":"t2","content":"b"}]}]}`)
	o, _ := ToOpenAIRequest(a, "m")
	if len(o.Messages) != 2 {
		t.Fatalf("two tool_results must yield two tool messages, got %d", len(o.Messages))
	}
}

func TestStreamRequestsUsage(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,"stream":true,
	  "messages":[{"role":"user","content":"hi"}]}`)
	o, _ := ToOpenAIRequest(a, "m")
	if o.StreamOptions == nil || !o.StreamOptions.IncludeUsage {
		t.Error("streaming must set stream_options.include_usage, otherwise token counts are missing")
	}
}

func TestNullContentProducesNoMessage(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,"messages":[
	  {"role":"user","content":null},
	  {"role":"user","content":"real"}]}`)
	o, err := ToOpenAIRequest(a, "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Messages) != 1 {
		t.Fatalf("null content must not produce an empty message, got %d: %+v", len(o.Messages), o.Messages)
	}
	if o.Messages[0].Content != "real" {
		t.Errorf("Messages[0] = %+v", o.Messages[0])
	}
}

func TestMixedToolResultAndTextKeepsBlockOrder(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,"messages":[
	  {"role":"user","content":[
	    {"type":"tool_result","tool_use_id":"t1","content":"result"},
	    {"type":"text","text":"and now continue"}]}]}`)
	o, err := ToOpenAIRequest(a, "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(o.Messages), o.Messages)
	}
	if o.Messages[0].Role != "tool" {
		t.Errorf("the tool_result came first and must stay first, got %+v", o.Messages[0])
	}
	if o.Messages[1].Role != "user" || o.Messages[1].Content != "and now continue" {
		t.Errorf("Messages[1] = %+v", o.Messages[1])
	}
}

func TestToolsWithoutParametersGetObjectDefaults(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,"messages":[{"role":"user","content":"hi"}],
	  "tools":[
	    {"name":"bare","description":"no schema","input_schema":{"type":"object"}},
	    {"name":"full","description":"schema",
	     "input_schema":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}]}`)
	o, err := ToOpenAIRequest(a, "m")
	if err != nil {
		t.Fatal(err)
	}
	// LM Studio returns 400 for parameters without properties.
	raw, err := json.Marshal(o.Tools)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"properties":{}`) {
		t.Errorf("empty properties missing: %s", raw)
	}
	if strings.Contains(string(raw), `"parameters":null`) {
		t.Errorf("parameters must not be null: %s", raw)
	}
	withSchema := o.Tools[1].Function.Parameters.(map[string]any)
	if _, ok := withSchema["properties"].(map[string]any)["path"]; !ok {
		t.Errorf("existing schema was overwritten: %+v", withSchema)
	}
	if req, ok := withSchema["required"].([]any); !ok || len(req) != 1 {
		t.Errorf("required was lost: %+v", withSchema)
	}
}

func TestMalformedSystemIsAnError(t *testing.T) {
	a := parse(t, `{"model":"x","max_tokens":100,"system":42,
	  "messages":[{"role":"user","content":"hi"}]}`)
	if _, err := ToOpenAIRequest(a, "m"); err == nil {
		t.Fatal("an unparsable system prompt must yield an error, not vanish silently")
	}
}
