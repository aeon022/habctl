package models

import "testing"

func TestStreakMilestone(t *testing.T) {
	for _, streak := range []int{7, 14, 21, 30, 60, 100} {
		if ms := StreakMilestone(streak); ms == "" {
			t.Errorf("StreakMilestone(%d) = \"\", want a message", streak)
		}
	}
	for _, streak := range []int{0, 1, 6, 8, 13, 101} {
		if ms := StreakMilestone(streak); ms != "" {
			t.Errorf("StreakMilestone(%d) = %q, want \"\"", streak, ms)
		}
	}
}

func TestProgressBar(t *testing.T) {
	if got := ProgressBar(0, 0, 4); got != "[░░░░]" {
		t.Errorf("zero total: got %q, want %q", got, "[░░░░]")
	}
	if got := ProgressBar(4, 4, 4); got != "[████]" {
		t.Errorf("full bar: got %q, want %q", got, "[████]")
	}
	// 1/8 of a width-12 bar is 1.5 blocks — math.Round rounds this up to 2,
	// where the old integer-division copies (1*12/8 = 1) truncated to 1.
	// This is the deliberate rounding-behavior change called out in the
	// commit that unified the three progressBar copies.
	if got := ProgressBar(1, 8, 12); got != "[██░░░░░░░░░░]" {
		t.Errorf("rounding: got %q, want %q", got, "[██░░░░░░░░░░]")
	}
}
