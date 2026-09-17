//go:build integration

package testdb

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/migrations"
)

var databaseURL string

func Main(m *testing.M) int {
	ctx := context.Background()
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		databaseURL = url
		if err := postgres.Migrate(ctx, url, migrations.FS, "up", io.Discard); err != nil {
			fmt.Fprintf(os.Stderr, "testdb: migrate: %v\n", err)
			return 1
		}
		return m.Run()
	}

	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("marketplace"),
		tcpostgres.WithUsername("marketplace"),
		tcpostgres.WithPassword("marketplace"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testdb: start postgres: %v\n", err)
		return 1
	}
	defer func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			fmt.Fprintf(os.Stderr, "testdb: terminate postgres: %v\n", err)
		}
	}()

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "testdb: connection string: %v\n", err)
		return 1
	}
	if err := postgres.Migrate(ctx, url, migrations.FS, "up", io.Discard); err != nil {
		fmt.Fprintf(os.Stderr, "testdb: migrate: %v\n", err)
		return 1
	}
	databaseURL = url
	return m.Run()
}

func URL() string {
	return databaseURL
}

func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := postgres.Connect(ctx, postgres.Config{URL: databaseURL, MaxConns: 64})
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func Truncate(t testing.TB, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	quoted := make([]string, len(tables))
	for i, table := range tables {
		schema, name, found := strings.Cut(table, ".")
		if !found {
			t.Fatalf("testdb: table %q must be schema-qualified", table)
		}
		quoted[i] = pgx.Identifier{schema, name}.Sanitize()
	}
	if _, err := pool.Exec(context.Background(), "TRUNCATE "+strings.Join(quoted, ", ")+" RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("testdb: truncate: %v", err)
	}
}
