package main

import (
	"context"
	"net"
	"os"
	"strings"

	// pb "StorageKernel/proto/worldstate"
	"LedgerDB/services/logging"
	ts "StorageKernel/proto/TransactionsStore"
	pb "StorageKernel/proto/worldstate"
	// pr "LedgerProxy/api"
	"LedgerProxy/types"
	"log"
	// "time"
	"fmt"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/aes"
	"crypto/cipher"
	"crypto"
	"flag"

	"crypto/x509"
	"encoding/json"
	"encoding/pem"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	// "google.golang.org/grpc/credentials/insecure"
)

var ctx = context.Background()
var logger = logging.New("StorageKernel", "./")
var file_name = "main.go"
var accountIDs []string
var (
	flagConcurrency  = flag.Int("c", 10, "Number of concurrent goroutines (workers)")
	flagDuration     = flag.Int("d", 30, "Benchmark duration in seconds")
	flagTarget       = flag.String("addr", "localhost:50001", "gRPC server address")
	flagSenderKey    = flag.String("sender-key", "modules/security/keys/banks/CIB", "Path to sender private key")
	flagReceiverKey  = flag.String("receiver-key", "modules/security/keys/my_key.pem", "Path to server public key")
	flagMode         = flag.String("mode", "transfer", "Workload mode: transfer | mixed | create")
	flagAccounts     = flag.Int("accounts", 100, "Number of pre-seeded accounts to use in transfers")
	flagWarmup       = flag.Int("warmup", 5, "Warmup duration in seconds (excluded from stats)")
)


func test_transfer(h *KernelHandler, offline bool, from_acc string, to_acc string, nonce string, amount int64) {
	logger.Info(" - [" + file_name + "] - Testing Transfer")
	_, err := h.Transfer(ctx, &pb.TransferRequest{Nonce: nonce, FromId: from_acc, ToId: &to_acc, Amount: amount, OfflineTransaction: offline})
	st, ok := status.FromError(err)
	switch {
	case err == nil:
		logger.Info(" - [" + file_name + "] - Test Transfer OK")
		logger.Info("test transfer OK")

	case ok && st.Code() == codes.FailedPrecondition:
		logger.Info(" - [" + file_name + "] - Test Transfer Failed due to insufficient funds")
		logger.Error(" - [" + file_name + "] - " + err.Error())
	case ok && st.Code() == codes.AlreadyExists:
		logger.Info(" - [" + file_name + "] - Test Transfer Failed due to nonce already used")
		logger.Error(" - [" + file_name + "] - " + err.Error())
	default:
		logger.Error(" - [" + file_name + "] - Test Transfer Failed")
		logger.Error(" - [" + file_name + "] - " + err.Error())
	}

}

func randomNonce() string {
	b := make([]byte, 32)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func accountID(i int) string {
	prefix := "000_"
	if i%2 == 1 { prefix = "001_" }
	return fmt.Sprintf("%sbench_acct_%06d", prefix, i)
}

func parsePrivateKey(pemStr string) *rsa.PrivateKey {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil { panic("failed to parse private key PEM") }
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil { panic(fmt.Sprintf("failed to parse private key: %v", err)) }
	return priv
}

func parsePublicKey(pemBytes []byte) *rsa.PublicKey {
	block, _ := pem.Decode(pemBytes)
	if block == nil { panic("failed to parse public key PEM") }
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err == nil {
		if rsaPub, ok := pub.(*rsa.PublicKey); ok { return rsaPub }
	}
	rsaPub, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil { panic(fmt.Sprintf("failed to parse public key: %v", err)) }
	return rsaPub
}

func encryptAndPack(payload []byte, senderPriv *rsa.PrivateKey, receiverPub *rsa.PublicKey) []byte {
	hashRaw := sha256.Sum256(payload)
	hash := hashRaw[:]

	signature, err := rsa.SignPKCS1v15(rand.Reader, senderPriv, crypto.SHA256, hash)
	if err != nil { panic(err) }

	pubBytes, _ := x509.MarshalPKIXPublicKey(&senderPriv.PublicKey)

	unpacked := types.UnpackedMessage{
		Data:       payload,
		Signature:  signature,
		Hash:       hash,
		BankPubKey: pubBytes,
	}
	unpackedBytes, _ := json.Marshal(unpacked)

	aesKey := make([]byte, 32)
	rand.Read(aesKey)
	block, _ := aes.NewCipher(aesKey)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	aesCiphertext := gcm.Seal(nonce, nonce, unpackedBytes, nil)

	encryptedAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, receiverPub, aesKey, nil)
	if err != nil { panic(err) }

	return append(encryptedAESKey, aesCiphertext...)
}

