package events

import (
	"testing"
	"time"
)

// simulate plays changes (offsets from zero) against a coalescer whose
// window lasts interval, in virtual time, and returns when it emitted.
func simulate(changes []time.Duration, interval time.Duration) []time.Duration {
	var c coalescer
	var emitted []time.Duration
	deadline := time.Duration(-1) // no window open
	tick := func(now time.Duration) {
		if c.tick() {
			emitted = append(emitted, now)
			deadline = now + interval
		} else {
			deadline = -1
		}
	}
	for _, at := range changes {
		for deadline >= 0 && deadline <= at {
			tick(deadline)
		}
		if c.change() {
			emitted = append(emitted, at)
			deadline = at + interval
		}
	}
	for deadline >= 0 {
		tick(deadline)
	}
	return emitted
}

func TestALoneChangeIsEmittedAtOnce(t *testing.T) {
	got := simulate([]time.Duration{0}, 250*time.Millisecond)
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("emitted at %v, want [0]", got)
	}
}

// Spec daemon-api, "Message burst": 40 messages within one second.
func TestABurstIsEmittedAtMostOncePerInterval(t *testing.T) {
	const interval = 250 * time.Millisecond
	var changes []time.Duration
	for i := range 40 {
		changes = append(changes, time.Duration(i)*25*time.Millisecond) // 0 .. 975 ms
	}
	got := simulate(changes, interval)

	want := []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond, 750 * time.Millisecond, time.Second}
	if len(got) != len(want) {
		t.Fatalf("emitted at %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("emitted at %v, want %v", got, want)
		}
	}
	inBurst := 0
	for _, at := range got {
		if at < time.Second {
			inBurst++
		}
	}
	if inBurst > 4 {
		t.Errorf("%d emissions during the one-second burst, want at most 4", inBurst)
	}
}

func TestAQuietIntervalClosesTheWindow(t *testing.T) {
	const interval = 250 * time.Millisecond
	got := simulate([]time.Duration{0, time.Second}, interval)
	if len(got) != 2 || got[1] != time.Second {
		t.Fatalf("emitted at %v, want [0 1s]: a change after a quiet interval leads again", got)
	}
}
