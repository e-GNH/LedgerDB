// package main

// import (
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
// 	"os"
// 	"strings"
// 	"bufio"
// 	"time"

// 	"LedgerProxy/types"
// )

// var (
// 	flagCount       = flag.Int("n", 10000, "Number of payloads to generate")
// 	flagSenderKey   = flag.String("sender-key", "/home/zizo/Documents/GP/LedgerDB/LedgerProxy/modules/security/keys/banks/CIB", "Path to sender private key")
// 	flagReceiverKey = flag.String("receiver-key", "/home/zizo/Documents/GP/LedgerDB/LedgerProxy/modules/security/keys/my_key.pem", "Path to server public key")
// 	flagAccountFile = flag.String("accounts", "accounts.txt", "Path to file with one account ID per line")
// 	flagOutput      = flag.String("out", "payloads.bin", "Output file for encrypted payloads")
// 	flagMode        = flag.String("mode", "transfer", "Payload type: transfer | create")
// )

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

// func randomNonce() string {
// 	b := make([]byte, 32)
// 	rand.Read(b)
// 	return fmt.Sprintf("%x", b)
// }

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
// 	if len(accounts) == 0 {
// 		panic("accounts file is empty")
// 	}
// 	return accounts
// }

// // File format (binary, length-prefixed):
// // For each payload: [8 bytes big-endian length][payload bytes]


// func writeUint64BE(b []byte, v uint64) {
// 	b[0] = byte(v >> 56)
// 	b[1] = byte(v >> 48)
// 	b[2] = byte(v >> 40)
// 	b[3] = byte(v >> 32)
// 	b[4] = byte(v >> 24)
// 	b[5] = byte(v >> 16)
// 	b[6] = byte(v >> 8)
// 	b[7] = byte(v)
// }

// func main() {
// 	flag.Parse()

// 	accounts := loadAccounts(*flagAccountFile)
// 	n := len(accounts)

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

// 	out, err := os.Create(*flagOutput)
// 	if err != nil {
// 		panic(fmt.Sprintf("Cannot create output file: %v", err))
// 	}
// 	defer out.Close()

// 	fmt.Printf("Generating %d encrypted payloads (mode=%s)...\n", *flagCount, *flagMode)
// 	start := time.Now()

// 	lenBuf := make([]byte, 8)

// 	for i := 0; i < *flagCount; i++ {
// 		var payloadBytes []byte

// 		switch *flagMode {
// 		case "create":
// 			payload := types.SecureCreateAccountMessage{
// 				AccountId: fmt.Sprintf("bench_new_%08d", i),
// 				Balance:   1_000_000,
// 				Nonce:     randomNonce(),
// 				Tier:      "PERSON",
// 				Name:      nil,
// 			}
// 			payloadBytes, _ = json.Marshal(payload)

// 		default: // transfer
// 			// pick two distinct random accounts
// 			idxFrom := i % n
// 			idxTo := (i + 1 + (i % (n - 1))) % n
// 			amount, _ := rand.Int(rand.Reader, big.NewInt(1000))
// 			payload := types.SecureMessage{
// 				Timestamp: time.Now(),
// 				From:      accounts[idxFrom],
// 				To:        accounts[idxTo],
// 				Amount:    amount.Int64() + 1,
// 				Message:   "bench-transfer",
// 				Nonce:     randomNonce(),
// 			}
// 			payloadBytes, _ = json.Marshal(payload)
// 		}

// 		enc := encryptAndPack(payloadBytes, senderPriv, receiverPub)

// 		// write length prefix then bytes
// 		writeUint64BE(lenBuf, uint64(len(enc)))
// 		out.Write(lenBuf)
// 		out.Write(enc)

// 		if (i+1)%500 == 0 {
// 			fmt.Printf("  %d / %d (%.1f/s)\n", i+1, *flagCount, float64(i+1)/time.Since(start).Seconds())
// 		}
// 	}

// 	fmt.Printf("\nDone. %d payloads written to %q in %.1fs\n", *flagCount, *flagOutput, time.Since(start).Seconds())
// }
