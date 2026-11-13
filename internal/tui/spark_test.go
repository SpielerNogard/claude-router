package tui

import (
	"math"
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestResampleAverages(t *testing.T) {
	if got := resample([]float64{1, 2, 3, 4}, 2); !reflect.DeepEqual(got, []float64{1.5, 3.5}) {
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

func TestSparklineConstantIsFlat(t *testing.T) {
	vals := make([]float64, 57)
	for i := range vals {
		vals[i] = 1
	}
	got := []rune(sparkline(vals, 24))
	for _, r := range got {
		if r != got[0] {
			t.Fatalf("constant input drew %q", string(got))
		}
	}
}

func TestSparklineNegativeNoPanic(t *testing.T) {
	_ = sparkline([]float64{-1, 2}, 5)
}
