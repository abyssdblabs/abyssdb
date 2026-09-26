package nulltime

import (
	"testing"
	"time"
)

func TestLightTimesArePlausible(t *testing.T) {
	now := time.Now()
	cases := []struct {
		region   string
		min, max time.Duration
	}{
		{"us-east-1", 0, 0},
		{"luna-shackleton-1", 1200 * time.Millisecond, 1300 * time.Millisecond},
		{"mars-olympus-1", 3 * time.Minute, 23 * time.Minute},
		{"jupiter-europa-1", 32 * time.Minute, 55 * time.Minute},
		{"voyager-1", 23 * time.Hour, 25 * time.Hour},
	}
	for _, c := range cases {
		r, err := Lookup(c.region)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.OneWayLightTime(now); got < c.min || got > c.max {
			t.Errorf("%s: OWLT %s outside [%s, %s]", c.region, got, c.min, c.max)
		}
	}
}

func TestMarsOpposition2025(t *testing.T) {
	// Mars was at opposition (closest approach) in January 2025, ~0.64 AU.
	r, _ := Lookup("mars-olympus-1")
	d := r.Distance(time.Date(2025, 1, 16, 0, 0, 0, 0, time.UTC)) / au
	if d < 0.5 || d > 0.8 {
		t.Fatalf("Mars distance at 2025 opposition = %.2f AU, want ~0.64", d)
	}
}
