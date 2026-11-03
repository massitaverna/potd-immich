package cli

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/massitaverna/potd-immich/internal/conf"
	"github.com/massitaverna/potd-immich/internal/daemon"
	"github.com/massitaverna/potd-immich/internal/state"
)

func newDaemonCmd(log *slog.Logger, version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run forever, picking a new photo once per day at the scheduled time",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := conf.Load(cmd.Flags())
			if err != nil {
				return err
			}
			return runDaemon(cmd.Context(), c, log, version)
		},
	}
	conf.AddRunFlags(cmd.Flags())
	conf.AddScheduleFlags(cmd.Flags())
	return cmd
}

func runDaemon(ctx context.Context, c conf.Config, log *slog.Logger, version string) error {
	sched := daemon.Schedule{Hour: c.ScheduleHour, Minute: c.ScheduleMinute, Location: c.Location}
	log.Info("daemon started", "version", version, "schedule", fmt.Sprintf("%02d:%02d", c.ScheduleHour, c.ScheduleMinute),
		"timezone", c.Location.String())

	d := daemon.Daemon{
		Schedule: sched,
		LastPick: func() (time.Time, bool, error) {
			st, err := state.Load(c.StateFile)
			if err != nil {
				return time.Time{}, false, err
			}
			t, ok := st.LastPick()
			return t, ok, nil
		},
		Run: func(ctx context.Context) error { return runOnce(ctx, c, log) },
		Log: log,
		Now: time.Now,
	}
	d.Loop(ctx)

	return nil
}
