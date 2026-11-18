// purgeleaked: one-off cleanup for the output-metadata leak
// (docs/compliance.md#output-metadata).
//
// Before the strip shipped, every delivered PNG carried the patched graph,
// including the blob IDs of the user's uploads (`LoadImage` inputs), and
// GET /images/{id} serves any blob by ID. This tool finds every blob ID that
// a stored job graph or an existing blob's embedded `prompt` chunk refers to
// and deletes those upload blobs, so the IDs already out in the world resolve
// to 404. Blob IDs are ephemeral by contract; orbit re-uploads from IndexedDB.
//
// Dry-run by default. Pass -apply to delete.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/core/internal/blob"
	"pulsar/core/internal/dotenv"
)

func main() {
	dotenv.Load() // native runs read the repo .env; real env always wins
	defaultDir := os.Getenv("DATA_DIR")
	if defaultDir == "" {
		defaultDir = "./data"
	}
	apply := flag.Bool("apply", false, "delete the blobs (default: dry-run, report only)")
	dataDir := flag.String("data-dir", defaultDir, "blob directory (core's DATA_DIR)")
	flag.Parse()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://pulsar:pulsar@localhost:5432/pulsar?sslmode=disable"
	}
	if _, err := os.Stat(*dataDir); err != nil {
		log.Fatalf("blob dir: %v", err) // never MkdirAll a typo into existence
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	referenced := map[string]bool{}
	jobIDs := map[string]bool{}
	active := map[string]bool{}

	rows, err := pool.Query(ctx, `SELECT id::text, state, workflow,
		coalesce(input_path, ''), coalesce(mask_path, ''), coalesce(ref_path, ''),
		coalesce(aux_paths, '{}') FROM jobs`)
	if err != nil {
		log.Fatalf("read jobs: %v", err)
	}
	var fromGraphs int
	for rows.Next() {
		var (
			id, state        string
			graph            []byte
			input, mask, ref string
			aux              []string
		)
		if err := rows.Scan(&id, &state, &graph, &input, &mask, &ref, &aux); err != nil {
			log.Fatalf("scan job: %v", err)
		}
		jobIDs[id] = true
		ids := graphBlobIDs(graph)
		for _, b := range ids {
			if !referenced[b] {
				fromGraphs++
			}
			referenced[b] = true
		}
		if state == "queued" || state == "running" {
			for _, b := range append(append(ids, input, mask, ref), aux...) {
				if b != "" {
					active[b] = true
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("read jobs: %v", err)
	}

	// Blobs outlive job rows (and a graph can differ from the row's), so the
	// files themselves are the second source.
	files, err := filepath.Glob(filepath.Join(*dataDir, "*.png"))
	if err != nil {
		log.Fatalf("list blobs: %v", err)
	}
	onDisk := map[string]bool{}
	var withGraph, fromBlobs int
	for _, f := range files {
		onDisk[strings.TrimSuffix(filepath.Base(f), ".png")] = true
		data, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("read blob: %v", err)
		}
		graph, ok := embeddedGraph(data)
		if !ok {
			continue
		}
		withGraph++
		for _, b := range graphBlobIDs(graph) {
			if !referenced[b] {
				fromBlobs++
			}
			referenced[b] = true
		}
	}

	p := makePlan(referenced, jobIDs, active, onDisk)
	fmt.Printf("jobs scanned: %d   blobs scanned: %d (%d carry a prompt chunk)\n", len(jobIDs), len(files), withGraph)
	fmt.Printf("referenced blob ids: %d (%d from job graphs, %d more from blob chunks)\n", len(referenced), fromGraphs, fromBlobs)
	report("kept — job output reused as input", p.KeptOutput)
	report("kept — input of a queued/running job", p.KeptActive)
	report("already gone", p.Gone)

	if !*apply {
		report("WOULD DELETE (dry-run; pass -apply)", p.Delete)
		return
	}
	store := &blob.Dir{Root: *dataDir}
	for _, id := range p.Delete {
		if err := store.Delete(id); err != nil {
			log.Fatalf("delete %s: %v", id, err)
		}
	}
	report("DELETED", p.Delete)
}

func report(label string, ids []string) {
	fmt.Printf("%s: %d\n", label, len(ids))
	for _, id := range ids {
		fmt.Printf("  %s\n", id)
	}
}
