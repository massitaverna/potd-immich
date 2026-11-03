package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/massitaverna/potd-immich/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Execute(ctx, log, version); err != nil {
		log.Error("running command failed", "error", err)
		stop()
		os.Exit(1)
	}
}
