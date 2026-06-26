package main

import (
	aml_checker "LedgerDB/services/aml"
	ls "LedgerServer/api"
	ts "StorageKernel/proto/TransactionsStore"
	pb "StorageKernel/proto/worldstate"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/colinmarc/hdfs/v2"
	"github.com/redis/go-redis/v9"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
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
	merchant_name := ""

	if req.Tier == "MERCHANT" {
		if req.Name != nil {
			merchant_name = *req.Name
		} else {
			logger.Error("merchants need to have merchant name")
			return nil, status.Error(codes.Internal, "merchants need to have a merchant name")
		}
	}
	if !slices.Contains(ValidTiers, req.Tier) {
		logger.Error("account tier isn't valid")
		return nil, status.Error(codes.Internal, "account tier isn't valid")
	}
	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.AccountId,
		"merchant:" + merchant_name,
	}
	args := []string{
		fmt.Sprint(req.Balance),
		req.Tier,
	}

	res, err := createAccountScript.Run(ctx, h.rdb, keys, args).Slice()

	if err != nil {
		logger.Error("Error at account creation")
		return nil, mapGrpcError(err)
	}

	if len(res) != 2 {
		logger.Error("unnexpected script result")
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]
	logger.Info("Account Creation successful sequence: " + fmt.Sprint(sequence))

	resp := pb.CreateAccountResponse{
		Ok:      true,
		Message: "Account Creation Successful sequence: " + fmt.Sprint(sequence),
	}
	return &resp, nil

}

func (h *KernelHandler) OfflineWithdraw(ctx context.Context, req *pb.OfflineWithdrawRequest) (*pb.OfflineWithdrawResponse, error) {
	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.AccountId,
	}
	res, err := offlineWithdrawScript.Run(ctx, h.rdb, keys, req.Amount).Slice()

	if err != nil {
		logger.Error("Error at offline withdraw")
		return nil, mapGrpcError(err)
	}

	if len(res) != 2 {
		logger.Error("unnexpected script result")
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]
	logger.Info("offline withdraw successful sequence: " + fmt.Sprint(sequence))

	resp := pb.OfflineWithdrawResponse{
		Ok:      true,
		Message: "offline withdraw successful sequence: " + fmt.Sprint(sequence),
	}
	return &resp, nil

}

func (h *KernelHandler) OfflineDeposit(ctx context.Context, req *pb.OfflineDepositRequest) (*pb.OfflineDepositResponse, error) {
	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.AccountId,
	}
	res, err := offlineDepositScript.Run(ctx, h.rdb, keys, req.Amount).Slice()

	if err != nil {
		logger.Error("Error at offline deposit")
		return nil, mapGrpcError(err)
	}

	if len(res) != 2 {
		logger.Error("unnexpected script result")
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]
	logger.Info("offline deposit successful sequence: " + fmt.Sprint(sequence))

	resp := pb.OfflineDepositResponse{
		Ok:      true,
		Message: "offline deposit successful sequence: " + fmt.Sprint(sequence),
	}
	return &resp, nil

}

func (h *KernelHandler) CommitTransfer(ctx context.Context, req *pb.TransferRequest) (*pb.TransferResponse, error) {
	to_id := ""
	to_id_without_prefix := ""

	// get to_id if not there
	if req.ToId != nil {
		to_id = *req.ToId
	} else if req.MerchantName != nil {
		request_merchant := pb.GetMerchantAccountIdRequest{
			MerchantName: *req.MerchantName,
		}
		resp, err := h.GetMerchantAccountId(ctx, &request_merchant)
		if err != nil {
			logger.Error("Couldn't get merchant account id")
			return nil, status.Error(codes.InvalidArgument, "merchant doesn't exist")
		}
		to_id = resp.AccountId

	} else {
		logger.Error("Either merchant or to account is required")
		return nil, status.Error(codes.Internal, "unexpected grpc input")
	}
	to_id_without_prefix = strings.TrimPrefix(to_id, "account:")

	keys := []string{
		"nonce:" + req.Nonce + "_commit",
		"account:" + to_id_without_prefix,
	}

	logger.Info(fmt.Sprintf("Transfer request from %s to %s with amount %v", req.FromId, to_id_without_prefix, req.Amount))

	res, err := commitTransferScript.Run(ctx, h.rdb, keys, req.Amount).Slice()

	if err != nil {
		logger.Error("Failed to commit transaction")
		return nil, mapGrpcError(err)
	}

	if len(res) != 2 {
		logger.Error("unnexpected script result")
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]

	resp := pb.TransferResponse{

		Ok:      true,
		Message: "Transfer committed sequence: " + fmt.Sprint(sequence),
	}
	return &resp, nil
}

