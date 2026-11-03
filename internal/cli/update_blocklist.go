package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/massitaverna/potd-immich/internal/blocklist"
	"github.com/massitaverna/potd-immich/internal/conf"
	"github.com/massitaverna/potd-immich/internal/immich"
)

func newUpdateBlocklistCmd(log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "update-blocklist",
		Short: "Interactively add newly named people to the blocklist",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := conf.Load(cmd.Flags())
			if err != nil {
				return err
			}
			return updateBlocklist(cmd.Context(), c, log, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

func updateBlocklist(ctx context.Context, c conf.Config, log *slog.Logger, in io.Reader, out io.Writer) error {
	old, err := blocklist.Load(c.BlocklistFile)
	if err != nil {
		return err
	}

	people, err := immich.New(c.APIURL, c.APIKey).People(ctx)
	if err != nil {
		return fmt.Errorf("fetching people from Immich API: %w", err)
	}

	updated, err := blocklist.Sync(old, people, blocklist.Prompter(in, out), log, time.Now())
	if err != nil {
		return err
	}

	if err := blocklist.Save(c.BlocklistFile, updated); err != nil {
		return fmt.Errorf("save blocklist: %w", err)
	}

	log.Info("blocklist updated", "file", c.BlocklistFile,
		"blocked", len(updated.Blocklist.Blocked), "catalog", len(updated.Blocklist.Catalog))

	return nil
}
