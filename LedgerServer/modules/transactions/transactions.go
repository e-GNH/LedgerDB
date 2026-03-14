package transactions

import (
	"context"
	"fmt"
	"time"

	"LedgerDB/services/logging"

	pb "LedgerServer/api"
	ts_store "StorageKernel/proto/TransactionsStore"
)


var logger = logging.New("server", "./")

type LedgerServer struct {
	pb.UnimplementedTransactionsServiceServer
	
	TransactionsStoreClient ts_store.TransactionsStoreServiceClient
}



func (s *LedgerServer) BatchAppend(ctx context.Context, req *pb.TransactionsBatch) (*pb.ServerResponse, error) {

	logger.Info("--> Received gRPC BatchAppend() request")
	if len(req.Transactions) == 0 {
        return &pb.ServerResponse{Success: false}, nil
    }
	storeReq := &ts_store.TransactionBatchRequest{}

	logger.Info("Iterating over batches to store")
    for _, tx := range req.Transactions { 
        storeReq.Transactions = append(storeReq.Transactions, &ts_store.Transaction{
			Status:    tx.Status,
            FromWallet:      tx.FromWallet,
            ToWallet:        tx.ToWallet,
            Amount:    tx.Amount,
            Message:   tx.Message,
            Nonce:     tx.Nonce,
            Hash:      tx.Hash,
			TimeStamp: time.Now().Format(time.RFC3339),
        })
    }

	_, err := s.TransactionsStoreClient.Store(ctx, storeReq) 
    if err != nil {
        logger.Error(fmt.Sprintf("Storage Kernel rejected batch: %v", err))
        return &pb.ServerResponse{
            Success: false,
        }, err 
    }

    logger.Info("Batch successfully saved to Storage Kernel")

    return &pb.ServerResponse{
        Success: true,
    }, nil
}