func (h *KernelHandler) GetMerchantAccountId(ctx context.Context, req *pb.GetMerchantAccountIdRequest) (*pb.GetMerchantAccountIdResponse, error) {
	merchant_name := []string{
		"merchant:" + req.MerchantName,
	}
	res, err := getMerchantAccountIdScript.Run(ctx, h.rdb, merchant_name).Result()
	if err != nil {
		logger.Error("Error at getting merchant account ID")
		return nil, mapGrpcError(err)
	}
	merchant := fmt.Sprint(res)
	merchant_trimmed := strings.TrimPrefix(merchant, "account:")
	resp := pb.GetMerchantAccountIdResponse{
		AccountId: merchant_trimmed,
	}
	logger.Info("account for merchant is " + merchant_trimmed)
	return &resp, nil
}
func (h *KernelHandler) Transfer(ctx context.Context, req *pb.TransferRequest) (*pb.TransferResponse, error) {

	to_id := ""
	to_id_without_prefix := ""

	// get to_id if not there
	if req.ToId != nil {
		to_id = *req.ToId
	} else if req.MerchantName != nil {
		request_merchant := pb.GetMerchantAccountIdRequest{
			MerchantName: *req.MerchantName,
		}
		resp, err := h.GetMerchantAccountId(ctx, &request_merchant)
		if err != nil {
			logger.Error("Couldn't get merchant account id")
			return nil, status.Error(codes.InvalidArgument, "merchant doesn't exist")
		}
		to_id = resp.AccountId

	} else {
		logger.Error("Either merchant or to account is required")
		return nil, status.Error(codes.Internal, "unexpected grpc input")
	}
	to_id_without_prefix = strings.TrimPrefix(to_id, "account:")

	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.FromId,
		"account:" + to_id_without_prefix,
		fmt.Sprint(req.OfflineTransaction),
	}

	logger.Info(fmt.Sprintf("Transfer request from %s to %s with amount %v", req.FromId, to_id_without_prefix, req.Amount))

	res, err := transferScript.Run(ctx, h.rdb, keys, req.Amount).Slice()

	if err != nil {
		logger.Error("Failed to add transaction")
		return nil, mapGrpcError(err)
	}

	if len(res) != 2 {
		logger.Error("unnexpected script result")
		return nil, status.Error(codes.Internal, "unexpected script result")
	}

	sequence := res[1]

	if req.FromId == "BANK_ACCOUNT" {
		resp := pb.TransferResponse{

			Ok:      true,
			Message: "Transfer added to redis sequence: " + fmt.Sprint(sequence),
		}
		return &resp, nil
	}

	// get account tiers for AML

	get_accounts_req := pb.GetAccountsTierRequest{
		SenderAccountId:   req.FromId,
		ReceiverAccountId: to_id_without_prefix,
	}
	tiers_response, err := h.GetAccountsTier(ctx, &get_accounts_req)

	if err != nil {
		logger.Error("couldn't get account tiers skipping aml checks")
		response := pb.TransferResponse{
			Ok:      true,
			Message: "transfer added to db sequence: " + fmt.Sprint(sequence),
		}
		return &response, nil
	}

	tx := aml_checker.Transaction{
		Sender:              req.FromId,
		Receiver:            to_id_without_prefix,
		Amount:              float64(req.Amount) / 100,
		Timestamp:           time.Now(),
		SenderAccountType:   tiers_response.SenderTier,
		ReceiverAccountType: tiers_response.ReceiverTier,
	}

	txCheckResp, err := aml_checker.CheckTransaction(tx, h.amlURL)

	if err != nil {
		logger.Error("couldn't run aml checks committing transaction")
		response := pb.TransferResponse{
			Ok:      true,
			Message: "transfer added to db sequence: " + fmt.Sprint(sequence),
		}
		return &response, nil
	}

	if txCheckResp.Status == "INTERNAL_ERROR" {

		logger.Error("aml checks failed internally committing transaction")
		response := pb.TransferResponse{
			Ok:      true,
			Message: "transfer added to db sequence: " + fmt.Sprint(sequence),
		}
		return &response, nil

	} else if txCheckResp.Status == "rejected" {

		if req.OfflineTransaction == false {
			logger.Info("transaction failed aml checks for reason " + txCheckResp.Reason)
			keys_rollback := []string{
				"nonce:" + req.Nonce + "_rollback",
				"account:" + req.FromId,
				"account:" + to_id_without_prefix,
				fmt.Sprint(req.OfflineTransaction),
			}
			undo_resp, err := transferRollBackScript.Run(ctx, h.rdb, keys_rollback, req.Amount).Slice()
			if err != nil {
				logger.Error("couldn't run rollback transaction")
				response := pb.TransferResponse{
					Ok:      true,
					Message: "transfer added to db sequence: " + fmt.Sprint(sequence),
				}
				return &response, nil
			}
			sequence = undo_resp[0]
			response := pb.TransferResponse{
				Ok:      false,
				Message: "transfer rolled back sequence: " + fmt.Sprint(sequence),
			}
			return &response, nil
		}

	} else {

		logger.Info("AML Checks passed")
		response := pb.TransferResponse{
			Ok:      true,
			Message: "transfer added to db sequence: " + fmt.Sprint(sequence),
		}
		return &response, nil

	}
	response := pb.TransferResponse{
		Ok:      true,
		Message: "transfer added to db sequence: " + fmt.Sprint(sequence),
	}
	return &response, nil
}

