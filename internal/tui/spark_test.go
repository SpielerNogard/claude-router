package tui

import (
	"math"
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestResampleSums(t *testing.T) {
	if got := resample([]float64{1, 2, 3, 4}, 2); !reflect.DeepEqual(got, []float64{3, 7}) {
		t.Errorf("got %v", got)
	}
}

func TestResampleShorterThanWidth(t *testing.T) {
	if got := resample([]float64{1, 2, 3}, 10); len(got) != 3 {
		t.Errorf("got %v", got)
	}
}

func TestSparklineScales(t *testing.T) {
	if got := sparkline([]float64{0, 4, 8}, 10); got != "▁▅█" {
		t.Errorf("got %q", got)
	}
}

func TestSparklineAllZero(t *testing.T) {
	if got := sparkline([]float64{0, 0, 0}, 10); got != "▁▁▁" {
		t.Errorf("got %q", got)
	}
}

func TestSparklineGap(t *testing.T) {
	if got := []rune(sparkline([]float64{1, math.NaN(), 1}, 10)); len(got) != 3 || got[1] != ' ' {
		t.Errorf("got %q", string(got))
	}
}

func TestSparklineWidth7d(t *testing.T) {
	vals := make([]float64, 56)
	for i := range vals {
		vals[i] = float64(i)
	}
	if w := lipgloss.Width(sparkline(vals, 20)); w != 20 {
		t.Errorf("width %d, want 20", w)
	}
}

func TestHitSeries(t *testing.T) {
	got := hitSeries([]int64{100, 0}, []int64{300, 0}, []int64{100, 0}, 2)
	if len(got) != 2 || got[0] != 0.6 || !math.IsNaN(got[1]) {
		t.Errorf("got %v", got)
	}
}
