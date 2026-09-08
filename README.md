# LedgerDB

LedgerDB is the backend ledger system for a CBDC prototype: a gRPC pipeline that authenticates and executes transactions, commits them to durable storage, maintains live account state, and screens every transaction and account through a three-level Anti-Money Laundering (AML) engine.

The system is split into four services that form a straight-through pipeline, plus a shared AML client:

```
Bank / Wallet Client
        │  SecureRequest (RSA+AES hybrid-encrypted, signed)
        ▼
┌───────────────┐        ┌───────────────┐        ┌───────────────┐
│  LedgerProxy  │──────► │ LedgerServer  │──────► │ StorageKernel │
│   :50001      │ Batch  │   :50003      │ Store  │   :50058      │
│               │ Append │               │        │               │
└───────┬───────┘        └───────────────┘        └───────┬───────┘
        │ Transfer (world state)                          │
        └──────────────────────────────────────────────────┘
                                                            │
                                                    ┌───────▼────────┐
                                                    │  AML Service   │
                                                    │ (FastAPI, :8000)│
                                                    │ Rules→Graph→ML │
                                                    └────────────────┘
```

- **LedgerProxy** - entry point for banks; decrypts/authenticates transactions, writes a local WAL, calls StorageKernel to update balances, batches transactions to LedgerServer, and streams receipts back to banks.
- **LedgerServer** - receives batches from LedgerProxy and forwards them to StorageKernel for durable persistence.
- **StorageKernel** - the lowest layer: the **World State** (live account balances in Redis, mutated via atomic Lua scripts) and the **Transaction Store** (permanent batch log in HDFS). Also the integration point with the AML service.
- **AML Service** - a Python/FastAPI microservice that screens every transaction synchronously and re-screens the whole account graph periodically, feeding flag/ban decisions back into the World State.

---

## Repository Layout

```
LedgerProxy/       gRPC entry point - auth, WAL, batching, receipt streaming
LedgerServer/      Batches transactions and forwards them for persistence
StorageKernel/     World State (Redis) + Transaction Store (HDFS) + AML integration
aml_service/       AML microservice - rule/graph/ML levels, training pipeline
services/aml/      Go client used by StorageKernel to call the AML service
services/logging/  Shared structured logging used across the Go services
```

---

## 1. LedgerProxy

The entry point of the system. Exposes two gRPC services on port `50001` (`api/service.proto`):

- `SecurityService.Execute(SecureRequest) → SecureResponse` - banks submit a hybrid-encrypted transaction payload.
- `ReceiptService.Subscribe(SubscribeRequest) → stream TransactionReceipt` - banks hold a connection open and receive receipts for any transaction touching their wallets.

**Payload format.** Transactions arrive as `[RSA-OAEP-encrypted AES key][AES-256-GCM ciphertext]`. The decrypted payload (`UnpackedMessage`) contains the JSON-encoded transaction (`SecureMessage`), an RSA-PKCS1v15 signature, a SHA-256 hash, and the sending bank's public key.

**Security pipeline (`modules/security/authentication.go`), in order - any failure aborts:**
1. Hybrid-decrypt the payload with the proxy's RSA private key + AES-GCM.
2. Unmarshal into `UnpackedMessage`.
3. Check the sender's public key against the trusted-bank key store (`modules/security/keys/banks/*.pem`).
4. Verify the RSA-PKCS1v15 signature over the payload hash.
5. Recompute SHA-256 over the payload and compare to the claimed hash (tamper check).
6. Reject if the timestamp is more than 5 minutes old or in the future (replay protection).

**WAL and batching (`modules/batching/batch.go`).** Verified transactions are appended to a local `ledger_batches.jsonl` write-ahead log under a mutex, and a receipt is streamed immediately. Once the WAL reaches `batchSize = 10`, the accumulated entries are packaged into a `TransactionsBatch`, sent to LedgerServer via `BatchAppend`, and the WAL file is truncated.

**Receipt routing (`cmd/server/registry.go`).** Wallet IDs encode their owning bank as a 3-character prefix (e.g. `000_wallet_A`). A same-bank transfer generates one receipt; a cross-bank transfer generates two, one per bank, routed through a thread-safe `BankRegistry` of active subscriber streams.

---

## 2. LedgerServer

The persistence coordinator, on port `50003` (`api/ledgerserver.proto`). Its only job is `TransactionsService.BatchAppend(TransactionsBatch) → ServerResponse`: it maps each transaction into StorageKernel's schema (stamping the server-side commit time rather than trusting the client's), calls StorageKernel's `Store`, and returns success only if both the RPC and the storage acknowledgement succeed.

---

