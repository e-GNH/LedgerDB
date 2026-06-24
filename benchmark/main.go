// package main

// import (
// 	"bufio"
// 	"context"
// 	"crypto"
// 	"crypto/aes"
// 	"crypto/cipher"
// 	"crypto/rand"
// 	"crypto/rsa"
// 	"crypto/sha256"
// 	"crypto/x509"
// 	"encoding/json"
// 	"encoding/pem"
// 	"flag"
// 	"fmt"
// 	"math/big"
// 	mathrand "math/rand"
// 	"os"
// 	"sort"
// 	"strings"
// 	"sync"
// 	"sync/atomic"
// 	"time"

// 	"google.golang.org/grpc"
// 	"google.golang.org/grpc/credentials/insecure"

// 	pb "LedgerProxy/api"
// 	"LedgerProxy/types"
// )

// // ─────────────────────────────────────────────
// // CLI FLAGS
// // ─────────────────────────────────────────────

// var (
// 	flagConcurrency = flag.Int("c", 10, "Number of concurrent goroutines (workers)")
// 	flagDuration    = flag.Int("d", 30, "Benchmark duration in seconds")
// 	flagTarget      = flag.String("addr", "localhost:50001", "gRPC server address")
// 	flagSenderKey   = flag.String("sender-key", "/home/zizo/Documents/GP/LedgerDB/LedgerProxy/modules/security/keys/banks/CIB", "Path to sender private key")
// 	flagReceiverKey = flag.String("receiver-key", "/home/zizo/Documents/GP/LedgerDB/LedgerProxy/modules/security/keys/my_key.pem", "Path to server public key")
// 	flagMode        = flag.String("mode", "transfer", "Workload mode: transfer | mixed | create")
// 	flagAccountFile = flag.String("accounts", "accounts.txt", "Path to file with one account ID per line")
// 	flagWarmup      = flag.Int("warmup", 5, "Warmup duration in seconds (excluded from stats)")
// )

// // ─────────────────────────────────────────────
// // TYPES
// // ─────────────────────────────────────────────

// type LatencySample struct {
// 	latency time.Duration
// 	success bool
// 	opType  string
// }

// type Stats struct {
// 	samples []LatencySample
// 	mu      sync.Mutex
// }

// func (s *Stats) Record(l time.Duration, ok bool, op string) {
// 	s.mu.Lock()
// 	s.samples = append(s.samples, LatencySample{l, ok, op})
// 	s.mu.Unlock()
// }

// func (s *Stats) Print(elapsed time.Duration) {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	byOp := map[string][]time.Duration{}
// 	var totalSuccess, totalFail int64

// 	for _, sm := range s.samples {
// 		if sm.success {
// 			totalSuccess++
// 			byOp[sm.opType] = append(byOp[sm.opType], sm.latency)
// 		} else {
// 			totalFail++
// 		}
// 	}

// 	fmt.Println("\n══════════════════════════════════════════════════")
// 	fmt.Println("                 BENCHMARK RESULTS                ")
// 	fmt.Println("══════════════════════════════════════════════════")
// 	fmt.Printf("Duration       : %.1fs\n", elapsed.Seconds())
// 	fmt.Printf("Total Requests : %d\n", int(totalSuccess)+int(totalFail))
// 	fmt.Printf("Success        : %d\n", totalSuccess)
// 	fmt.Printf("Errors         : %d\n", totalFail)
// 	fmt.Printf("Error Rate     : %.2f%%\n", float64(totalFail)/float64(totalSuccess+totalFail)*100)
// 	fmt.Printf("Overall TPS    : %.2f\n", float64(totalSuccess)/elapsed.Seconds())
// 	fmt.Println()

// 	for op, latencies := range byOp {
// 		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
// 		n := len(latencies)
// 		var sum time.Duration
// 		for _, l := range latencies {
// 			sum += l
// 		}
// 		avg := sum / time.Duration(n)

// 		p50 := latencies[n*50/100]
// 		p95 := latencies[n*95/100]
// 		p99 := latencies[n*99/100]
// 		pMax := latencies[n-1]

