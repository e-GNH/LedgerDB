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
)

func mapLuaError(err error) error {
    if err == nil {
        return nil
    }
    switch err.Error() {
    case "NONCE_ALREADY_USED":
        return ErrNonceAlreadyUsed
    case "FROM_ACCOUNT_NOT_FOUND":
        return ErrFromAccountMissing
    case "TO_ACCOUNT_NOT_FOUND":
        return ErrToAccountMissing
    case "INSUFFICIENT_FUNDS":
        return ErrInsufficientFunds
    default:
        return err
    }
}
func mapGrpcError(err error) error {
    if err == nil {
        return nil
    }
    // convert lua string to error
    err = mapLuaError(err)

    switch {
    case errors.Is(err, ErrNonceAlreadyUsed):
        return status.Error(codes.AlreadyExists, "nonce already used")
    case errors.Is(err, ErrFromAccountMissing):
        return status.Error(codes.NotFound, "source account not found")
    case errors.Is(err, ErrToAccountMissing):
        return status.Error(codes.NotFound, "destination account not found")
    case errors.Is(err, ErrInsufficientFunds):
        return status.Error(codes.FailedPrecondition, "insufficient funds")
    default:
        return status.Error(codes.Internal, err.Error())
    }
}