func (h *KernelHandler) ChangeAccountStatus(ctx context.Context, req *pb.ChangeAccountStatusRequest) (*pb.ChangeAccountStatusResponse, error) {
	keys := []string{
		"account:" + req.AccountId,
	}
	score := "-1"
	if req.Score != nil {
		score = fmt.Sprint(*req.Score)
	}
	reason := ""
	if req.Reason != nil {
		reason = *req.Reason
	}
	args := []string{
		req.Status,
		reason,
		score,
	}
	if req.Status != "active" && req.Status != "banned" && req.Status != "flagged" {
		logger.Error("Error at changing account, status is wrong")
		return nil, status.Error(codes.Internal, "unexpected script input wrong status for user")
	}
	res, err := changeAccountStatusScript.Run(ctx, h.rdb, keys, args).Slice()
	if err != nil {
		logger.Error("Error at changing account status")
		return nil, mapGrpcError(err)
	}
	if len(res) != 2 {
		logger.Error("Error at changing account status unexpected res")
		return nil, status.Error(codes.Internal, "unexpected script result")
	}
	logger.Info("Successfuly changed account status")
	sequence := res[1]
	resp := pb.ChangeAccountStatusResponse{
		Ok:      true,
		Message: "changed account status sequence: " + fmt.Sprint(sequence),
	}
	return &resp, nil
}

func (h *KernelHandler) GetAccountsTier(ctx context.Context, req *pb.GetAccountsTierRequest) (*pb.GetAccountsTierResponse, error) {
	// get keys
	keys := []string{
		"account:" + req.SenderAccountId,
		"account:" + req.ReceiverAccountId,
	}
	// run script
	res, err := getAccountsTierScript.Run(ctx, h.rdb, keys).Slice()
	// validate el errors
	if err != nil {
		logger.Error("error in getting account tiers" + err.Error())
		return nil, mapGrpcError(err)
	}
	if len(res) != 2 {
		logger.Error("error in getting account tiers expected to get 2 tiers")
		return nil, status.Error(codes.Internal, "tiers script returned wrong results")
	}
	// get tiers
	tiers := []string{
		fmt.Sprint(res[0]),
		fmt.Sprint(res[1]),
	}
	logger.Info("received tiers from world state")
	// return response
	response := pb.GetAccountsTierResponse{
		Ok:           true,
		Message:      "Tiers Received",
		SenderTier:   tiers[0],
		ReceiverTier: tiers[1],
	}
	return &response, nil
}

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

	marshaler := protojson.MarshalOptions{EmitUnpopulated: false}
	entries := make([]json.RawMessage, 0, len(batch.Logs))

	for _, anyMsg := range batch.Logs {
		jsonBytes, err := marshaler.Marshal(anyMsg)
		if err != nil {
			logger.Error(fmt.Sprintf(" - [%s] - failed to marshal entry: %v", fileName, err))
			return "", status.Error(codes.Internal, "failed to marshal entry")
		}
		entries = append(entries, json.RawMessage(jsonBytes))
	}

	txData, err := json.Marshal(entries)
	if err != nil {
		logger.Error(" - [" + fileName + "] - failed to marshal JSON array: " + err.Error())
		return "", status.Error(codes.Internal, "failed to marshal batch JSON")
	}

	ledgerDir := "/ledger/transactions"
	batchId := fmt.Sprintf("batch_%d", time.Now().UnixNano())
	filePath := fmt.Sprintf("%s/%s.json", ledgerDir, batchId)

	if err = h.hdfs.MkdirAll(ledgerDir, 0755); err != nil {
		logger.Error(" - [" + fileName + "] - failed to create HDFS directory: " + err.Error())
		return "", status.Error(codes.Internal, "failed to create HDFS directory")
	}

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
