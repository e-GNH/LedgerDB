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
    pb "LedgerDB/proto/worldstate"

)

var ctx = context.Background()
func test_transfer(rdb *redis.Client, h *KernelHandler) {
	err := h.Transfer_Test(ctx, "nonce:1211122", "A", "B", 30)
	switch {
	case err == nil:
		fmt.Println("test transfer OK")
	case errors.Is(err, ErrInsufficientFunds):
		fmt.Println("not enough balance")
	case errors.Is(err, ErrNonceAlreadyUsed):
		fmt.Println("duplicate transaction :(")
	default:
		log.Fatal(err)
	}


}
func main() {
    rdb := newRedisClient() 
    h := &KernelHandler{rdb: rdb}
    // TODO REMOVE after testing
	rdb.FlushAll(ctx) // Delete Everything 
    if err := h.CreateAccount(ctx, "A", 100); err != nil { // Shall be from onboarding
        log.Fatal(err)
    }
    if err := h.CreateAccount(ctx, "B", 100); err != nil {
        log.Fatal(err)
    }
	test_transfer(rdb, h) // TODO: remove after onboarding
    lis, err := net.Listen("tcp", ":50053")
    if err != nil {
        log.Fatalf("failed to listen: %v", err)
    }

    grpcServer := grpc.NewServer()
    pb.RegisterWorldStateServiceServer(grpcServer, h)
    reflection.Register(grpcServer)

    fmt.Println("StorageKernel listening on :50053")
    if err := grpcServer.Serve(lis); err != nil {
        log.Fatalf("failed to serve: %v", err)
    }

}