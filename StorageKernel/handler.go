package main

import (
	aml_checker "LedgerDB/services/aml"
	ls "LedgerServer/api"
	ts "StorageKernel/proto/TransactionsStore"
	pb "StorageKernel/proto/worldstate"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"sync"

	"github.com/colinmarc/hdfs/v2"
	"github.com/redis/go-redis/v9"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type KernelHandler struct {
	pb.UnimplementedWorldStateServiceServer
	rdb *redis.Client
	ts.UnimplementedTransactionsStoreServiceServer
	hdfs   *hdfs.Client
	amlURL string
}

var ValidTiers = []string{
	"PERSON",
	"POS",
	"MERCHANT",
}

func (h *KernelHandler) CreateAccount(ctx context.Context, req *pb.CreateAccountRequest) (*pb.CreateAccountResponse, error) {

	file_name := "handler.go"
	merchant_name := ""

	if req.Tier == "MERCHANT" {
		if req.Name != nil {
			merchant_name = *req.Name
		} else {
			logger.Error(" - [" + file_name + "] - Merchant account creation requires a name")
			return nil, status.Error(codes.InvalidArgument, "merchant account creation requires a name")
		}
	}

	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.AccountId,
		"merchant:" + merchant_name,
	}

	if !slices.Contains(ValidTiers, req.Tier) {
		logger.Error(" - [" + file_name + "] - Invalid tier: " + req.Tier)
		return nil, status.Error(codes.InvalidArgument, "invalid tier")
	}

	values := []string{
		fmt.Sprint(req.Balance),
		req.Tier,
	}
	logger.Info(" - [" + file_name + "] - Account Creation with balance " + fmt.Sprint(req.Balance) + " For " + req.AccountId + " with tier " + req.Tier)

	res, err := createAccountScript.Run(ctx, h.rdb, keys, values).Slice()
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil, mapGrpcError(err)
	}

	if len(res) != 2 {
		logger.Error(" - [" + file_name + "] - Unexpected script result: " + fmt.Sprint(res))
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]
	logger.Info(" - [" + file_name + "] - Create Account committed " + fmt.Sprint(sequence))
	return &pb.CreateAccountResponse{Ok: true, Message: "Create Account committed, sequence: " + fmt.Sprint(sequence)}, nil
}

func (h *KernelHandler) OfflineWithdraw(ctx context.Context, req *pb.OfflineWithdrawRequest) (*pb.OfflineWithdrawResponse, error) {
	file_name = "handler.go"

	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.AccountId,
	}
	logger.Info(" - [" + file_name + "] - Offline Withdrawal " + fmt.Sprint(req.Amount) + " For " + req.AccountId)

	res, err := offlineWithdrawScript.Run(ctx, h.rdb, keys, req.Amount).Slice()

	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil, mapGrpcError(err)
	}
	if len(res) != 2 {
		logger.Error(" - [" + file_name + "] - Unexpected script result: " + fmt.Sprint(res))
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]
	logger.Info(" - [" + file_name + "] - Offline Withdrawal committed " + fmt.Sprint(sequence))
	return &pb.OfflineWithdrawResponse{Ok: true, Message: "Offline Withdrawal committed, sequence: " + fmt.Sprint(sequence)}, nil
}

