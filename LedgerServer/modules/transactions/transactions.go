package transactions

import (
	"context"
	"fmt"

	"LedgerDB/services/logging"

	ledgerserverpb "LedgerServer/api"
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

func extractTransaction(a *anypb.Any) (*worldstate.TransferRequest, bool) {
	if !a.MessageIs(&ledgerserverpb.Transaction{}) {
		return nil, false
	}
	var tx_object ledgerserverpb.Transaction
	if err := a.UnmarshalTo(&tx_object); err != nil {
		logger.Info(fmt.Sprintf("Failed to unmarshal transaction: this was the object provided: %+v", a))
		return nil, false
	}
	// print tx
	to_wallet_string := tx_object.ToWallet
	to_id := &to_wallet_string
	if to_wallet_string == "" {
		to_id = nil
	}
	tx := worldstate.TransferRequest{
		FromId:             tx_object.FromWallet,
		ToId:               to_id,
		Amount:             tx_object.Amount,
		Nonce:              tx_object.Nonce,
		MerchantName:       tx_object.MerchantName,
		OfflineTransaction: true,
	}
	logger.Info(fmt.Sprintf("we extracted transaction from %v to %v with amount %v merchant %v", tx.FromId, to_wallet_string, tx.Amount, tx.MerchantName))
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
