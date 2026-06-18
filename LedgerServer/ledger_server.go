package main

import (
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"LedgerDB/services/logging"
	pb "LedgerServer/api"
	aml_batch "LedgerServer/modules/aml"
	ts "LedgerServer/modules/transactions"
	ts_store "StorageKernel/proto/TransactionsStore"
	worldstate "StorageKernel/proto/worldstate"
)

var logger = logging.New("server", "./")

func main() {
	logger.Info("Starting LedgerServer on port 50003...")

	lis, err := net.Listen("tcp", ":50003")
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to listen: %v", err))
		panic(fmt.Sprintf("Failed to listen: %v", err))
	}

	StorageKernelConn, err := grpc.Dial("localhost:50058", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to connect to StorageKernel: %v", err))
		panic(fmt.Sprintf("Failed to connect to StorageKernel: %v", err))
	}
	defer StorageKernelConn.Close()

	grpcServer := grpc.NewServer()

	serverInstance := &ts.LedgerServer{
		TransactionsStoreClient: ts_store.NewTransactionsStoreServiceClient(StorageKernelConn),
		WorldStateClient:        worldstate.NewWorldStateServiceClient(StorageKernelConn),
	}

	pb.RegisterTransactionsServiceServer(grpcServer, serverInstance)
	reflection.Register(grpcServer)
	go aml_batch.PeriodicAMLCheck("http://localhost:8000/", 5, logger, worldstate.NewWorldStateServiceClient(StorageKernelConn))
	logger.Info("LedgerServer running on port 50003...")
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error(fmt.Sprintf("Failed to serve: %v", err))
		panic(fmt.Sprintf("Failed to serve: %v", err))
	}
}