func (h *KernelHandler) OfflineDeposit(ctx context.Context, req *pb.OfflineDepositRequest) (*pb.OfflineDepositResponse, error) {
	file_name = "handler.go"

	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.AccountId,
	}
	logger.Info(" - [" + file_name + "] - Offline Deposit " + fmt.Sprint(req.Amount) + " For " + req.AccountId)

	res, err := offlineDepositScript.Run(ctx, h.rdb, keys, req.Amount).Slice()
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil, mapGrpcError(err)
	}

	if len(res) != 2 {
		logger.Error(" - [" + file_name + "] - Unexpected script result: " + fmt.Sprint(res))
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]
	logger.Info(" - [" + file_name + "] - Offline Deposit committed " + fmt.Sprint(sequence))
	return &pb.OfflineDepositResponse{Ok: true, Message: "Offline Deposit committed, sequence: " + fmt.Sprint(sequence)}, nil
}
func (h *KernelHandler) CommitTransfer(ctx context.Context, req *pb.TransferRequest) (*pb.TransferResponse, error) {

	file_name = "handler.go"
	merchant_name := ""

	to_id := ""

	if req.ToId != nil {
		to_id = "account:" + *req.ToId
	}

	if req.MerchantName != nil {
		merchant_name = "merchant:" + *req.MerchantName
		key := []string{
			merchant_name,
		}

		res, err := getMerchantAccountIdScript.Run(ctx, h.rdb, key).Result()
		if err != nil {
			logger.Error(" - [" + file_name + "] - Failed to get merchant account ID: " + err.Error())
			return nil, mapGrpcError(err)
		}

		to_id = fmt.Sprint(res)
	}

	if to_id == "" && merchant_name == "" {
		logger.Error(" - [" + file_name + "] - Transfer request must have either ToId or MerchantName")
		return nil, status.Error(codes.InvalidArgument, "transfer request must have either ToId or MerchantName")
	}

	keys := []string{
		"nonce:" + req.Nonce + "_commit",
		to_id,
	}

	logger.Info(" - [" + file_name + "] - Transferring " + fmt.Sprint(req.Amount) + " from " + req.FromId + " to " + to_id + " with merchant " + merchant_name)

	res, err := commitTransferScript.Run(ctx, h.rdb, keys, req.Amount).Slice()
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil, mapGrpcError(err)
	}

	if len(res) != 2 {
		logger.Error(" - [" + file_name + "] - Unexpected script result: " + fmt.Sprint(res))
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]
	logger.Info(" - [" + file_name + "] - Transfer committed to redis: " + fmt.Sprint(sequence))

	return &pb.TransferResponse{Ok: true, Message: "transfer committed, sequence: " + fmt.Sprint(sequence)}, nil
}
func (h *KernelHandler) GetMerchantAccountId(ctx context.Context, req *pb.GetMerchantAccountIdRequest) (*pb.GetMerchantAccountIdResponse, error) {
	merchant_name := "merchant:" + req.MerchantName
	key := []string{
		merchant_name,
	}

	res, err := getMerchantAccountIdScript.Run(ctx, h.rdb, key).Result()
	if err != nil {
		logger.Error(" - [" + file_name + "] - Failed to get merchant account ID: " + err.Error())
		return nil, mapGrpcError(err)
	}

	to_id := fmt.Sprint(res)
	to_id = strings.TrimPrefix(to_id, "account:")
	return &pb.GetMerchantAccountIdResponse{AccountId: to_id}, nil
}