func createManyAccounts(
	ctx context.Context,
	h *KernelHandler,
	id string,
	) {

	if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{
			AccountId: id,
			Balance:   1_000_000,
			Nonce:     randomNonce(),
			Tier:      "PERSON",
			Name:      nil,
		}); 

		err != nil { 
		logger.Error(" - [" + file_name + "] - " + err.Error())
	}

}

func main() {

	logger.Info(" - [" + file_name + "] - Creating Redis Client")

	rdb := newRedisClient()
	// TODO REMOVE after testing
	rdb.FlushAll(ctx) // Delete Everything
	ts_server, err := newHDFSClient()

	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return
	}

	h := &KernelHandler{
		hdfs:   ts_server,
		rdb:    rdb,
		amlURL: "http://127.0.0.1:8000/",
	}

	// ctx2, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	// defer cancel()

	seeded := 0
	// client := pr.NewSecurityServiceClient(conn)
	for i := 0; i < *flagAccounts; i++ {
		// createManyAccounts(ctx, client, senderPriv, receiverPub, "000_wallet_A")
		id := accountID(i)
		createManyAccounts(ctx, h, id)
		accountIDs = append(accountIDs, id)
		seeded++ 
		if i%10 == 0 { fmt.Printf("\r  Seeded %d/%d ", seeded, *flagAccounts) }
	}
	err2 := os.WriteFile(
		"account_ids.txt",
		[]byte(strings.Join(accountIDs, "\n")),
		0644,
	)
	if err2 != nil {
		panic(err2)
	}

