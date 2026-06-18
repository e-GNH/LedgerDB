package transactions

import (
	"context"
	"fmt"

	"LedgerDB/services/logging"

	pb "LedgerServer/api"
	ts_store "StorageKernel/proto/TransactionsStore"
	worldstate "StorageKernel/proto/worldstate"

	"google.golang.org/protobuf/types/known/anypb"
)

var logger = logging.New("server", "./")

type LedgerServer struct {
	pb.UnimplementedTransactionsServiceServer

	TransactionsStoreClient ts_store.TransactionsStoreServiceClient
}

// TODO: Zeyad, I don't remeber which object you would like to map.
type DummyTransaction struct {
	From      string
	To        string
	Amount    int64
	Timestamp string
	Nonce     string
	Status    bool
}

func extractTransaction(a *anypb.Any) (*worldstate.TransferRequest, bool) {
	if !a.MessageIs(&worldstate.TransferRequest{}) {
		return nil, false
	}
	var tx worldstate.TransferRequest
	if err := a.UnmarshalTo(&tx); err != nil {
		return nil, false
	}
	// print tx
	logger.Info(fmt.Sprintf("%+v is the transaction extracted from the batch", tx))
	return &tx, true
}

func (s *LedgerServer) BatchAppend(ctx context.Context, req *pb.BatchToAppend) (*pb.ServerResponse, error) {
	logger.Info("--> Received gRPC BatchAppend() request")
	if len(req.Logs) == 0 {
		return &pb.ServerResponse{Success: false}, nil
	}

	for _, item := range req.Logs {
		tx, ok := extractTransaction(item)
		if !ok {
			logger.Error("Failed to unmarshal transaction")
			// TODO: Zeyad
			continue
		}
		_ = tx
		// TODO: Zeyad
	}

	batch := &pb.BatchToAppend{Logs: req.Logs}
	anyReq, err := anypb.New(batch)
	if err != nil {
		return nil, err
	}
	ack, err := s.TransactionsStoreClient.Store(ctx, anyReq)
	if err != nil {
		logger.Error(fmt.Sprintf("Storage Kernel rejected batch: %v", err))
		return &pb.ServerResponse{Success: false}, err
	}
	if !ack.Success {
		logger.Error("Storage Kernel returned failure ack")
		return &pb.ServerResponse{Success: false}, nil
	}
	logger.Info(fmt.Sprintf("Batch committed by Storage Kernel, batch_id=%s", ack.BatchId))
	return &pb.ServerResponse{Success: true}, nil
}
