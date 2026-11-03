package cli

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/massitaverna/potd-immich/internal/conf"
)

const appName = "potd-immich"

// Execute runs the command line given in os.Args.
func Execute(ctx context.Context, log *slog.Logger, version string) error {
	return NewRootCmd(log, version).ExecuteContext(ctx)
}

// NewRootCmd builds a fresh command tree, so tests never share flag state.
func NewRootCmd(log *slog.Logger, version string) *cobra.Command {
	root := &cobra.Command{
		Use:           appName,
		Short:         "Keep an Immich album filled with one photo of the day",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	conf.AddConnectionFlags(root.PersistentFlags())
	root.AddCommand(newRunCmd(log), newDaemonCmd(log, version), newUpdateBlocklistCmd(log))
	return root
}
