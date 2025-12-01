// Benchmark tool: measures latency and QPS for baseline, range, and hash modes.
// It runs a worker pool generating feed queries concurrently and aggregates metrics.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"sync"
	"time"

	"partitioning/ready/internal/db"
	"partitioning/ready/internal/router"
)

func main() {
	// CLI flags to control workload.
	var mode string
	var concurrency int
	var requests int
	var limit int
	var users int
	var windowDays int
	var subs int
	flag.StringVar(&mode, "mode", "baseline", "benchmark mode: baseline | range | hash | hash-consistent")
	flag.IntVar(&concurrency, "concurrency", 50, "number of concurrent workers")
	flag.IntVar(&requests, "requests", 1000, "total number of requests")
	flag.IntVar(&limit, "limit", 50, "feed limit")
	flag.IntVar(&users, "users", 10000, "user id space (1..users)")
	flag.IntVar(&windowDays, "windowDays", 0, "time window in days for created_at cutoff (0 = no cutoff)")
	flag.IntVar(&subs, "subs", 10, "number of user_ids per request")
	flag.Parse()

	ctx := context.Background()
	var cutoff time.Time
	if windowDays > 0 {
		cutoff = time.Now().Add(-time.Duration(windowDays) * 24 * time.Hour)
	}

	// getFeed is bound to the selected router implementation for current mode.
	var getFeed func(ctx context.Context, userIDs []int64, limit int) error

	switch mode {
	case "baseline":
		// Single pool against baseline (non-partitioned) table.
		pool, err := db.NewBaselinePool(ctx)
		if err != nil {
			log.Fatalf("connect baseline: %v", err)
		}
		defer pool.Close()
		r := &router.BaselineRouter{DB: pool}
		getFeed = func(ctx context.Context, userIDs []int64, limit int) error {
			_, err := r.GetFeed(ctx, userIDs, limit)
			return err
		}
	case "range":
		// Single pool pointing to partitioned table in same instance.
		pool, err := db.NewRangePool(ctx)
		if err != nil {
			log.Fatalf("connect range: %v", err)
		}
		defer pool.Close()
		r := &router.RangeRouter{DB: pool}
		getFeed = func(ctx context.Context, userIDs []int64, limit int) error {
			_, err := r.GetFeed(ctx, userIDs, limit)
			return err
		}
	case "hash":
		// Three pools, one per shard; router fans out and merges results.
		pools, err := db.NewShardPools(ctx)
		if err != nil {
			log.Fatalf("connect shards: %v", err)
		}
		for _, p := range pools {
			defer p.Close()
		}
		r := &router.HashRouter{Shards: pools}
		getFeed = func(ctx context.Context, userIDs []int64, limit int) error {
			_, err := r.GetFeed(ctx, userIDs, limit)
			return err
		}
	case "hash-consistent":
		// Consistent hashing router to minimize key movement on shard changes.
		pools, err := db.NewShardPools(ctx)
		if err != nil {
			log.Fatalf("connect shards: %v", err)
		}
		for _, p := range pools {
			defer p.Close()
		}
		ring := router.NewRing(200)
		ids := make([]int, 0, len(pools))
		for i := range pools {
			ids = append(ids, i)
		}
		ring.Build(ids)
		r := &router.ConsistentHashRouter{Shards: pools, Ring: ring}
		getFeed = func(ctx context.Context, userIDs []int64, limit int) error {
			_, err := r.GetFeed(ctx, userIDs, limit)
			return err
		}
	default:
		log.Fatalf("unknown mode: %s", mode)
	}

	// result is a per-request measurement for aggregation.
	type result struct {
		latency time.Duration
		err     error
	}

	// jobs is a bounded channel; each entry indicates "run one request".
	jobs := make(chan struct{}, requests)
	results := make(chan result, requests)

	for i := 0; i < requests; i++ {
		jobs <- struct{}{}
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(concurrency)

	// Start N workers to process jobs in parallel.
	startAll := time.Now()
	for w := 0; w < concurrency; w++ {
		go func() {
			defer wg.Done()
			for range jobs {
				// Pick 10 random subscriptions
				userIDs := make([]int64, 0, subs)
				for len(userIDs) < subs {
					id := 1 + rand.Int63n(int64(users))
					userIDs = append(userIDs, id)
				}
				// Time a single logical request (one feed fetch).
				start := time.Now()
				callCtx := ctx
				if windowDays > 0 {
					callCtx = context.WithValue(ctx, router.CtxCutoffKey, cutoff)
				}
				err := getFeed(callCtx, userIDs, limit)
				dur := time.Since(start)
				results <- result{latency: dur, err: err}
			}
		}()
	}
	wg.Wait()
	close(results)
	totalDur := time.Since(startAll)

	// Aggregate metrics: average latency, p95, and QPS.
	var latencies []time.Duration
	var errs int
	for r := range results {
		if r.err != nil {
			errs++
			continue
		}
		latencies = append(latencies, r.latency)
	}
	if len(latencies) == 0 {
		log.Fatalf("no successful requests (errors=%d)", errs)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var sum time.Duration
	for _, l := range latencies {
		sum += l
	}
	avg := time.Duration(int64(sum) / int64(len(latencies)))
	p95 := latencies[int(float64(len(latencies))*0.95)-1]
	qps := float64(len(latencies)) / totalDur.Seconds()

	fmt.Printf("Mode: %s\n", mode)
	fmt.Printf("Requests: %d, Concurrency: %d, Errors: %d\n", len(latencies), concurrency, errs)
	fmt.Printf("Avg latency: %s\n", avg.Truncate(time.Microsecond))
	fmt.Printf("P95 latency: %s\n", p95.Truncate(time.Microsecond))
	fmt.Printf("Total QPS: %.2f\n", qps)
}
