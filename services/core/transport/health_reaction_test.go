package transport

import (
	"math"
	"strings"
	"testing"
)

func TestReactionSummaryUsesFiveValidatedTrials(t *testing.T) {
	samples := []float64{242.0, 210.0, 238.0, 270.0, 225.0}
	median, mean, err := reactionSummary(samples)
	if err != nil || median != 238.0 || mean != 237.0 {
		t.Fatalf("unexpected reaction summary: median=%g mean=%g error=%v", median, mean, err)
	}
	if samples[0] != 242.0 {
		t.Fatal("the input trial order was modified")
	}
}

func TestReactionSummaryRejectsInvalidMeasurements(t *testing.T) {
	for _, samples := range [][]float64{
		nil,
		{200},
		{200, 210, 220, 230},
		{200, 210, 220, 230, 240, 250},
		{0, 200, 210, 220, 230},
		{79.9, 200, 210, 220, 230},
		{1500.1, 200, 210, 220, 230},
		{math.NaN(), 200, 210, 220, 230},
		{math.Inf(1), 200, 210, 220, 230},
	} {
		if _, _, err := reactionSummary(samples); err == nil {
			t.Fatalf("invalid reaction trial set accepted: %v", samples)
		}
	}
	if _, _, err := reactionSummary([]float64{80, 1500, 100, 200, 300}); err != nil {
		t.Fatal(err)
	}
}

func TestReactionTaskErrorsDoNotExposeClinicalClaims(t *testing.T) {
	if _, _, err := reactionSummary(nil); err == nil ||
		!strings.Contains(err.Error(), "five valid trials") {
		t.Fatal("invalid trials must be rejected with an actionable error")
	}
}
