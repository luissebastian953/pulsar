// seed-tags: Danbooru vocabulary → tag DB (docs/tagdb.md#ingest).
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/core/internal/dotenv"
	"pulsar/core/internal/tags"
)

func main() {
	dotenv.Load() // native runs read the repo .env; real env always wins
	defaultFloor := 50
	if v := os.Getenv("MIN_POST_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Fatalf("invalid MIN_POST_COUNT: %v", err)
		}
		defaultFloor = n
	}
	floor := flag.Int("min-post-count", defaultFloor, "ingest floor: skip tags the model never learned")
	baseURL := flag.String("base-url", "https://danbooru.donmai.us", "tag source")
	flag.Parse()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://pulsar:pulsar@localhost:5432/pulsar?sslmode=disable"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping postgres: %v", err)
	}

	stats, err := tags.Ingest(ctx, pool, tags.IngestOptions{
		BaseURL:      *baseURL,
		MinPostCount: *floor,
	})
	if err != nil {
		log.Fatalf("ingest: %v", err)
	}
	log.Printf("seed-tags: done — %d tags, %d aliases, %d skipped, %d pose tags marked",
		stats.Tags, stats.Aliases, stats.Skipped, stats.PoseMarked)
	if len(stats.PoseMissing) > 0 {
		log.Printf("seed-tags: pose seeds with no tag row (review, docs/tagdb.md#pose-subset): %v",
			stats.PoseMissing)
	}
}