func (h *KernelHandler) Transfer(ctx context.Context, req *pb.TransferRequest) (*pb.TransferResponse, error) {

	file_name = "handler.go"
	merchant_name := ""

	to_id := ""
	to_id_without_prefix := ""

	if req.ToId != nil {
		to_id = "account:" + *req.ToId
	}

	if req.MerchantName != nil {
		merchant_name = "merchant:" + *req.MerchantName
		key := []string{
			merchant_name,
		}

		res, err := getMerchantAccountIdScript.Run(ctx, h.rdb, key).Result()
		if err != nil {
			logger.Error(" - [" + file_name + "] - Failed to get merchant account ID: " + err.Error())
			return nil, mapGrpcError(err)
		}

		to_id = fmt.Sprint(res)
	}

	to_id_without_prefix = strings.TrimPrefix(to_id, "account:")
	if to_id == "" && merchant_name == "" {
		logger.Error(" - [" + file_name + "] - Transfer request must have either ToId or MerchantName")
		return nil, status.Error(codes.InvalidArgument, "transfer request must have either ToId or MerchantName")
	}

	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.FromId,
		to_id,
		strconv.FormatBool(req.OfflineTransaction),
	}

	logger.Info(" - [" + file_name + "] - Transferring " + fmt.Sprint(req.Amount) + " from " + req.FromId + " to " + to_id + " with merchant " + merchant_name)

	res, err := transferScript.Run(ctx, h.rdb, keys, req.Amount).Slice()
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil, mapGrpcError(err)
	}

	if len(res) != 2 {
		logger.Error(" - [" + file_name + "] - Unexpected script result: " + fmt.Sprint(res))
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]
	logger.Info(" - [" + file_name + "] - Transfer committed to redis: " + fmt.Sprint(sequence))
	if req.FromId == "BANK_ACCOUNT" {
		return &pb.TransferResponse{Ok: true, Message: "transfer committed, sequence: " + fmt.Sprint(sequence)}, nil
	}
	tiers_response, err := h.GetAccountsTier(ctx, &pb.GetAccountsTierRequest{
		SenderAccountId:   req.FromId,
		ReceiverAccountId: to_id_without_prefix,
	})

	if err != nil {
		logger.Error(" - [" + file_name + "] - Failed to get account tiers, will skip AML Check: " + err.Error())
		return &pb.TransferResponse{Ok: true, Message: "transfer committed, sequence: " + fmt.Sprint(sequence)}, nil
	}

	tx := aml_checker.Transaction{
		Sender:              req.FromId,
		Receiver:            to_id_without_prefix,
		Amount:              float64(req.Amount) / 100, // Convert qorosh to GNEH
		Timestamp:           time.Now(),
		SenderAccountType:   tiers_response.SenderTier,
		ReceiverAccountType: tiers_response.ReceiverTier,
	}

	txCheckResp, err := aml_checker.CheckTransaction(tx, h.amlURL)
	if err != nil {
		logger.Error(" - [" + file_name + "] - AML Check failed (internally): " + err.Error())
		return &pb.TransferResponse{Ok: true, Message: "transfer committed, sequence: " + fmt.Sprint(sequence)}, nil
	}

	if txCheckResp.Status == "rejected" {
		logger.Info(" - [" + file_name + "] - Transfer rejected by AML: " + txCheckResp.Status + " - " + txCheckResp.Reason)
		// roll back transfer
		if req.OfflineTransaction == false {
			// Implementation for rolling back transfer
			keys_revert := []string{
				"nonce:" + req.Nonce + "_rollback",
				"account:" + req.FromId,
				to_id,
				strconv.FormatBool(req.OfflineTransaction),
			}
			_, err := transferRollBackScript.Run(ctx, h.rdb, keys_revert, req.Amount).Slice()
			if err != nil {
				logger.Error(" - [" + file_name + "] - Failed to roll back transfer: " + err.Error())
				return &pb.TransferResponse{Ok: false, Message: "transfer rejected by AML and failed to roll back: " + err.Error()}, err
			}
			logger.Info(" - [" + file_name + "] - rolled back transfer")

			return &pb.TransferResponse{Ok: false, Message: "transfer rejected by AML: " + txCheckResp.Reason}, errors.New("transfer rejected by AML: " + txCheckResp.Reason)
		}
	} else if txCheckResp.Status == "INTERNAL_ERROR" {
		logger.Info(" - [" + file_name + "] - Internal Error in AML: " + txCheckResp.Status + " - " + txCheckResp.Reason)
	} else {
		// approved
		logger.Info(" - [" + file_name + "] - Transfer approved by AML")
	}
	return &pb.TransferResponse{Ok: true, Message: "transfer committed, sequence: " + fmt.Sprint(sequence)}, nil
}
func (h *KernelHandler) ChangeAccountStatus(ctx context.Context, req *pb.ChangeAccountStatusRequest) (*pb.ChangeAccountStatusResponse, error) {
	keys := []string{
		"account:" + req.AccountId,
	}
	if req.Score == nil {
		req.Score = proto.Float32(-1)
	}
	if req.Reason == nil {
		req.Reason = proto.String("")
	}
	values := []string{
		req.Status,
		*req.Reason,
		fmt.Sprintf("%f", *req.Score),
	}
	logger.Info(" - [" + file_name + "] - Account Status Update for " + req.AccountId + " to " + req.Status)

	res, err := changeAccountStatusScript.Run(ctx, h.rdb, keys, values).Slice()
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil, mapGrpcError(err)
	}
	if len(res) != 2 {
		logger.Error(" - [" + file_name + "] - Unexpected script result: " + fmt.Sprint(res))
		return nil, status.Error(codes.Internal, "unexpected script result")
	}
	sequence := res[1]
	logger.Info(" - [" + file_name + "] - Account " + req.AccountId + " status updated, sequence: " + fmt.Sprint(sequence))
	return &pb.ChangeAccountStatusResponse{Ok: true, Message: "Account status updated, sequence: " + fmt.Sprint(sequence)}, nil
}
func (h *KernelHandler) GetAccountsTier(ctx context.Context, req *pb.GetAccountsTierRequest) (*pb.GetAccountsTierResponse, error) {
	keys := []string{
		"account:" + req.SenderAccountId,
		"account:" + req.ReceiverAccountId,
	}
	logger.Info(" - [" + file_name + "] - Account Tier Request for " + req.SenderAccountId + " and " + req.ReceiverAccountId)

	res, err := getAccountsTierScript.Run(ctx, h.rdb, keys).Slice()
	if err != nil {
		logger.Error(" - [" + file_name + "] - " + err.Error())
		return nil, mapGrpcError(err)
	}
	if len(res) != 2 {
		logger.Error(" - [" + file_name + "] - Unexpected script result: " + fmt.Sprint(res))
		return nil, status.Error(codes.Internal, "unexpected script result")
	}
	tiers := []string{
		fmt.Sprint(res[0]),
		fmt.Sprint(res[1]),
	}
	logger.Info(" - [" + file_name + "] - Account " + req.SenderAccountId + " tier is " + tiers[0] + " and Account " + req.ReceiverAccountId + " tier is " + tiers[1])
	return &pb.GetAccountsTierResponse{Ok: true, Message: "Accounts' tier received", SenderTier: tiers[0], ReceiverTier: tiers[1]}, nil
}

