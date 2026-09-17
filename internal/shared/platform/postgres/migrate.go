package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

type migrationCommand func(ctx context.Context, p *goose.Provider, out io.Writer) error

var migrationCommands = map[string]migrationCommand{
	"up":      migrateUp,
	"down":    migrateDown,
	"reset":   migrateReset,
	"status":  migrateStatus,
	"version": migrateVersion,
}

func Migrate(ctx context.Context, url string, fsys fs.FS, command string, out io.Writer) (err error) {
	run, ok := migrationCommands[command]
	if !ok {
		return fmt.Errorf("unknown migrate command %q (use up, down, reset, status, version)", command)
	}

	connConfig, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse migration database url: %w", err)
	}
	db := stdlib.OpenDB(*connConfig)
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close migration db: %w", closeErr))
		}
	}()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	return run(ctx, provider, out)
}

func migrateUp(ctx context.Context, p *goose.Provider, out io.Writer) error {
	results, err := p.Up(ctx)
	printResults(out, "applied", results)
	if err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func migrateDown(ctx context.Context, p *goose.Provider, out io.Writer) error {
	r, err := p.Down(ctx)
	if err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	printResults(out, "rolled back", []*goose.MigrationResult{r})
	return nil
}

func migrateReset(ctx context.Context, p *goose.Provider, out io.Writer) error {
	results, err := p.DownTo(ctx, 0)
	printResults(out, "rolled back", results)
	if err != nil {
		return fmt.Errorf("migrate reset: %w", err)
	}
	return nil
}

func migrateStatus(ctx context.Context, p *goose.Provider, out io.Writer) error {
	statuses, err := p.Status(ctx)
	if err != nil {
		return fmt.Errorf("migrate status: %w", err)
	}
	for _, s := range statuses {
		fmt.Fprintf(out, "%-10s %s\n", s.State, s.Source.Path)
	}
	return nil
}

func migrateVersion(ctx context.Context, p *goose.Provider, out io.Writer) error {
	v, err := p.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("migrate version: %w", err)
	}
	fmt.Fprintf(out, "%d\n", v)
	return nil
}

func printResults(out io.Writer, verb string, results []*goose.MigrationResult) {
	for _, r := range results {
		if r == nil || r.Source == nil {
			continue
		}
		fmt.Fprintf(out, "%s %s in %s\n", verb, r.Source.Path, r.Duration)
	}
}
