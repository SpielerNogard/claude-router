package convert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type StreamWriter interface {
	WriteEvent(name string, data []byte) error
	Flush()
}

// Buffered because some providers interleave fragments of several calls, but Anthropic's stream allows only one open block.
type toolBuf struct {
	id   string
	name string
	args strings.Builder
}

type StreamState struct {
	w            StreamWriter
	model        string
	id           string
	started      bool
	textOpen     bool
	textIndex    int
	nextIndex    int
	tools        []*toolBuf
	toolSlot     map[int]*toolBuf
	finishReason string
	toolUsed     bool
	inTokens     int64
	outTokens    int64
	skipped      int
	providerErr  string
	done         bool
}

func NewStreamState(requestedModel string, w StreamWriter) *StreamState {
	return &StreamState{w: w, model: requestedModel, toolSlot: map[int]*toolBuf{}}
}

func (s *StreamState) Usage() (int64, int64) { return s.inTokens, s.outTokens }

// Number of chunks that could not be parsed.
func (s *StreamState) Skipped() int { return s.skipped }

// Error message the provider sent in the stream, if any.
func (s *StreamState) ProviderError() string { return s.providerErr }

func (s *StreamState) Feed(line []byte) error {
	if s.done {
		return nil
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
		return nil
	}
	payload := bytes.TrimSpace(line[len("data:"):])
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return nil
	}

	if msg := providerErrorMessage(payload); msg != "" {
		s.providerErr = msg
	}

	var chunk OpenAIResponse
	if err := json.Unmarshal(payload, &chunk); err != nil {
		s.skipped++
		return nil
	}
	if chunk.Usage != nil {
		s.inTokens = chunk.Usage.PromptTokens
		s.outTokens = chunk.Usage.CompletionTokens
	}
	if chunk.ID != "" && s.id == "" {
		s.id = chunk.ID
	}
	if err := s.ensureStarted(); err != nil {
		return err
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	c := chunk.Choices[0]
	if c.FinishReason != "" {
		s.finishReason = c.FinishReason
	}
	if c.Delta.Content != "" {
		if err := s.flushTools(); err != nil {
			return err
		}
		if err := s.openText(); err != nil {
			return err
		}
		if err := s.event("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": s.textIndex,
			"delta": map[string]string{"type": "text_delta", "text": c.Delta.Content},
		}); err != nil {
			return err
		}
	}
	for _, tc := range c.Delta.ToolCalls {
		if err := s.closeText(); err != nil {
			return err
		}
		s.bufferToolCall(tc)
	}
	return nil
}

// Providers send in-stream errors as {"error":{"message":"..."}} or {"error":"..."}; both must be recognized.
func providerErrorMessage(payload []byte) string {
	var probe struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(payload, &probe) != nil || len(probe.Error) == 0 {
		return ""
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(probe.Error, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	var str string
	if json.Unmarshal(probe.Error, &str) == nil && str != "" {
		return str
	}
	return ""
}

// A new non-empty id starts a new call even if the provider omits index and everything lands in slot 0.
func (s *StreamState) bufferToolCall(tc OpenAIToolCall) {
	buf, ok := s.toolSlot[tc.Index]
	if ok && tc.ID != "" && buf.id != "" && tc.ID != buf.id {
		ok = false
	}
	if !ok {
		buf = &toolBuf{id: tc.ID, name: tc.Function.Name}
		s.toolSlot[tc.Index] = buf
		s.tools = append(s.tools, buf)
	}
	if buf.id == "" {
		buf.id = tc.ID
	}
	if buf.name == "" {
		buf.name = tc.Function.Name
	}
	buf.args.WriteString(tc.Function.Arguments)
}

func (s *StreamState) flushTools() error {
	if len(s.tools) > 0 {
		s.toolUsed = true
	}
	for _, t := range s.tools {
		idx := s.nextIndex
		id := t.id
		if id == "" {
			id = fmt.Sprintf("toolu_%d", idx)
		}
		if err := s.event("content_block_start", map[string]any{
			"type": "content_block_start", "index": idx,
			"content_block": map[string]any{
				"type": "tool_use", "id": id, "name": t.name, "input": map[string]any{},
			},
		}); err != nil {
			return err
		}
		s.nextIndex++
		if args := t.args.String(); args != "" {
			if err := s.event("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": idx,
				"delta": map[string]string{"type": "input_json_delta", "partial_json": args},
			}); err != nil {
				return err
			}
		}
		if err := s.event("content_block_stop", map[string]any{
			"type": "content_block_stop", "index": idx,
		}); err != nil {
			return err
		}
	}
	s.tools = nil
	s.toolSlot = map[int]*toolBuf{}
	return nil
}

func (s *StreamState) ensureStarted() error {
	if s.started {
		return nil
	}
	if err := s.event("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": firstNonEmpty(s.id, "msg_router"), "type": "message", "role": "assistant",
			"model": s.model, "content": []any{}, "stop_reason": nil,
			"usage": map[string]int64{"input_tokens": 0, "output_tokens": 0},
		},
	}); err != nil {
		return err
	}
	s.started = true
	return nil
}

func (s *StreamState) openText() error {
	if s.textOpen {
		return nil
	}
	idx := s.nextIndex
	if err := s.event("content_block_start", map[string]any{
		"type": "content_block_start", "index": idx,
		"content_block": map[string]string{"type": "text", "text": ""},
	}); err != nil {
		return err
	}
	s.textOpen = true
	s.textIndex = idx
	s.nextIndex++
	return nil
}

func (s *StreamState) closeText() error {
	if !s.textOpen {
		return nil
	}
	if err := s.event("content_block_stop", map[string]any{
		"type": "content_block_stop", "index": s.textIndex,
	}); err != nil {
		return err
	}
	s.textOpen = false
	return nil
}

// Also cleanly closes a stream aborted mid-block; idempotent.
func (s *StreamState) Finish() error {
	if s.done {
		return nil
	}
	if err := s.ensureStarted(); err != nil {
		return err
	}
	if err := s.closeText(); err != nil {
		return err
	}
	if err := s.flushTools(); err != nil {
		return err
	}
	s.done = true
	stopReason := StopReason(s.finishReason)
	// Force tool_use when tool blocks came with stop, but not on max_tokens: a truncated call is not complete.
	if s.toolUsed && stopReason != "max_tokens" {
		stopReason = "tool_use"
	}
	if err := s.event("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]int64{"output_tokens": s.outTokens},
	}); err != nil {
		return err
	}
	return s.event("message_stop", map[string]any{"type": "message_stop"})
}

func (s *StreamState) event(name string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := s.w.WriteEvent(name, b); err != nil {
		return err
	}
	s.w.Flush()
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
