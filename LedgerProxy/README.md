# LedgerProxy — Full Technical Documentation

## Overview

LedgerProxy is the entry point of the LedgerDB financial system. It is a gRPC server that sits between external banks and the internal system. Every transaction from a bank passes through LedgerProxy before anything else happens.

LedgerProxy is responsible for:

- Receiving hybrid-encrypted transaction payloads from banks
- Decrypting and authenticating the payload
- Writing transactions to a local WAL (Write-Ahead Log)
- Forwarding balance changes to StorageKernel (world state)
- Batching transactions and forwarding them to LedgerServer
- Streaming receipts back to subscribed banks in real time

---

## System Position

```
Bank (gRPC Client)
      │
      │  Execute(SecureRequest)        Subscribe(SubscribeRequest)
      │  ─────────────────────►        ◄─────────────────────────
      ▼
 LedgerProxy (:50001)
      │
      ├──► StorageKernel (:50058)   [Transfer — world state / Redis]
      │
      └──► LedgerServer (:50003)   [BatchAppend — HDFS persistence]
```

---

## Ports

| Service        | Port  |
|----------------|-------|
| LedgerProxy    | 50001 |

---

## gRPC Services

LedgerProxy exposes two gRPC services on port 50001, defined in `service.proto`.

### SecurityService

```protobuf
service SecurityService {
    rpc Execute(SecureRequest) returns (SecureResponse) {}
}
```

Banks call `Execute` to submit a transaction. The payload is hybrid-encrypted.

### ReceiptService

```protobuf
service ReceiptService {
    rpc Subscribe(SubscribeRequest) returns (stream TransactionReceipt) {}
}
```

Banks call `Subscribe` once and hold the connection open. LedgerProxy streams receipts to the bank whenever a transaction involving one of its wallets is committed.

---

## Proto Messages

### SecureRequest
```protobuf
message SecureRequest {
    bytes encrypted_data = 1;
}
```
Raw bytes of the hybrid-encrypted payload. See Payload Structure below.

### SecureResponse
```protobuf
message SecureResponse {
    bool   success = 1;
    string message = 2;
}
```

### SubscribeRequest
```protobuf
message SubscribeRequest {
    string bank_prefix = 1;
}
```
The bank's 3-character wallet prefix, e.g. `"000"`, `"001"`. All wallets belonging to that bank start with this prefix.

### TransactionReceipt
```protobuf
message TransactionReceipt {
    string hash        = 1;
    bool   status      = 2;
    string from_wallet = 3;
    string to_wallet   = 4;
    int64  amount      = 5;
    string message     = 6;
    string nonce       = 7;
    string time_stamp  = 8;
}
```

---

## Payload Structure

Transactions are sent as hybrid-encrypted binary blobs. The encryption scheme is RSA-OAEP + AES-256-GCM.

### Encryption Layout (wire format)

```
[ RSA-encrypted AES key (256 bytes) ][ AES-GCM nonce + ciphertext ]
```

The AES key is encrypted with LedgerProxy's RSA public key. The ciphertext is the AES-GCM encryption of the serialized `UnpackedMessage`.

### UnpackedMessage (after decryption)

```go
type UnpackedMessage struct {
    Data       []byte  // JSON-encoded SecureMessage
    Signature  []byte  // RSA-PKCS1v15 signature of Hash
    Hash       []byte  // SHA-256 hash of Data
    BankPubKey []byte  // DER-encoded PKIX public key of the sending bank
}
```

### SecureMessage (the actual transaction payload)

```go
type SecureMessage struct {
    Status    bool      // set by proxy after verification
    Timestamp time.Time // used for timeliness check
    From      string    // sender wallet ID, e.g. "000_wallet_A"
    To        string    // receiver wallet ID, e.g. "001_wallet_B"
    Amount    int64   // transaction amount
    Message   string    // human-readable memo
    Nonce     string    // unique transaction identifier
    Hash      []byte    // SHA-256 hash of the payload
}
```

---

## Security Pipeline

Every incoming `Execute` call passes through `VerifySecurity` in `modules/security/authentication.go`. The steps run in strict order — any failure aborts the pipeline and returns false.

### Step 1 — Hybrid Decrypt

```
encryptedData[:256]  → RSA-OAEP decrypt with proxy's private key → AES key
encryptedData[256:]  → AES-256-GCM decrypt with AES key → UnpackedMessage bytes
```

### Step 2 — Unmarshal UnpackedMessage

The decrypted bytes are JSON-unmarshalled into `UnpackedMessage`.

### Step 3 — Parse and Trust-Check Bank Public Key

The `BankPubKey` field (DER PKIX bytes) is parsed into an `*rsa.PublicKey`. It is then compared against every key in the trusted keys map (loaded from `modules/security/keys/banks/`). Comparison is done by checking both `N` (modulus) and `E` (exponent).

```go
func isKeyTrusted(received *rsa.PublicKey, trusted map[string]*rsa.PublicKey) bool {
    for _, trustedKey := range trusted {
        if received.N.Cmp(trustedKey.N) == 0 && received.E == trustedKey.E {
            return true
        }
    }
    return false
}
```

If the key is not in the trusted map, the request is rejected.

### Step 4 — Verify Signature

```go
rsa.VerifyPKCS1v15(senderPubKey, crypto.SHA256, hash, signature)
```

The signature in `UnpackedMessage.Signature` must be a valid RSA-PKCS1v15 signature of `UnpackedMessage.Hash` using the bank's public key.

### Step 5 — Hash Integrity Check

```go
sha256.Sum256(UnpackedMessage.Data) == UnpackedMessage.Hash
```

