package similarity

import "math"

const (
	burstScoreThreshold        = 2.5
	periodicityScoreThreshold  = 0.2
	accelerationScoreThreshold = 1.5
	minPeriodicityWindow       = 2 // counts[1] (14d) must be >= 2 to compute periodicity
)

// burstScore returns the ratio of the 7-day count to the expected weekly share
// of the 30-day total. > burstScoreThreshold means recent-week firing was
// concentrated — burst pattern.
func burstScore(counts [4]int) float64 {
	if counts[3] == 0 {
		return 0
	}
	expected := float64(counts[3]) / 4.0
	return float64(counts[0]) / expected
}

// periodicityScore returns how close the 7-day count is to half the 14-day count.
// A score near 0 means the alert fires at a perfectly even (periodic) rate.
// Returns +Inf when counts[1] < minPeriodicityWindow to avoid
// division by near-zero.
func periodicityScore(counts [4]int) float64 {
	if counts[1] < minPeriodicityWindow {
		return math.Inf(1)
	}
	expected := float64(counts[1]) / 2.0
	return math.Abs(float64(counts[0])/expected - 1.0)
}

// accelerationScore is numerically identical to burstScore. It is used with the
// lower accelerationScoreThreshold (1.5 vs 2.5) to catch gradual acceleration
// before it reaches burst level.
func accelerationScore(counts [4]int) float64 {
	return burstScore(counts)
}

// classifyNoisePattern returns the first matching noise pattern for the given
// 4-window counts [7d, 14d, 21d, 30d]. Priority: high_volume → burst →
// periodic → accelerating → persistent. Returns "" when no pattern matches.
func classifyNoisePattern(counts [4]int) string {
	switch {
	case counts[3] > behavioralNoiseThreshold:
		return "high_volume"
	case burstScore(counts) > burstScoreThreshold && counts[3] <= behavioralNoiseThreshold:
		return "burst"
	case periodicityScore(counts) < periodicityScoreThreshold && counts[1] >= minPeriodicityWindow:
		return "periodic"
	case accelerationScore(counts) > accelerationScoreThreshold && counts[3] <= behavioralNoiseThreshold:
		return "accelerating"
	case counts[0] > 0 && counts[1] > counts[0] && counts[2] > counts[1] && counts[3] <= behavioralNoiseThreshold:
		return "persistent"
	default:
		return ""
	}
}
