package main

import (
	"context"
	"net"

	// pb "StorageKernel/proto/worldstate"
	"LedgerDB/services/logging"
	ts "StorageKernel/proto/TransactionsStore"
	pb "StorageKernel/proto/worldstate"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

var ctx = context.Background()
var logger = logging.New("StorageKernel", "./")
var file_name = "main.go"

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

	if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:123s456", AccountId: "000_wallet_A", Balance: 92000000, Tier: "PERSON"}); err != nil { // Shall be from onboarding
		logger.Error(" - [" + file_name + "] - " + err.Error())
	}
	pedro := "Pedro"
	if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:123457", AccountId: "000_wallet_B", Balance: 2000000, Tier: "MERCHANT", Name: &pedro}); err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
	}
	if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:121233s456", AccountId: "000_wallet_C", Balance: 2000000, Tier: "PERSON"}); err != nil { // Shall be from onboarding
		logger.Error(" - [" + file_name + "] - " + err.Error())
	}
	if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:121233457", AccountId: "000_wallet_D", Balance: 2000000, Tier: "POS"}); err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
	}
	if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:123123s456", AccountId: "000_wallet_E", Balance: 2000000, Tier: "PERSON"}); err != nil { // Shall be from onboarding
		logger.Error(" - [" + file_name + "] - " + err.Error())
	}
	if _, err := h.CreateAccount(ctx, &pb.CreateAccountRequest{Nonce: "nonce:121251457", AccountId: "000_wallet_F", Balance: 5000000, Tier: "POS"}); err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
	}
	if _, err := h.OfflineDeposit(ctx, &pb.OfflineDepositRequest{Nonce: "nonce:123456", AccountId: "000_wallet_A", Amount: 99}); err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
	}
	A := "000_wallet_A"
	B := "000_wallet_B"
	C := "000_wallet_C"
	D := "000_wallet_D"
	E := "000_wallet_E"
	F := "000_wallet_F"
	test_transfer(h, false, A, B, "za3bololo", 1000000)
	test_transfer(h, false, A, C, "za3bo123lolo", 1000000)
	test_transfer(h, false, A, D, "za3b123ololo", 1000000)
	test_transfer(h, false, A, E, "za312bololo", 1000000)
	// test_transfer(h,  false,A, F, "za3bolo123lo", 1000000)
	test_transfer(h, false, B, F, "nonce:1232456", 1000000)
	test_transfer(h, false, C, F, "za3bo123123lolo", 1000000)
	test_transfer(h, false, D, F, "z123a3b123ololo", 1000000)
	test_transfer(h, false, E, F, "za312bolol123o", 1000000)
	test_transfer(h, false, F, A, "za3bolo121233lo", 5000000)
	go func() {
		time.Sleep(15 * time.Second)
		logger.Info(" - [" + file_name + "] - sleept AML Service")
		test_transfer(h, false, E, F, "za312asdasbolol123o", 1000000)
	}()
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