// 		fmt.Printf("── [%s] (%d ops, %.2f TPS)\n", op, n, float64(n)/elapsed.Seconds())
// 		fmt.Printf("   avg=%-8s p50=%-8s p95=%-8s p99=%-8s max=%s\n",
// 			avg.Round(time.Microsecond),
// 			p50.Round(time.Microsecond),
// 			p95.Round(time.Microsecond),
// 			p99.Round(time.Microsecond),
// 			pMax.Round(time.Microsecond),
// 		)
// 	}
// 	fmt.Println("══════════════════════════════════════════════════")
// }

// // ─────────────────────────────────────────────
// // CRYPTO HELPERS
// // ─────────────────────────────────────────────

// func parsePrivateKey(pemStr string) *rsa.PrivateKey {
// 	block, _ := pem.Decode([]byte(pemStr))
// 	if block == nil {
// 		panic("failed to parse private key PEM")
// 	}
// 	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
// 	if err != nil {
// 		panic(fmt.Sprintf("failed to parse private key: %v", err))
// 	}
// 	return priv
// }

// func parsePublicKey(pemBytes []byte) *rsa.PublicKey {
// 	block, _ := pem.Decode(pemBytes)
// 	if block == nil {
// 		panic("failed to parse public key PEM")
// 	}
// 	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
// 	if err == nil {
// 		if rsaPub, ok := pub.(*rsa.PublicKey); ok {
// 			return rsaPub
// 		}
// 	}
// 	rsaPub, err := x509.ParsePKCS1PublicKey(block.Bytes)
// 	if err != nil {
// 		panic(fmt.Sprintf("failed to parse public key: %v", err))
// 	}
// 	return rsaPub
// }

// func encryptAndPack(payload []byte, senderPriv *rsa.PrivateKey, receiverPub *rsa.PublicKey) []byte {
// 	hashRaw := sha256.Sum256(payload)
// 	hash := hashRaw[:]

// 	signature, err := rsa.SignPKCS1v15(rand.Reader, senderPriv, crypto.SHA256, hash)
// 	if err != nil {
// 		panic(err)
// 	}

// 	pubBytes, _ := x509.MarshalPKIXPublicKey(&senderPriv.PublicKey)

// 	unpacked := types.UnpackedMessage{
// 		Data:       payload,
// 		Signature:  signature,
// 		Hash:       hash,
// 		BankPubKey: pubBytes,
// 	}
// 	unpackedBytes, _ := json.Marshal(unpacked)

// 	aesKey := make([]byte, 32)
// 	rand.Read(aesKey)
// 	block, _ := aes.NewCipher(aesKey)
// 	gcm, _ := cipher.NewGCM(block)
// 	nonce := make([]byte, gcm.NonceSize())
// 	rand.Read(nonce)
// 	aesCiphertext := gcm.Seal(nonce, nonce, unpackedBytes, nil)

// 	encryptedAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, receiverPub, aesKey, nil)
// 	if err != nil {
// 		panic(err)
// 	}

// 	return append(encryptedAESKey, aesCiphertext...)
// }

// // ─────────────────────────────────────────────
// // ACCOUNT POOL — loaded from file
// // ─────────────────────────────────────────────

// func loadAccounts(path string) []string {
// 	f, err := os.Open(path)
// 	if err != nil {
// 		panic(fmt.Sprintf("cannot open accounts file %q: %v", path, err))
// 	}
// 	defer f.Close()

// 	var accounts []string
// 	scanner := bufio.NewScanner(f)
// 	for scanner.Scan() {
// 		line := strings.TrimSpace(scanner.Text())
// 		if line != "" {
// 			accounts = append(accounts, line)
// 		}
// 	}
// 	if err := scanner.Err(); err != nil {
// 		panic(fmt.Sprintf("error reading accounts file: %v", err))
// 	}
// 	if len(accounts) == 0 {
// 		panic("accounts file is empty")
// 	}
// 	return accounts
// }

// func randomNonce() string {
// 	b := make([]byte, 32)
// 	rand.Read(b)
// 	return fmt.Sprintf("%x", b)
// }

