# StorageKernel — Full Technical Documentation

## Overview

StorageKernel is the lowest layer of the LedgerDB financial system. It owns two distinct responsibilities: maintaining the live world state of account balances in Redis, and permanently storing committed transaction batches in HDFS.

StorageKernel is responsible for:

- Executing atomic balance transfers in Redis via a Lua script
- Preventing double-spends using nonce tracking in Redis
- Writing committed transaction batches to HDFS as JSON files
- Generating storage acknowledgements back to LedgerServer

---

## System Position

```
LedgerProxy (:50001)
      │
      │  Transfer(TransferRequest)        [world state]
      ▼
StorageKernel (:50058)
      │
      ├── Redis        [live account balances, nonce registry]
      └── HDFS         [permanent transaction log]

LedgerServer (:50003)
      │
      │  Store(TransactionBatchRequest)   [persistence]
      ▼
StorageKernel (:50058)
```

---

## Ports

| Service       | Port  |
|---------------|-------|
| StorageKernel | 50058 |
| Redis         | 6379  |
| HDFS NameNode | 9000  |

---

## gRPC Services

StorageKernel exposes two gRPC services on port 50058, both implemented on the same `KernelHandler` struct.

### WorldStateService

```protobuf
service WorldStateService {
    rpc Transfer(TransferRequest) returns (TransferResponse);
}
```

Called by LedgerProxy to atomically debit the sender and credit the receiver in Redis.

### TransactionsStoreService

```protobuf
service TransactionsStoreService {
    rpc Store(TransactionBatchRequest) returns (StoreAck);
}
```

Called by LedgerServer to permanently write a batch of transactions to HDFS.

---

## Proto Messages

### TransferRequest
```protobuf
message TransferRequest {
    string nonce   = 1;
    string from_id = 2;
    string to_id   = 3;
    int64  amount  = 4;
}
```

### TransferResponse
```protobuf
message TransferResponse {
    bool   ok      = 1;
    string message = 2;
}
```

### Transaction (TransactionsStore)
```protobuf
message Transaction {
    bool   status      = 1;
    string from_wallet = 2;
    string to_wallet   = 3;
    float  amount      = 4;
    string message     = 5;
    string nonce       = 6;
    string hash        = 7;
    string time_stamp  = 8;
}
```

### TransactionBatchRequest
```protobuf
message TransactionBatchRequest {
    repeated Transaction transactions = 1;
}
```

### StoreAck
```protobuf
message StoreAck {
    bool   success  = 1;
    string batch_id = 2;
}
```

---

## KernelHandler Struct

```go
type KernelHandler struct {
    pb.UnimplementedWorldStateServiceServer
    ts.UnimplementedTransactionsStoreServiceServer
    rdb  *redis.Client
    hdfs *hdfs.Client
}
```

Both services are implemented on the same struct, sharing the Redis and HDFS clients.

---

## World State: Transfer

### Flow

```
LedgerProxy calls Transfer(nonce, fromId, toId, amount)
         │
         ▼
Build Redis keys:
  "nonce:<nonce>"
  "account:<fromId>"
  "account:<toId>"
         │
         ▼
Run Lua transferScript atomically on Redis
         │
    ┌────┴────┐
  error      "OK"
    │         │
    ▼         ▼
mapGrpcError  return TransferResponse{Ok: true}
```

### Redis Key Schema

| Key | Type | Fields |
|---|---|---|
| `account:<id>` | Hash | `balance` (int64), `pending` (int64) |
| `nonce:<nonce>` | String | `1` (presence flag) |

### Lua Script (transferScript)

The entire transfer executes as a single atomic Lua script in Redis, preventing any race conditions:

```lua
-- KEYS[1] = nonce key
-- KEYS[2] = from account key
-- KEYS[3] = to account key
-- ARGV[1] = amount

local amount = tonumber(ARGV[1])

if amount <= 0 then
    return {err="INVALID_AMOUNT"}
end
if redis.call("EXISTS", KEYS[1]) == 1 then
    return {err="NONCE_ALREADY_USED"}
end
if redis.call("EXISTS", KEYS[2]) == 0 then
    return {err="FROM_ACCOUNT_NOT_FOUND"}
end
if redis.call("EXISTS", KEYS[3]) == 0 then
    return {err="TO_ACCOUNT_NOT_FOUND"}
end

local balance = tonumber(redis.call("HGET", KEYS[2], "balance") or "0")

if balance < amount then
    return {err="INSUFFICIENT_FUNDS"}
end

redis.call("HINCRBY", KEYS[2], "balance", -amount)
redis.call("HINCRBY", KEYS[3], "pending",  amount)
redis.call("SET",     KEYS[1], 1)

return "OK"
```

### Lua Script Checks (in order)

