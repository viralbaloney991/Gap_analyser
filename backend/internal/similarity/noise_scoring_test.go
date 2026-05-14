package similarity

import (
	"math"
	"testing"
)

func TestBurstScore(t *testing.T) {
	cases := []struct {
		name   string
		counts [4]int
		want   float64
	}{
		{"all zeros", [4]int{0, 0, 0, 0}, 0},
		{"even distribution", [4]int{7, 14, 21, 28}, 1.0},
		{"concentrated burst", [4]int{8, 1, 1, 9}, 3.556},
		{"below expected", [4]int{1, 3, 5, 8}, 0.5},
	}
	for _, tc := range cases {
		got := burstScore(tc.counts)
		if math.Abs(got-tc.want) > 0.01 {
			t.Errorf("burstScore %s (%v) = %.3f, want %.3f", tc.name, tc.counts, got, tc.want)
		}
	}
}

func TestPeriodicityScore(t *testing.T) {
	cases := []struct {
		name   string
		counts [4]int
		want   float64
		inf    bool
	}{
		{"perfect even rate", [4]int{4, 8, 12, 16}, 0.0, false},
		{"slightly uneven", [4]int{5, 8, 12, 16}, 0.25, false},
		{"too few 14d counts", [4]int{3, 1, 0, 1}, 0, true},
		{"completely zero 14d", [4]int{0, 0, 0, 0}, 0, true},
	}
	for _, tc := range cases {
		got := periodicityScore(tc.counts)
		if tc.inf {
			if !math.IsInf(got, 1) {
				t.Errorf("periodicityScore %s (%v) = %.3f, want +Inf", tc.name, tc.counts, got)
			}
		} else if math.Abs(got-tc.want) > 0.01 {
			t.Errorf("periodicityScore %s (%v) = %.3f, want %.3f", tc.name, tc.counts, got, tc.want)
		}
	}
}

func TestClassifyNoisePattern(t *testing.T) {
	cases := []struct {
		name   string
		counts [4]int
		want   string
	}{
		{"high_volume — 30d count > 10", [4]int{3, 4, 5, 11}, "high_volume"},
		{"burst — concentrated in recent 7d", [4]int{8, 1, 1, 9}, "burst"},
		{"periodic — even rate low total", [4]int{2, 4, 6, 8}, "periodic"},
		{"accelerating — recent 50%+ above expected", [4]int{4, 2, 2, 7}, "accelerating"},
		{"persistent — fires every week low total", [4]int{2, 3, 5, 8}, "persistent"},
		{"no pattern — below threshold flat", [4]int{0, 1, 1, 2}, ""},
		{"all zeros — no pattern", [4]int{0, 0, 0, 0}, ""},
	}
	for _, tc := range cases {
		got := classifyNoisePattern(tc.counts)
		if got != tc.want {
			t.Errorf("classifyNoisePattern %s (%v) = %q, want %q", tc.name, tc.counts, got, tc.want)
		}
	}
}

func TestAccelerationScore(t *testing.T) {
	cases := []struct {
		name   string
		counts [4]int
		want   float64
	}{
		{"all zeros", [4]int{0, 0, 0, 0}, 0},
		{"gradual ramp", [4]int{4, 2, 2, 7}, 2.286},
	}
	for _, tc := range cases {
		got := accelerationScore(tc.counts)
		if math.Abs(got-tc.want) > 0.01 {
			t.Errorf("accelerationScore %s (%v) = %.3f, want %.3f", tc.name, tc.counts, got, tc.want)
		}
	}
}
