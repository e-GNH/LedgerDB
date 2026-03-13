package main

import (
    "context"
    "fmt"
    "log"
	"errors"
    "net"
	"github.com/redis/go-redis/v9"
    "google.golang.org/grpc"
    "google.golang.org/grpc/reflection"
    // pb "StorageKernel/proto/worldstate"
    ts "StorageKernel/proto/TransactionsStore"
    "LedgerDB/services/logging"

)

var ctx = context.Background()
var logger = logging.New("StorageKernel", "./")
var file_name = "main.go"

func test_transfer(rdb *redis.Client, h *KernelHandler) {
	logger.Info(" - [" + file_name + "] - Testing Transfer")
    err := h.Transfer_Test(ctx, "nonce:1211123122", "A", "B", 1)
	switch {
	case err == nil:
        logger.Info(" - [" + file_name + "] - Test Transfer OK")
		fmt.Println("test transfer OK")
	case errors.Is(err, ErrInsufficientFunds):
        logger.Info(" - [" + file_name + "] - Test Transfer Failed due to insufficient funds")
		fmt.Println("not enough balance")
	case errors.Is(err, ErrNonceAlreadyUsed):
        logger.Info(" - [" + file_name + "] - Test Transfer Failed due to nonce already used")
		fmt.Println("duplicate transaction :(")
	default:
        logger.Error(" - [" + file_name + "] - Test Transfer Failed")
		log.Fatal(err)
	}


}
func main() {

    logger.Info(" - [" + file_name + "] - Creating Redis Client")

    // rdb := newRedisClient() 
    ts_server, err := newHDFSClient()

    if err != nil {
        logger.Error(" - [" + file_name + "] - " + err.Error())
        return 
    }

    h := &KernelHandler{hdfs: ts_server}

	// test_transfer(rdb, h) // TODO: remove after onboarding
    // if err := h.CreateAccount(ctx, "A", 100); err != nil { // Shall be from onboarding
    //     logger.Error(" - [" + file_name + "] - " + err.Error())
    // }
    // if err := h.CreateAccount(ctx, "B", 100); err != nil {
    //     logger.Error(" - [" + file_name + "] - " + err.Error())
    // }

    // ==========================================
    // 🧪 HDFS BATCH TEST
    // ==========================================
    logger.Info(" - [" + file_name + "] - Running HDFS Batch Test...")
    
    // 1. Create a fake batch of transactions
    fakeBatch := []*ts.Transaction{
        {
            Hash:       "tx_hash_111",
            FromWallet: "A",
            ToWallet:   "B",
            Amount:     50.0,
            Status:     true,
            Message:    "test transfer 1",
        },
        {
            Hash:       "tx_hash_222",
            FromWallet: "B",
            ToWallet:   "C",
            Amount:     10.0,
            Status:     true,
            Message:    "test transfer 2",
        },
    }

    // 2. Send it to HDFS
    receipts, success, hdfsErr := h.StoreBatch(fakeBatch, file_name)
    if hdfsErr != nil {
        logger.Error(" - [" + file_name + "] - HDFS Test Failed: " + hdfsErr.Error())
    } else if success {
        fmt.Println(receipts)
        logger.Info(" - [" + file_name + "] - HDFS Test PASSED! Batch written successfully.")
    }
    // ==========================================

    lis, err := net.Listen("tcp", ":50051")
    if err != nil {
        logger.Error(" - [" + file_name + "] - " + err.Error())
        return
    }

    logger.Info(" - [" + file_name + "] - Starting StorageKernel GRPC Server")
    grpcServer := grpc.NewServer()

    // pb.RegisterWorldStateServiceServer(grpcServer, h)
    ts.RegisterTransactionsStoreServiceServer(grpcServer, h)

    reflection.Register(grpcServer)


    logger.Info(" - [" + file_name + "] - Storage kernel listening on port 50051")
    if err := grpcServer.Serve(lis); err != nil {
        log.Fatalf("failed to serve: %v", err)
    }
	// TODO REMOVE after testing
	// rdb.FlushAll(ctx) // Delete Everything 

} 