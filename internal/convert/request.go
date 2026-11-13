package convert

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ToOpenAIRequest(a *AnthropicRequest, upstreamModel string) (*OpenAIRequest, error) {
	o := &OpenAIRequest{
		Model:       upstreamModel,
		Stream:      a.Stream,
		MaxTokens:   a.MaxTokens,
		Temperature: a.Temperature,
	}
	if a.Stream {
		o.StreamOptions = &StreamOptions{IncludeUsage: true}
	}
	sys, err := systemText(a.System)
	if err != nil {
		return nil, err
	}
	if sys != "" {
		o.Messages = append(o.Messages, OpenAIMessage{Role: "system", Content: sys})
	}
	for _, m := range a.Messages {
		blocks, err := decodeContent(m.Content)
		if err != nil {
			return nil, fmt.Errorf("message content: %w", err)
		}
		msgs, err := convertMessage(m.Role, blocks)
		if err != nil {
			return nil, err
		}
		o.Messages = append(o.Messages, msgs...)
	}
	for _, t := range a.Tools {
		params, err := toolParameters(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("tool %s: input_schema: %w", t.Name, err)
		}
		o.Tools = append(o.Tools, OpenAITool{
			Type:     "function",
			Function: OpenAIFunction{Name: t.Name, Description: t.Description, Parameters: params},
		})
	}
	return o, nil
}

// Stricter validators (e.g. LM Studio, which returns 400) require type and properties, though the OpenAI spec makes them optional.
func toolParameters(raw json.RawMessage) (map[string]any, error) {
	params := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, err
		}
	}
	if _, ok := params["type"]; !ok {
		params["type"] = "object"
	}
	if _, ok := params["properties"]; !ok {
		params["properties"] = map[string]any{}
	}
	return params, nil
}

// tool_result must directly follow the assistant tool_calls message, so blocks are emitted in order, not collected.
func convertMessage(role string, blocks []ContentBlock) ([]OpenAIMessage, error) {
	var out []OpenAIMessage
	var texts []string
	var calls []OpenAIToolCall

	flush := func() {
		if len(texts) == 0 && len(calls) == 0 {
			return
		}
		out = append(out, OpenAIMessage{Role: role, Content: strings.Join(texts, "\n"), ToolCalls: calls})
		texts, calls = nil, nil
	}

	for _, b := range blocks {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "tool_use":
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			calls = append(calls, OpenAIToolCall{
				ID: b.ID, Type: "function",
				Function: OpenAIFunctionCall{Name: b.Name, Arguments: args},
			})
		case "tool_result":
			flush()
			out = append(out, OpenAIMessage{
				Role: "tool", ToolCallID: b.ToolUseID, Content: toolResultText(b.Content),
			})
		case "thinking", "redacted_thinking":
			// no counterpart in the OpenAI format
		}
	}
	flush()
	return out, nil
}

func systemText(raw json.RawMessage) (string, error) {
	blocks, err := decodeContent(raw)
	if err != nil {
		return "", fmt.Errorf("system: %w", err)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// tool_result.content is a string or block array; both are joined into text.
func toolResultText(raw json.RawMessage) string {
	blocks, err := decodeContent(raw)
	if err != nil {
		return string(raw)
	}
	var parts []string
	for _, b := range blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
