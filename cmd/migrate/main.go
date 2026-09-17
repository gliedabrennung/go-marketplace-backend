package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	url := os.Getenv("MIGRATE_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		return errors.New("MIGRATE_DATABASE_URL or DATABASE_URL must be set")
	}
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	return postgres.Migrate(ctx, url, migrations.FS, command, os.Stdout)
}
