package main

import (
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"LedgerDB/services/logging"
	pb "LedgerServer/api"
	ts "LedgerServer/modules/transactions"
	ts_store "StorageKernel/proto/TransactionsStore"
)

var logger = logging.New("server", "./")

func main() {
	logger.Info("Starting LedgerServer on port 50003...")

	lis, err := net.Listen("tcp", ":50003")
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to listen: %v", err))
		panic(fmt.Sprintf("Failed to listen: %v", err))
	}

	TsStoreConn, err := grpc.Dial("localhost:50058", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to connect to StorageKernel: %v", err))
		panic(fmt.Sprintf("Failed to connect to StorageKernel: %v", err))
	}
	defer TsStoreConn.Close()

	grpcServer := grpc.NewServer()

	serverInstance := &ts.LedgerServer{
		TransactionsStoreClient: ts_store.NewTransactionsStoreServiceClient(TsStoreConn),
	}

	pb.RegisterTransactionsServiceServer(grpcServer, serverInstance)
	reflection.Register(grpcServer)

	logger.Info("LedgerServer running on port 50003...")
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error(fmt.Sprintf("Failed to serve: %v", err))
		panic(fmt.Sprintf("Failed to serve: %v", err))
	}
}