fmt.Printf("\nSaved %d account IDs to account_ids.txt\n", len(accountIDs))

	// if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:123s456", AccountId: "000_wallet_A", Balance: 92000000, Tier: "PERSON"}); err != nil { // Shall be from onboarding
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// pedro := "Pedro"
	// if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:123457", AccountId: "000_wallet_B", Balance: 2000000, Tier: "MERCHANT", Name: &pedro}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:121233s456", AccountId: "000_wallet_C", Balance: 2000000, Tier: "PERSON"}); err != nil { // Shall be from onboarding
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:121233457", AccountId: "000_wallet_D", Balance: 2000000, Tier: "POS"}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:123123s456", AccountId: "000_wallet_E", Balance: 2000000, Tier: "PERSON"}); err != nil { // Shall be from onboarding
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:121251457", AccountId: "000_wallet_F", Balance: 5000000, Tier: "POS"}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if _, err := h.OfflineDeposit(ctx, &pb.OfflineDepositRequest{Nonce: "nonce:123456", AccountId: "000_wallet_A", Amount: 99}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// A := "000_wallet_A"
	// B := "000_wallet_B"
	// C := "000_wallet_C"
	// D := "000_wallet_D"
	// E := "000_wallet_E"
	// F := "000_wallet_F"
	// test_transfer(h, false, A, B, "za3bololo", 1000000)
	// test_transfer(h, false, A, C, "za3bo123lolo", 1000000)
	// test_transfer(h, false, A, D, "za3b123ololo", 1000000)
	// test_transfer(h, false, A, E, "za312bololo", 1000000)
	// // test_transfer(h,  false,A, F, "za3bolo123lo", 1000000)
	// test_transfer(h, false, B, F, "nonce:1232456", 1000000)
	// test_transfer(h, false, C, F, "za3bo123123lolo", 1000000)
	// test_transfer(h, false, D, F, "z123a3b123ololo", 1000000)
	// test_transfer(h, false, E, F, "za312bolol123o", 1000000)
	// test_transfer(h, false, F, A, "za3bolo121233lo", 5000000)
	// go func() {
	// 	time.Sleep(15 * time.Second)
	// 	logger.Info(" - [" + file_name + "] - sleept AML Service")
	// 	test_transfer(h, false, E, F, "za312asdasbolol123o", 1000000)
	// }()
	// test_transfer(h, false, "nonce:1232426", 100)
	// test_transfer(h, false, "nonce:12324336", 100)
	// if _, err := h.OfflineWithdraw(ctx, &pb.OfflineWithdrawRequest{Nonce: "nonce:12323145", AccountId: "001_wallet_B", Amount: 10}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if _, err := h.OfflineDeposit(ctx, &pb.OfflineDepositRequest{Nonce: "nonce:123231245", AccountId: "001_wallet_B", Amount: 110}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if _, err := h.OfflineWithdraw(ctx, &pb.OfflineWithdrawRequest{Nonce: "nonce:12345", AccountId: "000_wallet_A", Amount: 49}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if _, err := h.OfflineWithdraw(ctx, &pb.OfflineWithdrawRequest{Nonce: "nonce:123345", AccountId: "000_wallet_A", Amount: 39}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if _, err := h.OfflineDeposit(ctx, &pb.OfflineDepositRequest{Nonce: "nonce:1234256", AccountId: "000_wallet_A", Amount: 89}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// if res, err := h.GetAccountsTier(ctx, &pb.GetAccountsTierRequest{SenderAccountId: "000_wallet_A", ReceiverAccountId: "001_wallet_B"}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// } else {
	// 	logger.Info(" - [" + file_name + "] - Sender Tier: " + res.SenderTier)
	// 	logger.Info(" - [" + file_name + "] - Receiver Tier: " + res.ReceiverTier)
	// }
	// if _, err := h.ChangeAccountStatus(ctx, &pb.ChangeAccountStatusRequest{AccountId: "000_wallet_A", Status: "flagged"}); err != nil {
	// 	logger.Error(" - [" + file_name + "] - " + err.Error())
	// }
	// ==========================================
	// 🧪 HDFS BATCH TEST
	// ==========================================
	// logger.Info(" - [" + file_name + "] - Running HDFS Batch Test...")

	// // 1. Create a fake batch of transactions
	// fakeBatch := []*ts.Transaction{
	// 	{
	// 		Hash:       "tx_hash_111",
	// 		FromWallet: "A",
	// 		ToWallet:   "B",
	// 		Amount:     50.0,
	// 		Status:     true,
	// 		Message:    "test transfer 1",
	// 	},
	// 	{
	// 		Hash:       "tx_hash_222",
	// 		FromWallet: "B",
	// 		ToWallet:   "C",
	// 		Amount:     10.0,
	// 		Status:     true,
	// 		Message:    "test transfer 2",
	// 	},
	// }

	// // 2. Send it to HDFS
	// receipts, success, hdfsErr := h.StoreBatch(fakeBatch, file_name)
	// if hdfsErr != nil {
	// 	logger.Error(" - [" + file_name + "] - HDFS Test Failed: " + hdfsErr.Error())
	// } else if success {
	// 	fmt.Println(receipts)
	// 	logger.Info(" - [" + file_name + "] - HDFS Test PASSED! Batch written successfully.")
	// }
	// ==========================================
	port := "50058"
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return
	}

	logger.Info(" - [" + file_name + "] - Starting StorageKernel GRPC Server")
	grpcServer := grpc.NewServer()

	pb.RegisterWorldStateServiceServer(grpcServer, h)
	ts.RegisterTransactionsStoreServiceServer(grpcServer, h)

	reflection.Register(grpcServer)

	logger.Info(" - [" + file_name + "] - Storage kernel listening on port " + port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

}
