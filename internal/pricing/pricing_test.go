package pricing

import "testing"

func TestLookupKnownModel(t *testing.T) {
	p := Lookup("gpt-4o")
	if p == nil {
		t.Fatal("gpt-4o must be in the price table")
	}
	if p.InputPerToken <= 0 || p.OutputPerToken <= 0 {
		t.Errorf("implausible prices: %+v", p)
	}
}

func TestLookupUnknownModelReturnsNil(t *testing.T) {
	if p := Lookup("made-up-model-xyz"); p != nil {
		t.Fatalf("unknown model must return nil, not %+v", p)
	}
}

func TestCostNilPriceIsNilNotZero(t *testing.T) {
	if c := Cost(nil, 1000, 500, 0, 0); c != nil {
		t.Fatalf("unknown price must return nil, not %v", *c)
	}
}

func TestCostAddsUp(t *testing.T) {
	p := &Price{InputPerToken: 1e-6, OutputPerToken: 2e-6}
	c := Cost(p, 1_000_000, 1_000_000, 0, 0)
	if c == nil {
		t.Fatal("expected a value")
	}
	if *c != 3.0 {
		t.Errorf("Cost = %v, want 3.0", *c)
	}
}

func TestLookupStripsProviderPrefix(t *testing.T) {
	if Lookup("openrouter/openai/gpt-4o") == nil {
		t.Error("provider prefixes must be stripped on lookup")
	}
}

func TestPartialPriceEntryIsUnknown(t *testing.T) {
	// oci/cohere.embed-multilingual-image-v3.0 has input_cost_per_token but no output_cost_per_token
	if p := Lookup("oci/cohere.embed-multilingual-image-v3.0"); p != nil {
		t.Fatalf("an entry without output price must count as unknown, not as %+v", p)
	}
}
