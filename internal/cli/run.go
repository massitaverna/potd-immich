package cli

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/spf13/cobra"

	"github.com/massitaverna/potd-immich/internal/blocklist"
	"github.com/massitaverna/potd-immich/internal/conf"
	"github.com/massitaverna/potd-immich/internal/immich"
	"github.com/massitaverna/potd-immich/internal/potd"
	"github.com/massitaverna/potd-immich/internal/state"
)

func newRunCmd(log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Pick a new photo of the day now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := conf.Load(cmd.Flags())
			if err != nil {
				return err
			}
			return runOnce(cmd.Context(), c, log)
		},
	}
	conf.AddRunFlags(cmd.Flags())
	conf.AddDryRunFlag(cmd.Flags())
	return cmd
}

func runOnce(ctx context.Context, c conf.Config, log *slog.Logger) error {
	bl, err := blocklist.Load(c.BlocklistFile)
	if err != nil {
		return err
	}

	log.Info("blocklist loaded", "entries", len(bl.Blocklist.Blocked), "file", c.BlocklistFile)
	st, err := state.Load(c.StateFile)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	now := time.Now()
	runner := potd.Runner{API: immich.New(c.APIURL, c.APIKey), Log: log, Rand: rand.IntN}
	chosen, err := runner.Run(ctx, potd.Options{
		AlbumName:  c.AlbumName,
		BlockedIDs: bl.BlockedIDs(),
		Recent:     st.Recent(now, c.AvoidRepeatDays),
		DryRun:     c.DryRun,
	})
	if err != nil {
		return err
	}
	if c.DryRun {
		return nil
	}
	st.Record(chosen.ID, now, c.AvoidRepeatDays)
	if err := st.Save(c.StateFile); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}
