package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	mathrand "math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "LedgerProxy/api"
)

// ─────────────────────────────────────────────
// CLI FLAGS
// ─────────────────────────────────────────────

var (
	flagConcurrency = flag.Int("c", 10, "Number of concurrent goroutines (workers)")
	flagDuration    = flag.Int("d", 30, "Benchmark duration in seconds")
	flagTarget      = flag.String("addr", "localhost:50001", "gRPC server address")
	flagMode        = flag.String("mode", "transfer", "Workload mode: transfer | mixed | create")
	flagAccountFile = flag.String("accounts", "accounts.txt", "Path to file with one account ID per line")
	flagWarmup      = flag.Int("warmup", 5, "Warmup duration in seconds (excluded from stats)")
	flagPayloads    = flag.String("payloads", "payloads.bin", "Pre-encrypted payloads file (from gen_payloads)")
)

// ─────────────────────────────────────────────
// TYPES
// ─────────────────────────────────────────────

type LatencySample struct {
	latency time.Duration
	success bool
	opType  string
}

type Stats struct {
	samples []LatencySample
	mu      sync.Mutex
}

func (s *Stats) Record(l time.Duration, ok bool, op string) {
	s.mu.Lock()
	s.samples = append(s.samples, LatencySample{l, ok, op})
	s.mu.Unlock()
}

func (s *Stats) Print(elapsed time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byOp := map[string][]time.Duration{}
	var totalSuccess, totalFail int64

	for _, sm := range s.samples {
		if sm.success {
			totalSuccess++
			byOp[sm.opType] = append(byOp[sm.opType], sm.latency)
		} else {
			totalFail++
		}
	}

	fmt.Println("\n══════════════════════════════════════════════════")
	fmt.Println("                 BENCHMARK RESULTS                ")
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Printf("Duration       : %.1fs\n", elapsed.Seconds())
	fmt.Printf("Total Requests : %d\n", int(totalSuccess)+int(totalFail))
	fmt.Printf("Success        : %d\n", totalSuccess)
	fmt.Printf("Errors         : %d\n", totalFail)
	fmt.Printf("Error Rate     : %.2f%%\n", float64(totalFail)/float64(totalSuccess+totalFail)*100)
	fmt.Printf("Overall TPS    : %.2f\n", float64(totalSuccess)/elapsed.Seconds())
	fmt.Println()

	for op, latencies := range byOp {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		n := len(latencies)
		var sum time.Duration
		for _, l := range latencies {
			sum += l
		}
		avg := sum / time.Duration(n)

		p50 := latencies[n*50/100]
		p95 := latencies[n*95/100]
		p99 := latencies[n*99/100]
		pMax := latencies[n-1]

		fmt.Printf("── [%s] (%d ops, %.2f TPS)\n", op, n, float64(n)/elapsed.Seconds())
		fmt.Printf("   avg=%-8s p50=%-8s p95=%-8s p99=%-8s max=%s\n",
			avg.Round(time.Microsecond),
			p50.Round(time.Microsecond),
			p95.Round(time.Microsecond),
			p99.Round(time.Microsecond),
			pMax.Round(time.Microsecond),
		)
	}
	fmt.Println("══════════════════════════════════════════════════")
}

// ─────────────────────────────────────────────
// LOAD PRE-ENCRYPTED PAYLOADS
// ─────────────────────────────────────────────

// loadPayloads reads the binary file written by gen_payloads.
// Format: repeated [8-byte big-endian length][payload bytes]
func loadPayloads(path string) [][]byte {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("cannot open payloads file %q: %v", path, err))
	}
	defer f.Close()

	var payloads [][]byte
	lenBuf := make([]byte, 8)

	for {
		_, err := io.ReadFull(f, lenBuf)
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("error reading payload length: %v", err))
		}

		size := binary.BigEndian.Uint64(lenBuf)
		data := make([]byte, size)
		if _, err := io.ReadFull(f, data); err != nil {
			panic(fmt.Sprintf("error reading payload data: %v", err))
		}
		payloads = append(payloads, data)
	}

	if len(payloads) == 0 {
		panic("payloads file is empty — run gen_payloads first")
	}
	return payloads
}

