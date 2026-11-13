package convert

import (
	"encoding/json"
	"strings"
)

var stopReasons = map[string]string{
	"stop":           "end_turn",
	"tool_calls":     "tool_use",
	"function_call":  "tool_use",
	"length":         "max_tokens",
	"content_filter": "stop_sequence",
}

func StopReason(finishReason string) string {
	if r, ok := stopReasons[finishReason]; ok {
		return r
	}
	return "end_turn"
}

// requestedModel is the client's alias; it must be echoed back or Claude Code treats the response as another model's.
func ToAnthropicResponse(o *OpenAIResponse, requestedModel string) *AnthropicResponse {
	a := &AnthropicResponse{
		ID: o.ID, Type: "message", Role: "assistant",
		Model: requestedModel, Content: []ContentBlock{},
	}
	if a.ID == "" {
		a.ID = "msg_router"
	}
	if len(o.Choices) > 0 {
		c := o.Choices[0]
		if c.Message.Content != "" {
			a.Content = append(a.Content, ContentBlock{Type: "text", Text: c.Message.Content})
		}
		for _, tc := range c.Message.ToolCalls {
			a.Content = append(a.Content, ContentBlock{
				Type: "tool_use", ID: tc.ID, Name: tc.Function.Name,
				Input: rawJSONOrEmpty(tc.Function.Arguments),
			})
		}
		a.StopReason = StopReason(c.FinishReason)
		// Force tool_use when tool blocks came with stop, but not on max_tokens: a truncated call is not complete.
		if hasToolUse(a.Content) && a.StopReason != "max_tokens" {
			a.StopReason = "tool_use"
		}
	} else {
		a.StopReason = "end_turn"
	}
	if o.Usage != nil {
		a.Usage = AnthropicUsage{InputTokens: o.Usage.PromptTokens, OutputTokens: o.Usage.CompletionTokens}
	}
	return a
}

func hasToolUse(blocks []ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

// OpenAI sends arguments as a string, Anthropic expects an object; anything else becomes {} rather than an invalid response.
func rawJSONOrEmpty(s string) json.RawMessage {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	return json.RawMessage("{}")
}
