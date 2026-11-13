package tui

import (
	"math"
	"strings"
)

var sparkGlyphs = []rune("▁▂▃▄▅▆▇█")

// resample averages consecutive buckets so a long range fits the card width; sums would turn uneven group sizes into fake spikes.
func resample(vals []float64, width int) []float64 {
	if width <= 0 || len(vals) <= width {
		return vals
	}
	out := make([]float64, width)
	for i := range out {
		group := vals[i*len(vals)/width : (i+1)*len(vals)/width]
		for _, v := range group {
			out[i] += v
		}
		out[i] /= float64(len(group))
	}
	return out
}

// sparkline scales to the largest visible value; NaN marks a gap.
func sparkline(vals []float64, width int) string {
	vals = resample(vals, width)
	top := 0.0
	for _, v := range vals {
		if !math.IsNaN(v) {
			top = math.Max(top, v)
		}
	}
	var b strings.Builder
	for _, v := range vals {
		switch {
		case math.IsNaN(v):
			b.WriteRune(' ')
		case top == 0:
			b.WriteRune(sparkGlyphs[0])
		default:
			b.WriteRune(sparkGlyphs[max(0, int(math.Round(v/top*float64(len(sparkGlyphs)-1))))])
		}
	}
	return b.String()
}

// hitSeries resamples the raw token counts before dividing, so merged buckets are weighted by traffic.
func hitSeries(in, cr, cw []int64, width int) []float64 {
	rin, rcr, rcw := resample(ints(in), width), resample(ints(cr), width), resample(ints(cw), width)
	out := make([]float64, len(rin))
	for i := range out {
		if total := rin[i] + rcr[i] + rcw[i]; total > 0 {
			out[i] = rcr[i] / total
		} else {
			out[i] = math.NaN()
		}
	}
	return out
}

func ints(v []int64) []float64 {
	out := make([]float64, len(v))
	for i, n := range v {
		out[i] = float64(n)
	}
	return out
}
