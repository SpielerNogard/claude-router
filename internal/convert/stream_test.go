package convert

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type capture struct {
	names []string
	datas [][]byte
}

func (c *capture) WriteEvent(name string, data []byte) error {
	c.names = append(c.names, name)
	c.datas = append(c.datas, append([]byte(nil), data...))
	return nil
}
func (c *capture) Flush() {}

func (c *capture) seq() string { return strings.Join(c.names, " ") }

func (c *capture) eventsOfType(name string) []map[string]any {
	var out []map[string]any
	for i, n := range c.names {
		if n != name {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(c.datas[i], &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

func run(t *testing.T, file string) *capture {
	t.Helper()
	f, err := os.Open("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c := &capture{}
	s := NewStreamState("sub:opus-5", c)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if err := s.Feed(sc.Bytes()); err != nil {
			t.Fatalf("Feed(%q): %v", sc.Text(), err)
		}
	}
	if err := s.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return c
}

func TestTextOnlyStreamHasCorrectEventOrder(t *testing.T) {
	c := run(t, "text_only.sse")
	want := "message_start content_block_start content_block_delta content_block_delta content_block_stop message_delta message_stop"
	if c.seq() != want {
		t.Errorf("event order:\n got: %s\nwant: %s", c.seq(), want)
	}
}

func TestToolCallSplitAcrossChunksIsReassembled(t *testing.T) {
	c := run(t, "tool_call_split.sse")
	starts := c.eventsOfType("content_block_start")
	if len(starts) != 2 {
		t.Fatalf("expected text and tool_use block, got %d starts", len(starts))
	}
	toolIndex := float64(-1)
	for _, st := range starts {
		cb, ok := st["content_block"].(map[string]any)
		if ok && cb["type"] == "tool_use" {
			toolIndex, _ = st["index"].(float64)
		}
	}
	if toolIndex == -1 {
		t.Fatal("no tool_use block found")
	}
	var args strings.Builder
	for i, n := range c.names {
		if n != "content_block_delta" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(c.datas[i], &m); err != nil {
			continue
		}
		if idx, _ := m["index"].(float64); idx != toolIndex {
			continue
		}
		if dl, ok := m["delta"].(map[string]any); ok {
			if p, ok := dl["partial_json"].(string); ok {
				args.WriteString(p)
			}
		}
	}
	if args.String() != `{"tz":"UTC"}` {
		t.Errorf("reassembled arguments = %q, want {\"tz\":\"UTC\"}", args.String())
	}
	di := indexOf(c, "message_delta")
	if di == -1 {
		t.Fatal("no message_delta event found")
	}
	if !strings.Contains(string(c.datas[di]), "tool_use") {
		t.Error("stop_reason must be tool_use")
	}
}

func TestBlockIndicesAreSequentialAndBalanced(t *testing.T) {
	for _, f := range []string{"text_only.sse", "tool_call_split.sse", "two_tool_calls.sse"} {
		t.Run(f, func(t *testing.T) {
			c := run(t, f)
			opened := map[float64]int{}
			closed := map[float64]int{}
			startPos := map[float64]int{}
			stopPos := map[float64]int{}
			maxIndex := float64(-1)
			for i, n := range c.names {
				var m map[string]any
				if json.Unmarshal(c.datas[i], &m) != nil {
					continue
				}
				idx, ok := m["index"].(float64)
				if !ok {
					continue
				}
				switch n {
				case "content_block_start":
					opened[idx]++
					startPos[idx] = i
					if idx > maxIndex {
						maxIndex = idx
					}
				case "content_block_stop":
					closed[idx]++
					stopPos[idx] = i
				}
			}
			for idx := float64(0); idx <= maxIndex; idx++ {
				if opened[idx] != 1 {
					t.Errorf("index %v opened %dx, want exactly 1x", idx, opened[idx])
				}
				if closed[idx] != 1 {
					t.Errorf("index %v closed %dx, want exactly 1x", idx, closed[idx])
				}
				if opened[idx] == 1 && closed[idx] == 1 && startPos[idx] >= stopPos[idx] {
					t.Errorf("index %v: content_block_start (position %d) is not before content_block_stop (position %d)", idx, startPos[idx], stopPos[idx])
				}
			}
		})
	}
}

func TestTwoToolCallsBecomeTwoBlocks(t *testing.T) {
	c := run(t, "two_tool_calls.sse")
	if got := len(c.eventsOfType("content_block_start")); got != 2 {
		t.Errorf("expected 2 blocks, got %d", got)
	}
}

func TestProviderErrorRecognizesObjectAndStringForms(t *testing.T) {
	for _, tc := range []struct{ payload, want string }{
		{`data: {"error":{"message":"rate limited"}}`, "rate limited"},
		{`data: {"error":"rate limited"}`, "rate limited"},
	} {
		c := &capture{}
		s := NewStreamState("m", c)
		if err := s.Feed([]byte(tc.payload)); err != nil {
			t.Fatal(err)
		}
		if got := s.ProviderError(); got != tc.want {
			t.Errorf("payload=%q: ProviderError() = %q, want %q", tc.payload, got, tc.want)
		}
	}
}

func TestStreamForcesToolUseEvenWithFinishReasonStop(t *testing.T) {
	c := &capture{}
	s := NewStreamState("m", c)
	for _, line := range []string{
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	} {
		if err := s.Feed([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	di := indexOf(c, "message_delta")
	if di == -1 {
		t.Fatal("no message_delta event found")
	}
	if !strings.Contains(string(c.datas[di]), "tool_use") {
		t.Errorf("stop_reason must be tool_use even though the provider reported stop: %s", c.datas[di])
	}
}

func TestStreamDoesNotOverrideMaxTokens(t *testing.T) {
	c := &capture{}
	s := NewStreamState("m", c)
	for _, line := range []string{
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
	} {
		if err := s.Feed([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	di := indexOf(c, "message_delta")
	if di == -1 {
		t.Fatal("no message_delta event found")
	}
	if !strings.Contains(string(c.datas[di]), "max_tokens") {
		t.Errorf("stop_reason must stay max_tokens, the tool call is truncated: %s", c.datas[di])
	}
	if strings.Contains(string(c.datas[di]), "tool_use") {
		t.Errorf("stop_reason must not be tool_use for a truncated call: %s", c.datas[di])
	}
}

func TestUsageIsCaptured(t *testing.T) {
	f, err := os.Open("testdata/text_only.sse")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c := &capture{}
	s := NewStreamState("m", c)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s.Feed(sc.Bytes())
	}
	s.Finish()
	in, out := s.Usage()
	if in != 5 || out != 2 {
		t.Errorf("Usage = %d/%d, want 5/2", in, out)
	}
}

func TestAbortMidBlockStillCloses(t *testing.T) {
	c := &capture{}
	s := NewStreamState("m", c)
	s.Feed([]byte(`data: {"id":"x","choices":[{"index":0,"delta":{"content":"half"}}]}`))
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(c.seq(), "content_block_stop message_delta message_stop") {
		t.Errorf("aborted stream must be closed cleanly, sequence was: %s", c.seq())
	}
}

func TestInterleavedToolIndicesStayInTheirBlocks(t *testing.T) {
	c := &capture{}
	s := NewStreamState("m", c)
	for _, line := range []string{
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"f","arguments":"{\"x\":"}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"g","arguments":"{\"y\":"}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"2}"}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	} {
		if err := s.Feed([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}

	args := map[float64]string{}
	blockID := map[float64]string{}
	for i, n := range c.names {
		var m map[string]any
		if json.Unmarshal(c.datas[i], &m) != nil {
			continue
		}
		idx, _ := m["index"].(float64)
		switch n {
		case "content_block_start":
			if cb, ok := m["content_block"].(map[string]any); ok {
				blockID[idx], _ = cb["id"].(string)
			}
		case "content_block_delta":
			if d, ok := m["delta"].(map[string]any); ok {
				if p, ok := d["partial_json"].(string); ok {
					args[idx] += p
				}
			}
		}
	}
	for idx, want := range map[float64]string{0: `{"x":1}`, 1: `{"y":2}`} {
		if args[idx] != want {
			t.Errorf("block %v: arguments = %q, want %q — interleaved fragments ended up in the wrong block", idx, args[idx], want)
		}
	}
	if blockID[0] != "a" || blockID[1] != "b" {
		t.Errorf("block IDs = %v, want map[0:a 1:b]", blockID)
	}

	type span struct{ start, stop int }
	spans := map[float64]*span{}
	var posDeltas []struct {
		idx float64
		pos int
	}
	for i, n := range c.names {
		var m map[string]any
		if json.Unmarshal(c.datas[i], &m) != nil {
			continue
		}
		idx, ok := m["index"].(float64)
		if !ok {
			continue
		}
		switch n {
		case "content_block_start":
			spans[idx] = &span{start: i, stop: -1}
		case "content_block_stop":
			if sp := spans[idx]; sp != nil {
				sp.stop = i
			}
		case "content_block_delta":
			posDeltas = append(posDeltas, struct {
				idx float64
				pos int
			}{idx, i})
		}
	}
	for _, d := range posDeltas {
		sp := spans[d.idx]
		if sp == nil {
			t.Fatalf("delta on index %v without matching content_block_start", d.idx)
		}
		if sp.stop < 0 {
			t.Fatalf("block %v was never closed", d.idx)
		}
		if d.pos < sp.start || d.pos > sp.stop {
			t.Errorf("delta on index %v is at position %d, but the block only spans %d to %d — "+
				"a fragment was written into an already closed block",
				d.idx, d.pos, sp.start, sp.stop)
		}
	}
}

func TestMissingIndexWithTwoToolCallsKeepsThemApart(t *testing.T) {
	c := &capture{}
	s := NewStreamState("m", c)
	for _, line := range []string{
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"id":"a","type":"function","function":{"name":"f","arguments":"{\"x\":1}"}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"id":"b","type":"function","function":{"name":"g","arguments":"{\"y\":2}"}}]}}]}`,
		`data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	} {
		s.Feed([]byte(line))
	}
	s.Finish()
	if got := len(c.eventsOfType("content_block_start")); got != 2 {
		t.Errorf("expected 2 blocks, got %d — without index both calls land in the same slot", got)
	}
}

func indexOf(c *capture, name string) int {
	for i, n := range c.names {
		if n == name {
			return i
		}
	}
	return -1
}
