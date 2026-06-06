# LedgerServer — Full Technical Documentation

## Overview

LedgerServer is the persistence layer coordinator in the LedgerDB financial system. It sits between LedgerProxy and StorageKernel. It receives batches of transactions from LedgerProxy, forwards them to StorageKernel for permanent storage in HDFS, and (via LedgerProxy) triggers receipt streaming back to banks.

LedgerServer is responsible for:

- Receiving transaction batches from LedgerProxy via `BatchAppend`
- Forwarding batches to StorageKernel for HDFS persistence
- Confirming storage success back to LedgerProxy

---

## System Position

```
LedgerProxy (:50051)
      │
      │  BatchAppend(TransactionsBatch)
      ▼
 LedgerServer (:50053)
      │
      │  Store(TransactionBatchRequest)
      ▼
 StorageKernel (:50058)   [HDFS write]
```

---

## Ports

| Service        | Port  |
|----------------|-------|
| LedgerServer   | 50053 |
| StorageKernel  | 50058 |

---

## gRPC Services

LedgerServer exposes two gRPC services on port 50053, defined in `ledgerserver.proto`.

### TransactionsService

```protobuf
service TransactionsService {
    rpc BatchAppend(TransactionsBatch) returns (ServerResponse) {}
}
```

Called by LedgerProxy when the WAL reaches the batch threshold (10 transactions). Forwards the batch to StorageKernel.

### ReceiptService

```protobuf
service ReceiptService {
    rpc Subscribe(SubscribeRequest) returns (stream TransactionReceipt) {}
}
```

**Note:** This service is defined in the proto but receipt streaming was moved to LedgerProxy. This service is registered but not the active receipt path in the current implementation.

---

## Proto Messages

### TransactionsBatch
```protobuf
message TransactionsBatch {
    repeated Transaction transactions = 1;
}
```

### Transaction
```protobuf
message Transaction {
    bool   status      = 1;
    string time_stamp  = 2;
    string from_wallet = 3;
    string to_wallet   = 4;
    float  amount      = 5;
    string message     = 6;
    string nonce       = 7;
    string hash        = 8;
}
```

### ServerResponse
```protobuf
message ServerResponse {
    bool success = 1;
}
```

### SubscribeRequest
```protobuf
message SubscribeRequest {
    string bank_prefix = 1;
}
```

### TransactionReceipt
```protobuf
message TransactionReceipt {
    string hash        = 1;
    bool   status      = 2;
    string from_wallet = 3;
    string to_wallet   = 4;
    float  amount      = 5;
    string message     = 6;
    string nonce       = 7;
    string time_stamp  = 8;
}
```

---

## BatchAppend Flow

```
LedgerProxy calls BatchAppend(TransactionsBatch)
         │
         ▼
len(transactions) == 0? → return ServerResponse{Success: false}
         │
         ▼
Build ts_store.TransactionBatchRequest
(map each pb.Transaction → ts_store.Transaction, stamp time.Now())
         │
         ▼
StorageKernel.Store(TransactionBatchRequest)
         │
    ┌────┴────┐
  error      ok
    │         │
    ▼         ▼
return     ack.Success == false? → return ServerResponse{Success: false}
false               │
                    ▼
         return ServerResponse{Success: true}
```

---

## Transaction Mapping

When building the store request, LedgerServer maps from its own proto types to StorageKernel's proto types:

| LedgerServer field | StorageKernel field | Note |
|---|---|---|
| `tx.Status` | `Status` | passed through |
| `tx.FromWallet` | `FromWallet` | passed through |
| `tx.ToWallet` | `ToWallet` | passed through |
| `tx.Amount` | `Amount` | passed through |
| `tx.Message` | `Message` | passed through |
| `tx.Nonce` | `Nonce` | passed through |
| `tx.Hash` | `Hash` | passed through |
| `time.Now()` | `TimeStamp` | **server-side timestamp, client timestamp ignored** |

---

## Downstream: StorageKernel Store

LedgerServer calls StorageKernel's `TransactionsStoreService`:

```protobuf
// StorageKernel proto
rpc Store(TransactionBatchRequest) returns (StoreAck);

message StoreAck {
    bool   success  = 1;
    string batch_id = 2;
}
```

StorageKernel writes the batch to HDFS under `/ledger/transactions/batch_<timestamp>.json` and returns a `StoreAck` with a `batch_id`. LedgerServer checks both the gRPC error and `ack.Success` before returning to LedgerProxy.

---

## Server Setup (ledger_server.go)

```
startup
  │
  ▼
net.Listen(":50053")
  │
  ▼
grpc.Dial("localhost:50058")   ← connect to StorageKernel
  │
  ▼
LedgerServer{
    TransactionsStoreClient: ts_store client,
    Registry: registry,
}
  │
  ▼
RegisterTransactionsServiceServer
  │
  ▼
grpcServer.Serve()
```

---

## File Structure

```
LedgerServer/
├── main/
│   └── ledger_server.go     — main, gRPC server setup
├── modules/
│   └── transactions/
│       └── transactions.go  — BatchAppend handler, Subscribe handler
├── api/
│   └── ledgerserver.proto   — TransactionsService + ReceiptService definitions
```

---

## Dependencies

| Package | Purpose |
|---|---|
| `google.golang.org/grpc` | gRPC server and client |
| `StorageKernel/proto/TransactionsStore` | Store RPC client |
| `LedgerDB/services/logging` | Structured logging |

---

## Interaction Summary

| Caller | Method | Direction |
|---|---|---|
| LedgerProxy | `BatchAppend` | → LedgerServer |
| LedgerServer | `Store` | → StorageKernel |
| Bank | `Subscribe` | → LedgerServer (defined, active path is LedgerProxy) |