// ─────────────────────────────────────────────
// ACCOUNT POOL
// ─────────────────────────────────────────────

func loadAccounts(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		panic(fmt.Sprintf("cannot open accounts file %q: %v", path, err))
	}
	defer f.Close()

	var accounts []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			accounts = append(accounts, line)
		}
	}
	if len(accounts) == 0 {
		panic("accounts file is empty")
	}
	return accounts
}

// ─────────────────────────────────────────────
// WORKER
// ─────────────────────────────────────────────

func worker(
	ctx context.Context,
	stop <-chan struct{},
	warmupUntil time.Time,
	client pb.SecurityServiceClient,
	payloads [][]byte,
	mode string,
	stats *Stats,
	counter *atomic.Int64,
) {
	n := len(payloads)

	for {
		select {
		case <-stop:
			return
		default:
		}

		// pick a random pre-encrypted payload
		idx := mathrand.Intn(n)
		enc := payloads[idx]

		start := time.Now()
		res, err := client.Execute(ctx, &pb.SecureRequest{EncryptedData: enc})
		latency := time.Since(start)

		if time.Now().After(warmupUntil) {
			if err != nil {
				stats.Record(latency, false, mode)
			} else {
				stats.Record(latency, res.Success, mode)
			}
		}

		counter.Add(1)
	}
}

// ─────────────────────────────────────────────
// MAIN
// ─────────────────────────────────────────────

func main() {
	flag.Parse()

	fmt.Println("═══════════════════════════════════════")
	fmt.Println("  Ledger Benchmark (TPC-C inspired)")
	fmt.Println("═══════════════════════════════════════")

	// Load pre-encrypted payloads
	fmt.Printf("Loading payloads from %q...\n", *flagPayloads)
	payloads := loadPayloads(*flagPayloads)
	fmt.Printf("Loaded %d pre-encrypted payloads\n", len(payloads))

	fmt.Printf("Mode         : %s\n", *flagMode)
	fmt.Printf("Concurrency  : %d workers\n", *flagConcurrency)
	fmt.Printf("Duration     : %ds (+ %ds warmup)\n", *flagDuration, *flagWarmup)
	fmt.Printf("Target       : %s\n", *flagTarget)
	fmt.Println()

	// Connect
	conn, err := grpc.Dial(*flagTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(10*time.Second),
	)
	if err != nil {
		panic(fmt.Sprintf("Cannot connect: %v", err))
	}
	defer conn.Close()

	client := pb.NewSecurityServiceClient(conn)

	warmupUntil := time.Now().Add(time.Duration(*flagWarmup) * time.Second)
	totalDuration := time.Duration(*flagWarmup+*flagDuration) * time.Second

	fmt.Printf("Warming up for %ds...\n", *flagWarmup)

	stats := &Stats{}
	stop := make(chan struct{})
	ctx := context.Background()
	var counter atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < *flagConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(ctx, stop, warmupUntil, client, payloads, *flagMode, stats, &counter)
		}()
	}

	// Progress ticker
	benchStart := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for range ticker.C {
			elapsed := time.Since(benchStart)
			if elapsed > totalDuration {
				return
			}
			stats.mu.Lock()
			n := len(stats.samples)
			stats.mu.Unlock()
			realElapsed := elapsed - time.Duration(*flagWarmup)*time.Second
			if realElapsed > 0 {
				fmt.Printf("  [%.0fs] recorded ops: %d | TPS: %.1f\n",
					elapsed.Seconds(), n, float64(n)/realElapsed.Seconds())
			}
		}
	}()

	time.Sleep(totalDuration)
	close(stop)
	ticker.Stop()
	wg.Wait()

	elapsed := time.Duration(*flagDuration) * time.Second
	stats.Print(elapsed)
}