// %%%%%%%%%%%% JUST TESTING %%%%%%%%%%%%%
// func (h *KernelHandler) StoreBatch(transactions []*ts.Transaction, fileName string) ([]*ts.TransactionReceipt, bool, error) {
// 	txData, err := json.Marshal(transactions)
// 	if err != nil {
// 		logger.Error(" - [" + fileName + "] - " + err.Error())
// 		return nil, false, err
// 	}

// 	ledgerDir := "/ledger/transactions"
// 	err = h.hdfs.MkdirAll(ledgerDir, 0755)
// 	if err != nil {
// 		logger.Error(" - [" + fileName + "] - failed to create hdfs dir: " + err.Error())
// 		return nil, false, err
// 	}

// 	batchId := fmt.Sprintf("batch_%d", time.Now().UnixNano())
// 	filePath := fmt.Sprintf("%s/%s.json", ledgerDir, batchId)

// 	writer, err := h.hdfs.Create(filePath)
// 	if err != nil {
// 		logger.Error(" - [" + fileName + "] - failed to create file: " + err.Error())
// 		return nil, false, err
// 	}
// 	defer writer.Close()

// 	_, err = writer.Write(txData)
// 	if err != nil {
// 		logger.Error(" - [" + fileName + "] - failed to write data: " + err.Error())
// 		return nil, false, err
// 	}

// 	logger.Info(" - [" + fileName + "] - Successfully stored batch: " + batchId)

// 	// receipts := h.GenerateReceipts(transactions, fileName)

// 	return true, nil
// }

// func (h *KernelHandler) GenerateReceipts(transactions []*ts.Transaction, fileName string) []*ts.TransactionReceipt {
// 	var receipts []*ts.TransactionReceipt
// 	for _, tx := range transactions {
// 		receipt := &ts.TransactionReceipt{
// 			TransactionId: tx.Hash,
// 			Status:        tx.Status,
// 			FromWallet:    tx.FromWallet,
// 			ToWallet:      tx.ToWallet,
// 			Amount:        tx.Amount,
// 			Message:       tx.Message,
// 			TimeStamp:     tx.TimeStamp,
// 		}
// 		receipts = append(receipts, receipt)

// 		receiptJson, err := json.MarshalIndent(receipt, "", "  ")
// 		if err == nil {
// 			fmt.Printf("--- RECEIPT GENERATED ---\n%s\n-------------------------\n", string(receiptJson))
// 		}
// 	}
// 	return receipts
// }

// %%%%%%%%%%% END JUST TESTING %%%%%%%%%%%%

func (h *KernelHandler) Store(ctx context.Context, req *anypb.Any) (*ts.StoreAck, error) {
	fileName := "handler.go"
	logger.Info(" - [" + fileName + "] - Storing Batch")

	batchId, err := h.writeBatchToHDFS(req, fileName)
	if err != nil {
		return nil, err
	}

	logger.Info(" - [" + fileName + "] - Successfully stored batch: " + batchId)
	return &ts.StoreAck{
		Success: true,
		BatchId: batchId,
	}, nil
}

