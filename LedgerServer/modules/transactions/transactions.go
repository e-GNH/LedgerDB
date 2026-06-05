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
	pb.UnimplementedReceiptServiceServer

	TransactionsStoreClient ts_store.TransactionsStoreServiceClient
	Registry                *BankRegistry
}

func (s *LedgerServer) BatchAppend(ctx context.Context, req *pb.TransactionsBatch) (*pb.ServerResponse, error) {
	logger.Info("--> Received gRPC BatchAppend() request")

	if len(req.Transactions) == 0 {
		return &pb.ServerResponse{Success: false}, nil
	}

	// Build store request for StorageKernel
	storeReq := &ts_store.TransactionBatchRequest{}
	for _, tx := range req.Transactions {
		storeReq.Transactions = append(storeReq.Transactions, &ts_store.Transaction{
			Status:     tx.Status,
			FromWallet: tx.FromWallet,
			ToWallet:   tx.ToWallet,
			Amount:     tx.Amount,
			Message:    tx.Message,
			Nonce:      tx.Nonce,
			Hash:       tx.Hash,
			TimeStamp:  time.Now().Format(time.RFC3339),
		})
	}

	ack, err := s.TransactionsStoreClient.Store(ctx, storeReq)
	if err != nil {
		logger.Error(fmt.Sprintf("Storage Kernel rejected batch: %v", err))
		return &pb.ServerResponse{Success: false}, err
	}
	if !ack.Success {
		logger.Error("Storage Kernel returned failure ack")
		return &pb.ServerResponse{Success: false}, nil
	}

	logger.Info(fmt.Sprintf("Batch committed by Storage Kernel, batch_id=%s", ack.BatchId))

	// Generate and stream receipts to banks
	s.streamReceipts(req.Transactions)

	return &pb.ServerResponse{Success: true}, nil
}

// streamReceipts routes receipts to the correct subscribed banks.
// If both wallets share the same prefix, only one receipt is sent.
func (s *LedgerServer) streamReceipts(transactions []*pb.Transaction) {
	for _, tx := range transactions {
		fromPrefix := walletPrefix(tx.FromWallet)
		toPrefix := walletPrefix(tx.ToWallet)

		receipt := &pb.TransactionReceipt{
			Hash:       tx.Hash,
			Status:     tx.Status,
			FromWallet: tx.FromWallet,
			ToWallet:   tx.ToWallet,
			Amount:     tx.Amount,
			Message:    tx.Message,
			Nonce:      tx.Nonce,
			TimeStamp:  time.Now().Format(time.RFC3339),
		}

		// Send to from-bank
		s.sendReceipt(fromPrefix, receipt)

		// Send to to-bank only if it's a different bank
		if toPrefix != fromPrefix {
			s.sendReceipt(toPrefix, receipt)
		}
	}
}

func (s *LedgerServer) sendReceipt(prefix string, receipt *pb.TransactionReceipt) {
	bs := s.Registry.Get(prefix)
	if bs == nil {
		logger.Info(fmt.Sprintf("No active subscription for prefix %s, skipping receipt", prefix))
		return
	}

	if err := bs.stream.Send(receipt); err != nil {
		logger.Error(fmt.Sprintf("Failed to stream receipt to bank %s: %v", prefix, err))
		s.Registry.Unregister(prefix)
	}
}

// Subscribe is the long-lived streaming RPC banks call to receive receipts
func (s *LedgerServer) Subscribe(req *pb.SubscribeRequest, stream pb.ReceiptService_SubscribeServer) error {
	prefix := req.BankPrefix
	if prefix == "" {
		return fmt.Errorf("bank_prefix is required")
	}

	logger.Info(fmt.Sprintf("Bank subscribed with prefix: %s", prefix))
	bs := s.Registry.Register(prefix, stream)

	// Block until the client disconnects or context is cancelled
	select {
	case <-stream.Context().Done():
		logger.Info(fmt.Sprintf("Bank %s disconnected", prefix))
	case <-bs.done:
		logger.Info(fmt.Sprintf("Bank %s stream closed by server", prefix))
	}

	s.Registry.Unregister(prefix)
	return nil
}

// walletPrefix returns the first 3 characters of a wallet ID as the bank prefix
func walletPrefix(walletID string) string {
	if len(walletID) < 3 {
		return walletID
	}
	return walletID[:3]
}
