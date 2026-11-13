package convert

import (
	"encoding/json"
	"testing"
)

func TestStopReasonTable(t *testing.T) {
	for in, want := range map[string]string{
		"stop":           "end_turn",
		"tool_calls":     "tool_use",
		"length":         "max_tokens",
		"content_filter": "stop_sequence",
		"":               "end_turn",
		"whatever":       "end_turn",
	} {
		if got := StopReason(in); got != want {
			t.Errorf("StopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolCallsBecomeToolUseBlocks(t *testing.T) {
	o := &OpenAIResponse{
		ID: "chatcmpl-1", Model: "deepseek-chat",
		Choices: []OpenAIChoice{{
			Message: OpenAIMessage{
				Role: "assistant", Content: "Let me check.",
				ToolCalls: []OpenAIToolCall{{
					ID: "call_1", Type: "function",
					Function: OpenAIFunctionCall{Name: "clock", Arguments: `{"tz":"UTC"}`},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: &OpenAIUsage{PromptTokens: 10, CompletionTokens: 4},
	}
	a := ToAnthropicResponse(o, "sub:opus-5")
	if a.Type != "message" || a.Role != "assistant" {
		t.Errorf("wrong envelope: %+v", a)
	}
	if a.Model != "sub:opus-5" {
		t.Errorf("Model = %q — the response must carry the name the client requested", a.Model)
	}
	if len(a.Content) != 2 {
		t.Fatalf("expected text plus tool_use block, got %d", len(a.Content))
	}
	if a.Content[0].Type != "text" || a.Content[0].Text != "Let me check." {
		t.Errorf("Block 0 = %+v", a.Content[0])
	}
	if a.Content[1].Type != "tool_use" || a.Content[1].ID != "call_1" || a.Content[1].Name != "clock" {
		t.Errorf("Block 1 = %+v", a.Content[1])
	}
	if string(a.Content[1].Input) != `{"tz":"UTC"}` {
		t.Errorf("Input = %s, must be real JSON, not a string", a.Content[1].Input)
	}
	if a.StopReason != "tool_use" {
		t.Errorf("StopReason = %q", a.StopReason)
	}
	if a.Usage.InputTokens != 10 || a.Usage.OutputTokens != 4 {
		t.Errorf("Usage = %+v", a.Usage)
	}
}

func TestToolCallsForceToolUseEvenWithFinishReasonStop(t *testing.T) {
	o := &OpenAIResponse{Choices: []OpenAIChoice{{
		Message: OpenAIMessage{ToolCalls: []OpenAIToolCall{{
			ID: "call_1", Function: OpenAIFunctionCall{Name: "clock", Arguments: `{}`},
		}}},
		FinishReason: "stop", // Ollama and others report this despite tool calls
	}}}
	a := ToAnthropicResponse(o, "m")
	if a.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want tool_use — otherwise Claude Code does not run the tool", a.StopReason)
	}
}

func TestToolCallsDoNotOverrideMaxTokens(t *testing.T) {
	o := &OpenAIResponse{Choices: []OpenAIChoice{{
		Message: OpenAIMessage{ToolCalls: []OpenAIToolCall{{
			ID: "call_1", Function: OpenAIFunctionCall{Name: "clock", Arguments: `{}`},
		}}},
		FinishReason: "length", // call was cut off midway
	}}}
	a := ToAnthropicResponse(o, "m")
	if a.StopReason != "max_tokens" {
		t.Errorf("StopReason = %q, want max_tokens — the tool call is incomplete", a.StopReason)
	}
}

func TestBrokenToolArgumentsBecomeEmptyObject(t *testing.T) {
	o := &OpenAIResponse{Choices: []OpenAIChoice{{
		Message: OpenAIMessage{ToolCalls: []OpenAIToolCall{{
			ID: "c1", Function: OpenAIFunctionCall{Name: "f", Arguments: "this is not JSON"},
		}}},
		FinishReason: "tool_calls",
	}}}
	a := ToAnthropicResponse(o, "m")
	if string(a.Content[0].Input) != "{}" {
		t.Errorf("unparsable arguments must become {}, got %s", a.Content[0].Input)
	}
}

func TestEmptyResponseStillHasContentArray(t *testing.T) {
	a := ToAnthropicResponse(&OpenAIResponse{}, "m")
	if a.Content == nil {
		t.Fatal("content must never be null, Claude Code expects an array")
	}
	b, _ := json.Marshal(a)
	if !json.Valid(b) {
		t.Fatal("response is not valid JSON")
	}
}

func TestNonObjectToolArgumentsBecomeEmptyObject(t *testing.T) {
	for _, args := range []string{"42", "[1,2]", `"a string"`, "true", "null"} {
		o := &OpenAIResponse{Choices: []OpenAIChoice{{
			Message: OpenAIMessage{ToolCalls: []OpenAIToolCall{{
				ID: "c1", Function: OpenAIFunctionCall{Name: "f", Arguments: args},
			}}},
			FinishReason: "tool_calls",
		}}}
		a := ToAnthropicResponse(o, "m")
		if got := string(a.Content[0].Input); got != "{}" {
			t.Errorf("Arguments %q: Input = %s, want {} — Anthropic requires an object there", args, got)
		}
	}
}

func TestErrorBodyHasAnthropicShape(t *testing.T) {
	var v struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(ErrorBody("not_found_error", "no provider"), &v); err != nil {
		t.Fatal(err)
	}
	if v.Type != "error" || v.Error.Type != "not_found_error" || v.Error.Message != "no provider" {
		t.Errorf("wrong error shape: %+v", v)
	}
}

func TestStatusToKind(t *testing.T) {
	for status, want := range map[int]string{
		400: "invalid_request_error",
		401: "authentication_error",
		403: "permission_error",
		404: "not_found_error",
		429: "rate_limit_error",
		500: "api_error",
		502: "api_error",
	} {
		if got := StatusToKind(status); got != want {
			t.Errorf("StatusToKind(%d) = %q, want %q", status, got, want)
		}
	}
}
