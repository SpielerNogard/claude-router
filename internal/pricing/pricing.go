package pricing

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed prices.json
var raw []byte

type Price struct {
	InputPerToken      float64
	OutputPerToken     float64
	CacheReadPerToken  float64
	CacheWritePerToken float64
}

type entry struct {
	Input      *float64 `json:"input_cost_per_token"`
	Output     *float64 `json:"output_cost_per_token"`
	CacheRead  float64  `json:"cache_read_input_token_cost"`
	CacheWrite float64  `json:"cache_creation_input_token_cost"`
}

var (
	once  sync.Once
	table map[string]Price
)

func load() {
	var m map[string]entry
	if err := json.Unmarshal(raw, &m); err != nil {
		table = map[string]Price{}
		return
	}
	table = make(map[string]Price, len(m))
	for k, v := range m {
		if v.Input == nil || v.Output == nil {
			continue
		}
		table[strings.ToLower(k)] = Price{*v.Input, *v.Output, v.CacheRead, v.CacheWrite}
	}
}

// Tries the full name first, then without provider prefixes — "openrouter/openai/gpt-4o" ends at "gpt-4o".
func Lookup(model string) *Price {
	once.Do(load)
	name := strings.ToLower(model)
	for {
		if p, ok := table[name]; ok {
			return &p
		}
		i := strings.Index(name, "/")
		if i < 0 {
			return nil
		}
		name = name[i+1:]
	}
}

func Cost(p *Price, in, out, cacheRead, cacheWrite int64) *float64 {
	if p == nil {
		return nil
	}
	c := float64(in)*p.InputPerToken +
		float64(out)*p.OutputPerToken +
		float64(cacheRead)*p.CacheReadPerToken +
		float64(cacheWrite)*p.CacheWritePerToken
	return &c
}
