package daemon

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestDue(t *testing.T) {
	zurich, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Fatal(err)
	}
	s := Schedule{Hour: 6, Minute: 0, Location: zurich}
	at := func(day, hour, min int) time.Time { return time.Date(2026, 10, day, hour, min, 0, 0, zurich) }

	cases := []struct {
		name    string
		now     time.Time
		last    time.Time
		hasLast bool
		want    bool
	}{
		{"before slot", at(17, 5, 59), at(16, 6, 0), true, false},
		{"at slot", at(17, 6, 0), at(16, 6, 0), true, true},
		{"catch up after boot", at(17, 9, 30), at(16, 6, 0), true, true},
		{"already ran today", at(17, 9, 30), at(17, 6, 1), true, false},
		{"manual run before slot", at(17, 6, 5), at(17, 5, 0), true, true},
		{"first ever run", at(17, 6, 5), time.Time{}, false, true},
		{"missed several days", at(20, 7, 0), at(16, 6, 0), true, true},
	}
	for _, c := range cases {
		if got := s.Due(c.now, c.last, c.hasLast); got != c.want {
			t.Errorf("%s: due = %v, want %v", c.name, got, c.want)
		}
	}
}

// Slot must be built from the wall-clock date, not as midnight + duration: DST-change days
// last 23h or 25h, so midnight + 6h30 would land at 07:30 or 05:30 local time on those days.
func TestSlotIsWallClockTimeOnDSTChanges(t *testing.T) {
	zurich, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		t.Fatal(err)
	}
	s := Schedule{Hour: 6, Minute: 30, Location: zurich}
	utc := func(month time.Month, day, hour, min int) time.Time {
		return time.Date(2026, month, day, hour, min, 0, 0, time.UTC)
	}

	cases := []struct {
		name string
		day  time.Time
		want time.Time // 06:30 local, as UTC
	}{
		{"normal day, CET (+01:00)", utc(time.March, 28, 12, 0), utc(time.March, 28, 5, 30)},
		{"DST starts, 23h day, CEST (+02:00)", utc(time.March, 29, 12, 0), utc(time.March, 29, 4, 30)},
		{"DST ends, 25h day, CET (+01:00)", utc(time.October, 25, 12, 0), utc(time.October, 25, 5, 30)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.Slot(c.day); !got.Equal(c.want) {
				t.Errorf("slot = %v, want %v", got, c.want.In(zurich))
			}
		})
	}
}

func TestLoopRunsImmediatelyWhenDueAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runs := 0
	d := Daemon{
		Schedule: Schedule{Hour: 6, Location: time.UTC},
		LastPick: func() (time.Time, bool, error) { return time.Time{}, false, nil },
		Run: func(context.Context) error {
			runs++
			cancel()
			return nil
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC) },
	}
	done := make(chan struct{})
	go func() { d.Loop(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not stop after cancel")
	}
	if runs != 1 {
		t.Errorf("runs = %d, want 1", runs)
	}
}