## 3. StorageKernel

The storage and truth layer, on port `50058`, implementing two gRPC services on one `KernelHandler` backed by Redis and HDFS.

### World State (`worldstate.proto`)

The authoritative, real-time state of every account. Backed by Redis, with every mutation implemented as an atomic Lua script to eliminate read-modify-write race conditions - Redis guarantees no other command executes while a script runs.

**Account schema** (`account:<id>` Redis hash):
- `balance` - spendable online balance
- `pending` - funds received but not yet committed (see below)
- `offline` - balance available for offline transactions
- `status` - `active`, `flagged`, or `banned`
- `tier` - account type (`PERSON`, `POS`, `MERCHANT`) used for AML thresholds
- `reason` / `score` - optional, set when an account is flagged/banned by the AML service

**RPCs:**
| RPC | Purpose |
|---|---|
| `CreateAccount` | Creates an account with an initial balance and tier; merchant accounts also register a `merchant:<name> → account:<id>` mapping. |
| `Transfer` | Debits the sender, credits the receiver's `pending` balance, resolves a merchant name to an account ID if needed, then synchronously calls the AML service before returning. |
| `CommitTransfer` | Moves a receiver's `pending` balance into `balance` once a batch is durably stored. |
| `OfflineDeposit` / `OfflineWithdraw` | Moves funds between `balance` and `offline`, each idempotent via a nonce check. |
| `ChangeAccountStatus` | Sets an account to `active` / `flagged` / `banned`, optionally recording an AML reason and score. |
| `GetAccountsTier` / `GetMerchantAccountId` | Lookups used internally to resolve tiers for AML and merchant names to account IDs. |

**Double-spend / partial-commit safety.** Every write path takes a nonce key as part of its Lua script and rejects replays. Transfers use a two-phase settle: the sender's `balance` is deducted immediately, but the amount lands in the receiver's `pending` field, not their spendable `balance`. It only becomes spendable once `CommitTransfer` runs after the batch is durably stored. If the AML check on a `Transfer` rejects the transaction, a rollback script reverses the debit/credit before it ever reaches the batch - so a sender can never double-spend while a decision is pending, and a receiver can never spend funds from a transaction that didn't ultimately settle.

**AML integration.** `Transfer` calls out synchronously to the AML service (via `services/aml`) after the balance mutation but before returning to LedgerProxy: it looks up both accounts' tiers, sends the transaction for Level-1 rule checking, and either lets the transfer stand or runs the rollback script if it's rejected. Internal AML errors fail open (the transfer is not blocked by an AML service outage).

### Transaction Store (`TransactionsStore.proto`)

`TransactionsStoreService.Store` receives a batch from LedgerServer, JSON-marshals it, and writes it to HDFS at `/ledger/transactions/batch_<unix_nano>.json`, returning a `StoreAck` with a generated batch ID.

---

## 4. AML Service

A Python/FastAPI microservice (`aml_service/`) that is the compliance layer of the system. It exposes two endpoints:

- `POST /check_transaction` - called synchronously by StorageKernel on every transfer; runs Level 1 rule checks and, if approved, adds the transaction to the in-memory transaction graph for Level 2/3 analysis.
- `POST /check_accounts` - called periodically; rebuilds the recent-transaction graph, runs Level 2 graph checks and Level 3 ML scoring across all accounts, and returns the accounts to flag or ban.

Thresholds for every check are defined per account type (`PERSON`, `MERCHANT`, `POS`) in `config/thresholds.json`, decoupling policy from detection logic.

### Level 1 - Rule-Based (`levels/level_rules.py`)

Synchronous, in-memory checks using rolling time windows per sender:

- **Transaction limit** - rejects a single transaction above the account type's ceiling.
- **Daily limit** - rejects transactions that push a sender's rolling 24-hour total over their limit.
- **Structuring / smurfing** - flags repeated transactions just under the reporting threshold (e.g. ≥90% of the limit) within 24 hours, a common tactic to dodge reporting requirements.
- **Velocity** - flags bursts of rapid-fire transactions within a short window (e.g. 5 in 1 minute).

### Level 2 - Graph-Based (`graph/builder.py`, `levels/level_graph.py`)

Models recent transactions as a directed multigraph (`NetworkX MultiDiGraph`) - accounts as nodes, timestamped transactions as edges - and screens for the classic laundering topologies from IBM's synthetic AML transactions paper:

