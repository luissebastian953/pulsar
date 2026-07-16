// Package pgtest provisions an isolated Postgres schema per test with all
// migrations applied. Tests skip unless PULSAR_TEST_DATABASE_URL is set.
package pgtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool returns a pool whose search_path is "<schema>,public" — public stays
// visible so pg_trgm's operator classes resolve inside test schemas. Schema
// setup across parallel test packages is serialized with an advisory lock
// (CREATE EXTENSION IF NOT EXISTS races with itself); the lock is
// session-scoped, so setup runs on a single acquired connection.
func Pool(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PULSAR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PULSAR_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	files, err := filepath.Glob(filepath.Join(migrationsDir(), "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found: %v", err)
	}
	sort.Strings(files)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(727272)"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock(727272)")

	stmts := []string{
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema),
		fmt.Sprintf("CREATE SCHEMA %s", schema),
	}
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		stmts = append(stmts, string(sql))
	}
	for _, sql := range stmts {
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("test schema setup: %v", err)
		}
	}
	return pool
}

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "db", "migrations")
}
