package main

import (
	"fmt"
	"net"
	"google.golang.org/grpc/reflection"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"LedgerDB/services/logging"
	pb "LedgerServer/api"
	ts "LedgerServer/modules/transactions"
	ts_store "StorageKernel/proto/TransactionsStore"
)


var logger = logging.New("server", "./")


func main() {


	logger.Info("Starting gRPC Security Server on port 50053...")
	lis, err := net.Listen("tcp", ":50053")
	if err != nil {
		logger.Error(fmt.Sprintf("Failed to listen: %v", err))
		panic(fmt.Sprintf("Failed to listen: %v", err))
	}

	TsStoreConn, err := grpc.Dial("localhost:50055", grpc.WithTransportCredentials(insecure.NewCredentials()))

	grpcServer := grpc.NewServer()

	myServerInstance := &ts.LedgerServer{
		TransactionsStoreClient: ts_store.NewTransactionsStoreServiceClient(TsStoreConn),
	}

	pb.RegisterTransactionsServiceServer(grpcServer, myServerInstance)
	reflection.Register(grpcServer)

	logger.Info("gRPC Security Server is running on port 50053...")
	if err := grpcServer.Serve(lis); err != nil {
		logger.Error(fmt.Sprintf("Failed to serve: %v", err))
		panic(fmt.Sprintf("Failed to serve: %v", err))
	}
}