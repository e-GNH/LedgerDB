package main

import (
	ts "StorageKernel/proto/TransactionsStore"
	pb "StorageKernel/proto/worldstate"

	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/colinmarc/hdfs/v2"
	"github.com/redis/go-redis/v9"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type KernelHandler struct {
	pb.UnimplementedWorldStateServiceServer
	rdb *redis.Client
	ts.UnimplementedTransactionsStoreServiceServer
	hdfs *hdfs.Client
}

func (h *KernelHandler) CreateAccount(ctx context.Context, id string, balance int64) error { // TODO FINISH AFTER ONBOARDING
	return h.rdb.HSet(ctx, "account:"+id,
		"balance", balance,
		"pending", 0,
	).Err()
}
func (h *KernelHandler) Transfer(ctx context.Context, req *pb.TransferRequest) (*pb.TransferResponse, error) {
	file_name = "handler.go"
	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.FromId,
		"account:" + req.ToId,
	}
	logger.Info(" - [" + file_name + "] - Transferring " + fmt.Sprint(req.Amount) + " from " + req.FromId + " to " + req.ToId)

	err := transferScript.Run(ctx, h.rdb, keys, req.Amount).Err()
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil, mapGrpcError(err)
	}
	logger.Info(" - [" + file_name + "] - Transfer committed")
	return &pb.TransferResponse{Ok: true, Message: "transfer committed"}, nil
}
func (h *KernelHandler) Transfer_Test(ctx context.Context, nonce, from, to string, amount int64) error {
	keys := []string{
		"nonce:" + nonce,
		"account:" + from,
		"account:" + to,
	}
	err := transferScript.Run(ctx, h.rdb, keys, amount).Err()
	return mapLuaError(err)
}

// %%%%%%%%%%%% JUST TESTING %%%%%%%%%%%%%
func (h *KernelHandler) StoreBatch(transactions []*ts.Transaction, fileName string) ([]*ts.TransactionReceipt, bool, error) {
	txData, err := json.Marshal(transactions)
	if err != nil {
		logger.Error(" - [" + fileName + "] - " + err.Error())
		return nil, false, err
	}

	ledgerDir := "/ledger/transactions"
	err = h.hdfs.MkdirAll(ledgerDir, 0755)
	if err != nil {
		logger.Error(" - [" + fileName + "] - failed to create hdfs dir: " + err.Error())
		return nil, false, err
	}

	batchId := fmt.Sprintf("batch_%d", time.Now().UnixNano())
	filePath := fmt.Sprintf("%s/%s.json", ledgerDir, batchId)

	writer, err := h.hdfs.Create(filePath)
	if err != nil {
		logger.Error(" - [" + fileName + "] - failed to create file: " + err.Error())
		return nil, false, err
	}
	defer writer.Close()

	_, err = writer.Write(txData)
	if err != nil {
		logger.Error(" - [" + fileName + "] - failed to write data: " + err.Error())
		return nil, false, err
	}

	logger.Info(" - [" + fileName + "] - Successfully stored batch: " + batchId)

	receipts := h.GenerateReceipts(transactions, fileName)

	return receipts, true, nil
}

func (h *KernelHandler) GenerateReceipts(transactions []*ts.Transaction, fileName string) []*ts.TransactionReceipt {
	var receipts []*ts.TransactionReceipt
	for _, tx := range transactions {
		receipt := &ts.TransactionReceipt{
			TransactionId: tx.Hash,
			Status:        tx.Status,
			FromWallet:    tx.FromWallet,
			ToWallet:      tx.ToWallet,
			Amount:        tx.Amount,
			Message:       tx.Message,
			TimeStamp:     tx.TimeStamp,
		}
		receipts = append(receipts, receipt)

		receiptJson, err := json.MarshalIndent(receipt, "", "  ")
		if err == nil {
			fmt.Printf("--- RECEIPT GENERATED ---\n%s\n-------------------------\n", string(receiptJson))
		}
	}
	return receipts
}

// %%%%%%%%%%% END JUST TESTING %%%%%%%%%%%%

func (h *KernelHandler) Store(ctx context.Context, req *ts.TransactionBatchRequest) (*ts.TransactionBatchResponse, error) {
	fileName := "handler.go"
	logger.Info(" - [" + fileName + "] - Storing Transaction Batch")

	batchId, err := h.writeBatchToHDFS(req.Transactions, fileName)
	if err != nil {
		return nil, err
	}

	receipts := h.generateReceipts(req.Transactions, fileName)

	logger.Info(" - [" + fileName + "] - Successfully stored batch: " + batchId)
	return &ts.TransactionBatchResponse{
		Receipts: receipts,
	}, nil
}

func (h *KernelHandler) writeBatchToHDFS(transactions []*ts.Transaction, fileName string) (string, error) {
	logger.Info(" - [" + fileName + "] - Converting batch to JSON")
	txData, err := json.Marshal(transactions)
	if err != nil {
		logger.Error(" - [" + fileName + "] - " + err.Error())
		return "", status.Error(codes.Internal, "failed to marshal transaction data")
	}

	ledgerDir := "/ledger/transactions"
	err = h.hdfs.MkdirAll(ledgerDir, 0755)
	if err != nil {
		logger.Error(" - [" + fileName + "] - failed to create hdfs dir: " + err.Error())
		return "", status.Error(codes.Internal, "failed to create hdfs directory")
	}

	batchId := fmt.Sprintf("batch_%d", time.Now().UnixNano())
	filePath := fmt.Sprintf("%s/%s.json", ledgerDir, batchId)

	writer, err := h.hdfs.Create(filePath)
	if err != nil {
		logger.Error(" - [" + fileName + "] - failed to create file: " + err.Error())
		return "", status.Error(codes.Internal, "failed to create file in HDFS")
	}
	defer writer.Close()

	_, err = writer.Write(txData)
	if err != nil {
		logger.Error(" - [" + fileName + "] - failed to write data: " + err.Error())
		return "", status.Error(codes.Internal, "failed to write data to HDFS")
	}
	logger.Info(" - [" + fileName + "] - Converted batch to JSON")
	return batchId, nil
}

func (h *KernelHandler) generateReceipts(transactions []*ts.Transaction, fileName string) []*ts.TransactionReceipt {
	var receipts []*ts.TransactionReceipt
	for _, tx := range transactions {
		logger.Info(" - [" + fileName + "] - Generating receipt: " + tx.Hash)
		receipts = append(receipts, &ts.TransactionReceipt{
			TransactionId: tx.Hash,
			Status:        tx.Status,
			FromWallet:    tx.FromWallet,
			ToWallet:      tx.ToWallet,
			Amount:        tx.Amount,
			Message:       tx.Message,
		})
	}
	return receipts
}