// // ─────────────────────────────────────────────
// // OPERATIONS
// // ─────────────────────────────────────────────

// func opCreateAccount(
// 	ctx context.Context,
// 	client pb.SecurityServiceClient,
// 	senderPriv *rsa.PrivateKey,
// 	receiverPub *rsa.PublicKey,
// 	id string,
// ) (bool, error) {
// 	payload := types.SecureCreateAccountMessage{
// 		AccountId: id,
// 		Balance:   1_000_000,
// 		Nonce:     randomNonce(),
// 		Tier:      "PERSON",
// 		Name:      nil,
// 	}
// 	payloadBytes, _ := json.Marshal(payload)
// 	enc := encryptAndPack(payloadBytes, senderPriv, receiverPub)

// 	res, err := client.Execute(ctx, &pb.SecureRequest{EncryptedData: enc})
// 	if err != nil {
// 		return false, err
// 	}
// 	return res.Success, nil
// }

// func opTransfer(
// 	ctx context.Context,
// 	client pb.SecurityServiceClient,
// 	senderPriv *rsa.PrivateKey,
// 	receiverPub *rsa.PublicKey,
// 	from, to string,
// ) (bool, error) {
// 	amount, _ := rand.Int(rand.Reader, big.NewInt(1000))
// 	payload := types.SecureMessage{
// 		Timestamp: time.Now(),
// 		From:      from,
// 		To:        to,
// 		Amount:    amount.Int64() + 1,
// 		Message:   "bench-transfer",
// 		Nonce:     randomNonce(),
// 	}
// 	payloadBytes, _ := json.Marshal(payload)
// 	enc := encryptAndPack(payloadBytes, senderPriv, receiverPub)

// 	res, err := client.Execute(ctx, &pb.SecureRequest{EncryptedData: enc})
// 	if err != nil {
// 		return false, err
// 	}
// 	return res.Success, nil
// }

// // ─────────────────────────────────────────────
// // WORKER
// // ─────────────────────────────────────────────

// func worker(
// 	ctx context.Context,
// 	stop <-chan struct{},
// 	warmupUntil time.Time,
// 	client pb.SecurityServiceClient,
// 	senderPriv *rsa.PrivateKey,
// 	receiverPub *rsa.PublicKey,
// 	accounts []string,
// 	mode string,
// 	stats *Stats,
// 	createCounter *atomic.Int64,
// ) {
// 	n := len(accounts)
// 	for {
// 		select {
// 		case <-stop:
// 			return
// 		default:
// 		}

// 		var opType string
// 		var ok bool
// 		var err error

// 		start := time.Now()

// 		switch mode {
// 		case "create":
// 			idx := int(createCounter.Add(1))
// 			// Generate new IDs beyond the existing pool
// 			prefix := "000_"
// 			if idx%2 == 1 {
// 				prefix = "001_"
// 			}
// 			id := fmt.Sprintf("%sbench_new_%06d", prefix, idx)
// 			opType = "create_account"
// 			ok, err = opCreateAccount(ctx, client, senderPriv, receiverPub, id)

// 		case "mixed":
// 			r := mathrand.Intn(100)
// 			switch {
// 			case r < 88:
// 				opType = "transfer"
// 				from := accounts[mathrand.Intn(n)]
// 				to := accounts[mathrand.Intn(n)]
// 				for to == from {
// 					to = accounts[mathrand.Intn(n)]
// 				}
// 				ok, err = opTransfer(ctx, client, senderPriv, receiverPub, from, to)
// 			default:
// 				opType = "create_account"
// 				idx := int(createCounter.Add(1))
// 				prefix := "000_"
// 				if idx%2 == 1 {
// 					prefix = "001_"
// 				}
// 				id := fmt.Sprintf("%sbench_new_%06d", prefix, idx)
// 				ok, err = opCreateAccount(ctx, client, senderPriv, receiverPub, id)
// 			}

// 		default: // "transfer"
// 			opType = "transfer"
// 			from := accounts[mathrand.Intn(n)]
// 			to := accounts[mathrand.Intn(n)]
// 			for to == from {
// 				to = accounts[mathrand.Intn(n)]
// 			}
// 			ok, err = opTransfer(ctx, client, senderPriv, receiverPub, from, to)
// 		}