Ensures the payload data has not been tampered with after signing.

### Step 6 — Timeliness Check

```go
abs(time.Now() - msg.Timestamp) <= 5 minutes
```

Rejects messages older than 5 minutes or with a future timestamp. Guards against replay attacks.

---

## Execute Flow (server.go)

```
Bank calls Execute(SecureRequest)
         │
         ▼
VerifySecurity(encryptedData, privKey, trustedKeys)
         │
    ┌────┴────┐
  fail       ok
    │         │
    │         ▼
    │   msg.Status = true
    │         │
    └────►    ▼
         msg == nil? → return SecureResponse{Success: false}
         │
         ▼
SaveBatchItem(msg) ← WAL write + immediate receipt stream
         │
         ▼
    ok == true?
    ├── no  → return SecureResponse{Success: false}
    └── yes → kernelClient.Transfer(nonce, from, to, amount)
                   │
              ┌────┴────┐
            fail        ok
              │          │
              ▼          ▼
         return false   return SecureResponse{Success: true}
```

---

## WAL and Batching (modules/batching/batch.go)

### Purpose

Transactions are written to a local JSONL file (`ledger_batches.jsonl`) as a Write-Ahead Log before being forwarded to LedgerServer. This provides local durability.

### SaveBatchItem

1. Acquires mutex lock
2. JSON-marshals the `SecureMessage`
3. Appends it as a newline-delimited JSON entry to `ledger_batches.jsonl`
4. Immediately calls `StreamReceipt` to notify subscribed banks
5. Counts lines in the file
6. If line count >= `batchSize` (10): reads all entries, builds a `TransactionsBatch`, calls `LedgerServerClient.BatchAppend`, then truncates the file

### Batch Threshold

```
batchSize = 10
```

Every 10 transactions, the full batch is forwarded to LedgerServer and the file is cleared.

### Concurrency

A package-level `sync.Mutex` (`mu`) serializes all writes. Only one goroutine writes or reads the batch file at a time.

---

## Receipt Streaming

### Bank Identification

Wallet IDs encode the owning bank in their first 3 characters:

```
"000_wallet_A"  →  prefix "000"  →  Bank 000
"001_wallet_B"  →  prefix "001"  →  Bank 001
```

### Routing Logic

After a transaction is written to the WAL, `StreamReceipt` is called:

```
fromPrefix = walletID[:3] of From
toPrefix   = walletID[:3] of To

send receipt to fromPrefix bank
if toPrefix != fromPrefix:
    send receipt to toPrefix bank
```

Same-bank transactions produce exactly one receipt. Cross-bank transactions produce exactly two receipts — one per bank.

### BankRegistry

`registry.go` maintains a thread-safe map of active streams:

```go
type BankRegistry struct {
    mu      sync.RWMutex
    streams map[string]*bankStream  // key: bank prefix
}
```

Operations:

- `Register(prefix, stream)` — called when a bank connects via `Subscribe`
- `Unregister(prefix)` — called on disconnect or failed send
- `Get(prefix)` — returns the active stream for a prefix, nil if not connected

### Subscribe Lifecycle

```
Bank calls Subscribe("000")
         │
         ▼
Registry.Register("000", stream)
         │
         ▼
Block on stream.Context().Done() or bs.done
         │
         ▼
Registry.Unregister("000")
         │
         ▼
Return nil (clean disconnect)
```

The stream blocks for the lifetime of the bank's connection. If `Send` fails, the stream is unregistered immediately.

---

## Key Management

### Proxy Private Key

Loaded at startup from:
```
modules/security/keys/my_key
```
Used to decrypt incoming AES keys (RSA-OAEP).

### Trusted Bank Public Keys

Loaded at startup from all `.pem` files in:
```
modules/security/keys/banks/
```
Each file is named after the bank (e.g. `CIB.pem`). The filename becomes the bank's identifier in the trusted keys map. Keys are parsed as either PKIX or PKCS1 format.

---

## Downstream Connections

### StorageKernel (World State)

```
grpc.Dial("localhost:50058")
kernelClient.Transfer(nonce, fromId, toId, amount)
```

Updates account balances in Redis atomically via a Lua script. Called only when security verification passes.

### LedgerServer (Persistence)

```
grpc.Dial("localhost:50003")
LedgerServerClient.BatchAppend(TransactionsBatch)
```

Called when the WAL reaches 10 transactions. Forwards the batch to LedgerServer which writes it to HDFS.

---

## File Structure

```
LedgerProxy/
├── cmd/
│   └── server/
│       ├── server.go        — main, gRPC server setup, Execute, Subscribe
│       └── registry.go      — BankRegistry, bankStream
├── modules/
│   ├── batching/
│   │   └── batch.go         — WAL write, batch forwarding, receipt streaming
│   └── security/
│       ├── authentication.go — decrypt, verify, timeliness
│       └── keys/
│           ├── my_key        — proxy RSA private key
│           ├── my_key.pem    — proxy RSA public key
│           └── banks/        — trusted bank public keys (.pem)
├── api/
│   └── service.proto         — SecurityService + ReceiptService definitions
└── types/
    └── security.go           — SecureMessage, UnpackedMessage, Batch
```

---

## Dependencies

| Package | Purpose |
|---|---|
| `google.golang.org/grpc` | gRPC server and client |
| `crypto/rsa` | RSA encryption and signature verification |
| `crypto/aes` + `crypto/cipher` | AES-256-GCM decryption |
| `crypto/sha256` | Hashing |
| `crypto/x509` | Key parsing |
| `encoding/pem` | PEM decoding |
| `LedgerDB/services/logging` | Structured logging |