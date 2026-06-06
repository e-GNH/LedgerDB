package main

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrNonceAlreadyUsed   = errors.New("NONCE_ALREADY_USED")
	ErrFromAccountMissing = errors.New("FROM_ACCOUNT_NOT_FOUND")
	ErrToAccountMissing   = errors.New("TO_ACCOUNT_NOT_FOUND")
	ErrInsufficientFunds  = errors.New("INSUFFICIENT_FUNDS")
	ErrInvalidAmount      = errors.New("INVALID_AMOUNT")
	ErrUserAccountMissing = errors.New("USER_ACCOUNT_NOT_FOUND")      // For offline withdrawal & deposit
	ErrUserAccountExists  = errors.New("USER_ACCOUNT_ALREADY_EXISTS") // For account creation
	ErrInvalidBalance     = errors.New("INVALID_BALANCE")             // For account creation
)

func mapLuaError(err error) error {
	if err == nil {
		return nil
	}
	switch err.Error() {
	case "INVALID_AMOUNT":
		return ErrInvalidAmount
	case "NONCE_ALREADY_USED":
		return ErrNonceAlreadyUsed
	case "FROM_ACCOUNT_NOT_FOUND":
		return ErrFromAccountMissing
	case "TO_ACCOUNT_NOT_FOUND":
		return ErrToAccountMissing
	case "INSUFFICIENT_FUNDS":
		return ErrInsufficientFunds
	case "USER_ACCOUNT_NOT_FOUND":
		return ErrUserAccountMissing
	case "INVALID_BALANCE":
		return ErrInvalidBalance
	case "USER_ACCOUNT_ALREADY_EXISTS":
		return ErrUserAccountExists
	default:
		return err
	}
}
func mapGrpcError(err error) error {
	if err == nil {
		logger.Debug(" - [error.go] No errors.")
		return nil
	}
	// convert lua string to error
	err = mapLuaError(err)

	switch {
	case errors.Is(err, ErrNonceAlreadyUsed):
		logger.Debug(" - [error.go] Nonce already used.")
		return status.Error(codes.AlreadyExists, "nonce already used")
	case errors.Is(err, ErrFromAccountMissing):
		logger.Debug(" - [error.go] No From Account")
		return status.Error(codes.NotFound, "source account not found")
	case errors.Is(err, ErrToAccountMissing):
		logger.Debug(" - [error.go] No To Account")
		return status.Error(codes.NotFound, "destination account not found")
	case errors.Is(err, ErrInsufficientFunds):
		logger.Debug(" - [error.go] Insufficient funds")
		return status.Error(codes.FailedPrecondition, "insufficient funds")
	default:
		logger.Debug(" - [error.go] Internal error")
		return status.Error(codes.Internal, err.Error())
	}
}