- **Fan-out / Fan-in** - flags accounts sending to, or receiving from, an abnormally high number of *unique* counterparties (not raw transaction volume).
- **Gather-scatter** - flags accounts exceeding both fan-in and fan-out thresholds that pass most incoming money straight back out.
- **Scatter-gather** - detected via a time-aware search following strictly-increasing-timestamp paths that converge on a single account, catching indirect consolidation across many hops.
- **Time-respecting cycle detection (round-tripping)** - a custom DFS-based flow-routing algorithm (`fast_get_money_cycled`, see `README_graph.md` for the full design history) finds money that loops back to its origin through intermediaries. Standard max-flow algorithms (e.g. Edmonds-Karp) can't be constrained to strictly-chronological paths, so this was built from scratch:
  - Root out-edges and DFS branches are sorted by timestamp so a later transaction can never drain capacity a chronologically earlier one needed.
  - Sibling branches are isolated via dynamically computed available capacity, preventing the same upstream money from being double-counted across overlapping paths.
  - No paths are materialized in memory - the traversal stack itself carries the current path, giving O(depth) memory instead of the path-explosion blowup of earlier full-materialization approaches.
  - Branches are pruned the instant remaining capacity hits zero.
  - The result is a mathematically defensible **lower bound** on cycled money - if it reports $5,000 cycled, at least that much genuinely cycled.
- **Bipartite subgraph checks** - communities produced by Level 3's Louvain detection are tested with a graph-coloring approach to catch two-sided layering structures.

### Level 3 - Machine Learning (`graph/garg_index.py`, `levels/level_ml.py`, `train.py`)

Catches patterns the first two levels can't reliably generalize to (e.g. stacked or randomized laundering), using a LightGBM classifier scored on account-level features, refreshed periodically alongside Level 2.

- **GARG-index** (implemented from scratch, undirected variant) - an interpretable graph-based smurfing risk score computed from each account's second-order neighborhood adjacency structure.
- **Louvain community detection** (implemented from scratch, weighted and unweighted) - used both as a preprocessing step for the GARG-index and to feed the Level 2 bipartite check; verified for correctness and benchmarked against `networkx`'s implementation (networkx won on speed and is used in the live path).
- **Graph topology features** - total cycled money (from Level 2), input/output volume and ratios, unique senders/receivers, transaction counts and per-counterparty averages.
- **Model selection** - Random Forest and LightGBM were both trained and evaluated; LightGBM won and was thresholded by optimizing a high-β Fβ score (β=7), since in AML missing a launderer is far costlier than a false positive.

**Results**, trained/evaluated on the IBM AMLWorld synthetic dataset (HI-Small: 5M+ transactions, 500k+ accounts):

| Dataset | AUC ROC (ours) | AUC ROC (GARG paper) | AUC PR (ours) | AUC PR (GARG paper) |
|---|---|---|---|---|
| AMLWorld HI-Small (validation) | 0.9491 | 0.612 | 0.1645 | 0.02023 |
| AMLWorld LI-Small (generalization check) | 0.9356 | 0.551 | 0.0476 | 0.00255 |

Tests for the graph builder, GARG index, and each level live in `aml_service/tests/`.

---

## Running Locally

**Prerequisites:** Go, Python 3, Docker (for Redis and HDFS).

```bash
make run-all
```

This installs the AML service's virtualenv, starts Redis and HDFS via Docker, and runs all four services in parallel (logs land in `logs/`).

Or run each component manually, in separate terminals:

```bash
# Terminal 1 - LedgerServer
cd LedgerServer && go mod tidy && go run .

# Terminal 2 - LedgerProxy
cd LedgerProxy && go mod tidy && go run ./cmd/server/

# Terminal 3 - StorageKernel (requires Redis + HDFS running)
cd StorageKernel && go mod tidy && go run .

# Terminal 4 - AML Service
cd aml_service && pip install -r requirements.txt && uvicorn main:app --reload
```

---

## Future Work

- Scale beyond a single LedgerProxy/LedgerServer instance with a ledger-master coordinator to handle multiple proxies/servers and server failures; evaluate Redis Cluster sharding for World State throughput.
- Reimplement the AML service in Go or C++ for faster graph analysis, allowing higher cycle-detection thresholds without CPU cost.
- Add neighbor-aggregated features to the ML level (e.g. average GARG score or cycled money across an account's neighborhood).

## References

- LedgerDB: A Centralized Ledger Database for Universal Audit and Verification - VLDB 2020
- GARG-AML Against Smurfing: A Scalable and Interpretable Graph-Based Framework for AML - 2026
- Fast Unfolding of Communities in Large Networks (Louvain) - 2008
- Realistic Synthetic Financial Transactions for Anti-Money Laundering Models - NeurIPS 2023 (IBM AMLWorld dataset)
- Money Laundering Schemes: 7 Common Criminal Tactics - Linkurious, 2024
