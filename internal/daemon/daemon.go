package daemon

import (
	"context"
	"log/slog"
	"time"
)

const (
	// CheckInterval bounds how late a pick runs, also after a wall-clock jump or a resume from suspend,
	// which a single long sleep would not notice. 15 minutes keeps wake-ups at 96 a day.
	CheckInterval = 15 * time.Minute
	RetryDelay    = 15 * time.Minute
)

type Schedule struct {
	Hour, Minute int
	Location     *time.Location
}

// Slot returns today's scheduled time in the schedule's location.
func (s Schedule) Slot(now time.Time) time.Time {
	local := now.In(s.Location)
	return time.Date(local.Year(), local.Month(), local.Day(), s.Hour, s.Minute, 0, 0, s.Location)
}

// Due reports whether a run should happen: today's slot has passed and no pick was made since.
func (s Schedule) Due(now, lastPick time.Time, hasLast bool) bool {
	slot := s.Slot(now)
	if now.Before(slot) {
		return false
	}
	return !hasLast || lastPick.Before(slot)
}

type Daemon struct {
	Schedule Schedule
	LastPick func() (time.Time, bool, error)
	Run      func(ctx context.Context) error
	Log      *slog.Logger
	Now      func() time.Time
}

// Loop checks every CheckInterval whether a run is due, retrying failures after RetryDelay.
func (d Daemon) Loop(ctx context.Context) {
	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()
	var notBefore time.Time
	for {
		if now := d.Now(); !now.Before(notBefore) && d.due(now) {
			if err := d.Run(ctx); err != nil {
				notBefore = now.Add(RetryDelay)
				d.Log.Error("run failed, will retry", "error", err, "retry_at", notBefore)
			}
		}
		select {
		case <-ctx.Done():
			d.Log.Info("daemon stopping")
			return
		case <-ticker.C:
		}
	}
}

func (d Daemon) due(now time.Time) bool {
	last, ok, err := d.LastPick()
	if err != nil {
		d.Log.Error("failed to read last pick", "error", err)
		return false
	}
	return d.Schedule.Due(now, last, ok)
}