1. Amount must be > 0
2. Nonce must not already exist (replay / double-spend prevention)
3. Sender account must exist
4. Receiver account must exist
5. Sender balance must be >= amount

If all checks pass: sender `balance` decremented, receiver `pending` incremented, nonce marked as used.

### Error Mapping

Lua returns error strings which are mapped to typed Go errors, then to gRPC status codes:

| Lua error string | Go error | gRPC code |
|---|---|---|
| `INVALID_AMOUNT` | `ErrInvalidAmount` | `InvalidArgument` |
| `NONCE_ALREADY_USED` | `ErrNonceAlreadyUsed` | `AlreadyExists` |
| `FROM_ACCOUNT_NOT_FOUND` | `ErrFromAccountMissing` | `NotFound` |
| `TO_ACCOUNT_NOT_FOUND` | `ErrToAccountMissing` | `NotFound` |
| `INSUFFICIENT_FUNDS` | `ErrInsufficientFunds` | `FailedPrecondition` |
| anything else | raw error | `Internal` |

---

## Persistence: Store

### Flow

```
LedgerServer calls Store(TransactionBatchRequest)
         │
         ▼
writeBatchToHDFS(transactions)
         │
         ├── json.Marshal(transactions)
         ├── hdfs.MkdirAll("/ledger/transactions")
         ├── batchId = "batch_<UnixNano>"
         ├── hdfs.Create("/ledger/transactions/batch_<UnixNano>.json")
         └── writer.Write(txData)
         │
    ┌────┴────┐
  error      ok
    │         │
    ▼         ▼
return nil  generateReceipts(transactions)
                    │
                    ▼
         return StoreAck{Success: true, BatchId: batchId}
```

### HDFS File Layout

```
/ledger/
└── transactions/
    ├── batch_1718000000000000000.json
    ├── batch_1718000000000000001.json
    └── ...
```

Each file contains a JSON array of all transactions in that batch. The filename is `batch_` followed by `time.Now().UnixNano()` to ensure uniqueness.

### generateReceipts

After a successful HDFS write, receipts are generated for each transaction:

```go
TransactionReceipt{
    TransactionId: tx.Hash,
    Status:        tx.Status,
    FromWallet:    tx.FromWallet,
    ToWallet:      tx.ToWallet,
    Amount:        tx.Amount,
    Message:       tx.Message,
    // TimeStamp not populated in production path
}
```

These receipts are returned in `StoreAck` back to LedgerServer.

---

## Account Management

Accounts are created via `CreateAccount` (called during onboarding, not via gRPC):

```go
func (h *KernelHandler) CreateAccount(ctx context.Context, id string, balance int64) error {
    return h.rdb.HSet(ctx, "account:"+id,
        "balance", balance,
        "pending", 0,
    ).Err()
}
```

---

## Server Setup (main.go)

```
startup
  │
  ▼
newRedisClient()        ← connect to localhost:6379, ping check
  │
  ▼
newHDFSClient()         ← connect to localhost:9000
  │
  ▼
KernelHandler{rdb, hdfs}
  │
  ▼
CreateAccount("A", 100)   ← test accounts, to be removed
CreateAccount("B", 100)
  │
  ▼
net.Listen(":50058")
  │
  ▼
RegisterWorldStateServiceServer
RegisterTransactionsStoreServiceServer
  │
  ▼
grpcServer.Serve()
```

---

## Client Initialization

### Redis

```go
redis.NewClient(&redis.Options{
    Addr:     "localhost:6379",
    Password: "",
    DB:       0,
    Protocol: 2,
})
```

A `PING` is issued immediately after connection. If it fails, `nil` is returned.

### HDFS

```go
hdfs.New("localhost:9000")
```

Connects to the HDFS NameNode. If connection fails, an error is returned and startup aborts.

---

## File Structure

```
StorageKernel/
├── main.go          — main, gRPC server setup, test helpers
├── handler.go       — Transfer, Store, writeBatchToHDFS, generateReceipts
├── scripts.go       — Lua transferScript definition
├── errors.go        — error types, mapLuaError, mapGrpcError
├── clients.go       — newRedisClient, newHDFSClient
└── proto/
    ├── worldstate/
    │   └── worldstate.proto       — WorldStateService
    └── TransactionsStore/
        └── TransactionsStore.proto — TransactionsStoreService
```

---

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/redis/go-redis/v9` | Redis client |
| `github.com/colinmarc/hdfs/v2` | HDFS client |
| `google.golang.org/grpc` | gRPC server |
| `LedgerDB/services/logging` | Structured logging |

---

## Interaction Summary

| Caller | Method | Transport | Purpose |
|---|---|---|---|
| LedgerProxy | `Transfer` | gRPC | Atomic balance update in Redis |
| LedgerServer | `Store` | gRPC | Permanent batch write to HDFS |