// 		latency := time.Since(start)

// 		if time.Now().After(warmupUntil) {
// 			if err != nil {
// 				stats.Record(latency, false, opType)
// 			} else {
// 				stats.Record(latency, ok, opType)
// 			}
// 		}
// 	}
// }

// // ─────────────────────────────────────────────
// // MAIN
// // ─────────────────────────────────────────────

// func main() {
// 	flag.Parse()

// 	// Load account pool from file
// 	accounts := loadAccounts(*flagAccountFile)

// 	fmt.Println("═══════════════════════════════════════")
// 	fmt.Println("  Ledger Benchmark (TPC-C inspired)")
// 	fmt.Println("═══════════════════════════════════════")
// 	fmt.Printf("Mode         : %s\n", *flagMode)
// 	fmt.Printf("Concurrency  : %d workers\n", *flagConcurrency)
// 	fmt.Printf("Duration     : %ds (+ %ds warmup)\n", *flagDuration, *flagWarmup)
// 	fmt.Printf("Target       : %s\n", *flagTarget)
// 	fmt.Printf("Account pool : %d accounts (from %s)\n", len(accounts), *flagAccountFile)
// 	fmt.Println()

// 	// Load keys
// 	senderPrivBytes, err := os.ReadFile(*flagSenderKey)
// 	if err != nil {
// 		panic(fmt.Sprintf("Cannot read sender key: %v", err))
// 	}
// 	senderPriv := parsePrivateKey(string(senderPrivBytes))

// 	receiverPubBytes, err := os.ReadFile(*flagReceiverKey)
// 	if err != nil {
// 		panic(fmt.Sprintf("Cannot read receiver key: %v", err))
// 	}
// 	receiverPub := parsePublicKey(receiverPubBytes)

// 	// Connect
// 	conn, err := grpc.Dial(*flagTarget,
// 		grpc.WithTransportCredentials(insecure.NewCredentials()),
// 		grpc.WithBlock(),
// 		grpc.WithTimeout(10*time.Second),
// 	)
// 	if err != nil {
// 		panic(fmt.Sprintf("Cannot connect: %v", err))
// 	}
// 	defer conn.Close()

// 	client := pb.NewSecurityServiceClient(conn)

// 	// Warmup boundary
// 	warmupUntil := time.Now().Add(time.Duration(*flagWarmup) * time.Second)
// 	totalDuration := time.Duration(*flagWarmup+*flagDuration) * time.Second

// 	fmt.Printf("Warming up for %ds...\n", *flagWarmup)

// 	stats := &Stats{}
// 	stop := make(chan struct{})
// 	ctx := context.Background()
// 	var createCounter atomic.Int64

// 	var wg sync.WaitGroup
// 	for i := 0; i < *flagConcurrency; i++ {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			worker(ctx, stop, warmupUntil, client, senderPriv, receiverPub,
// 				accounts, *flagMode, stats, &createCounter)
// 		}()
// 	}

// 	// Progress ticker
// 	benchStart := time.Now()
// 	ticker := time.NewTicker(5 * time.Second)
// 	go func() {
// 		for range ticker.C {
// 			elapsed := time.Since(benchStart)
// 			if elapsed > totalDuration {
// 				return
// 			}
// 			stats.mu.Lock()
// 			n := len(stats.samples)
// 			stats.mu.Unlock()
// 			realElapsed := elapsed - time.Duration(*flagWarmup)*time.Second
// 			if realElapsed > 0 {
// 				fmt.Printf("  [%.0fs] recorded ops: %d | TPS: %.1f\n",
// 					elapsed.Seconds(), n, float64(n)/realElapsed.Seconds())
// 			}
// 		}
// 	}()

// 	time.Sleep(totalDuration)
// 	close(stop)
// 	ticker.Stop()
// 	wg.Wait()

// 	elapsed := time.Duration(*flagDuration) * time.Second
// 	stats.Print(elapsed)
// }
package main