func (h *KernelHandler) writeBatchToHDFS(req *anypb.Any, fileName string) (string, error) {

	var batch ls.BatchToAppend
	if err := req.UnmarshalTo(&batch); err != nil {
		logger.Error(" - [" + fileName + "] - failed to unmarshal batch: " + err.Error())
		return "", status.Error(codes.Internal, "failed to unmarshal batch")
	}

	type result struct {
		ind int
		data json.RawMessage
		err error
	}

	numLogs := len(batch.Logs)
	results := make([]json.RawMessage, numLogs)
    errCh := make(chan error, 1)
    resCh := make(chan result, numLogs)

	var wait sync.WaitGroup

	marshaler := protojson.MarshalOptions{EmitUnpopulated: false}

	for i, anyMsg := range batch.Logs {
		wait.Add(1)
		go func(idx int, msg proto.Message) {
            defer wait.Done()
            jsonBytes, err := marshaler.Marshal(msg)
            if err != nil {
                select {
                case errCh <- fmt.Errorf("entry %d: %w", idx, err):
                default:
                }
                return
            }
            resCh <- result{ind: idx, data: json.RawMessage(jsonBytes)}
        }(i, anyMsg)
	}

	go func() {
		wait.Wait()
		close(resCh)
	}()

	for r := range resCh {
        results[r.ind] = r.data
    }
	
	select {
		case err := <-errCh:
			logger.Error(" - [" + fileName + "] - failed to marshal batch entry: " + err.Error())
			return "", status.Error(codes.Internal, "failed to marshal batch entry")
		default:
			// no error, continue
	}

	var txData   []byte

    ledgerDir := "/ledger/transactions"
    mkdirErrCh := make(chan error, 1)
    marshalErrCh := make(chan error, 1)

    go func() {
        mkdirErrCh <- h.hdfs.MkdirAll(ledgerDir, 0755)
    }()

    go func() {
        data, err := json.Marshal(results)
        if err != nil {
            marshalErrCh <- err
            return
        }
        txData = data
        marshalErrCh <- nil
    }()

    if err := <-marshalErrCh; err != nil {
        logger.Error(fmt.Sprintf(" - [%s] - failed to marshal JSON array: %v", fileName, err))
        return "", status.Error(codes.Internal, "failed to marshal batch JSON")
    }

    if err := <-mkdirErrCh; err != nil {
        logger.Error(fmt.Sprintf(" - [%s] - failed to create HDFS directory: %v", fileName, err))
        return "", status.Error(codes.Internal, "failed to create HDFS directory")
    }

	batchId := fmt.Sprintf("batch_%d", time.Now().UnixNano())
	filePath := fmt.Sprintf("%s/%s.json", ledgerDir, batchId)

	writer, err := h.hdfs.Create(filePath)
	if err != nil {
		logger.Error(" - [" + fileName + "] - failed to create file: " + err.Error())
		return "", status.Error(codes.Internal, "failed to create file in HDFS")
	}
	defer func() {
		if cerr := writer.Close(); cerr != nil {
			logger.Error(" - [" + fileName + "] - failed to close writer: " + cerr.Error())
		}
	}()
	

	if _, err = writer.Write(txData); err != nil {
		logger.Error(" - [" + fileName + "] - failed to write data: " + err.Error())
		return "", status.Error(codes.Internal, "failed to write data to HDFS")
	}

	return batchId, nil
}

// func (h *KernelHandler) generateReceipts(transactions []*ts.Transaction, fileName string) []*ts.TransactionReceipt {
// 	var receipts []*ts.TransactionReceipt
// 	for _, tx := range transactions {
// 		logger.Info(" - [" + fileName + "] - Generating receipt: " + tx.Hash)
// 		receipts = append(receipts, &ts.TransactionReceipt{
// 			TransactionId: tx.Hash,
// 			Status:        tx.Status,
// 			FromWallet:    tx.FromWallet,
// 			ToWallet:      tx.ToWallet,
// 			Amount:        tx.Amount,
// 			Message:       tx.Message,
// 		})
// 	}
// 	return receipts
// }
