package ratebudget

import "time"

// Clock is the time source a Limiter runs on. Timers take an absolute
// instant rather than a duration, so a waiter that computed its deadline
// cannot be pushed later by time passing before the timer exists.
type Clock interface {
	Now() time.Time
	NewTimer(at time.Time) Timer
}

// Timer fires once, at or after the instant it was created for.
type Timer interface {
	C() <-chan time.Time
	Stop()
}

// SystemClock is the wall clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

func (SystemClock) NewTimer(at time.Time) Timer {
	return systemTimer{time.NewTimer(time.Until(at))}
}

type systemTimer struct{ t *time.Timer }

func (s systemTimer) C() <-chan time.Time { return s.t.C }

func (s systemTimer) Stop() { s.t.Stop() }
