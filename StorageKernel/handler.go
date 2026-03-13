package main

import (
	pb "StorageKernel/proto/worldstate"
	"context"

	"github.com/redis/go-redis/v9"
)

type KernelHandler struct {
	pb.UnimplementedWorldStateServiceServer
	rdb *redis.Client
}

func (h *KernelHandler) CreateAccount(ctx context.Context, id string, balance int64) error { // TODO FINISH AFTER ONBOARDING
	return h.rdb.HSet(ctx, "account:"+id,
		"balance", balance,
		"pending", 0,
	).Err()
}
func (h *KernelHandler) Transfer(ctx context.Context, req *pb.TransferRequest) (*pb.TransferResponse, error) {
	keys := []string{
		"nonce:" + req.Nonce,
		"account:" + req.FromId,
		"account:" + req.ToId,
	}
	err := transferScript.Run(ctx, h.rdb, keys, req.Amount).Err()
	if err != nil {
		return nil, mapGrpcError(err)
	}
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
