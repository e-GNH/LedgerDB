# LedgerDB

LedgerDB is the backend ledger system for a CBDC prototype: a gRPC pipeline that authenticates and executes transactions, commits them to durable storage, maintains live account state, and screens every transaction and account through a three-level Anti-Money Laundering (AML) engine.

**Highlights:**
- A hybrid RSA+AES encrypted, signed gRPC transaction pipeline (LedgerProxy), with WAL-backed batching and real-time cross-bank receipt streaming.
- Server-stamped batch persistence (LedgerServer) that decouples proxy-side WAL batches from durable storage, never trusting a client-supplied commit time.
- Atomic, nonce-guarded Lua scripts driving all account-state mutations in Redis (StorageKernel), with a two-phase pending/commit settlement to prevent double-spending across a synchronous AML check.
- A from-scratch, three-level AML pipeline (rule-based, graph-based, ML) including a custom time-respecting cycle-detection algorithm for round-trip laundering, plus Louvain community detection and a GARG-index implementation feeding the ML layer.

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

Each service directory has its own detailed README - this document is an overview; see those for full proto definitions, RPC-by-RPC internals, and design-history notes.

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

The entry point of the system. Exposes two gRPC services on port `50001`: `SecurityService.Execute` for banks to submit a transaction, and `ReceiptService.Subscribe` for banks to stream receipts on their wallets.

Transactions arrive hybrid-encrypted (`[RSA-OAEP-encrypted AES key][AES-256-GCM ciphertext]`) and pass a strict six-step security pipeline before anything else happens: decrypt, unmarshal, verify the sender's key against a trusted-bank store, verify an RSA-PKCS1v15 signature, recompute and compare a SHA-256 hash (tamper check), and reject anything older than 5 minutes or timestamped in the future (replay protection).

Verified transactions are appended to a local write-ahead log and immediately receipted; once the WAL reaches 10 entries, it's batched and forwarded to LedgerServer. Receipts are routed by a bank-prefix embedded in each wallet ID, so cross-bank transfers correctly notify both sides.

→ [full details](./LedgerProxy/README.md)

## 2. LedgerServer

The persistence coordinator, on port `50003`. Its only job is `BatchAppend`: map each transaction into StorageKernel's schema (stamping the server-side commit time, not the client's), call `Store`, and confirm success only if both the RPC and the storage acknowledgement succeed.

→ [full details](./LedgerServer/README.md)

## 3. StorageKernel

The storage and truth layer, on port `50058`, backed by Redis (**World State**) and HDFS (**Transaction Store**).

Every account mutation runs as an atomic Redis Lua script, gated by a nonce, eliminating read-modify-write races. Transfers settle in two phases - the sender's balance is deducted immediately, but funds land in the receiver's `pending` field, not their spendable balance, until a later `CommitTransfer` confirms durable storage. This means a transaction that fails its AML check can be cleanly rolled back before it's ever finalized: a sender can't double-spend while a decision is pending, and a receiver can't spend money from a transaction that didn't ultimately settle.

`Transfer` calls out synchronously to the AML service after the balance mutation but before returning - looking up both accounts' tiers, running Level-1 rule checks, and rolling back on rejection. AML errors fail open, so a service outage never blocks legitimate transfers.

→ [full details](./StorageKernel/README.md), [world state internals](./StorageKernel/README_worldstate.md)

## 4. AML Service

A Python/FastAPI microservice, and the compliance layer of the system. `POST /check_transaction` runs synchronously on every transfer; `POST /check_accounts` runs periodically over the whole account graph. Every threshold is configured per account type (`PERSON`, `MERCHANT`, `POS`) in `config/thresholds.json`, decoupling policy from detection logic.

**Level 1 - Rule-based.** In-memory, per-sender rolling windows catch transaction/daily limit breaches, structuring (repeated transactions just under the reporting threshold), and velocity bursts.

**Level 2 - Graph-based.** Recent transactions are modeled as a directed multigraph (accounts as nodes, timestamped transactions as edges) and screened for the classic laundering topologies - fan-in/fan-out, gather-scatter, scatter-gather, bipartite layering, and round-tripping. The round-tripping check is a custom DFS-based flow-routing algorithm built from scratch, since standard max-flow algorithms (e.g. Edmonds-Karp) can't be constrained to strictly-chronological paths. It sorts branches by timestamp, tracks per-edge remaining capacity to avoid double-counting money across overlapping paths, and never materializes full paths in memory - giving O(depth) space and a mathematically defensible lower bound on cycled money.

→ [design history for cycle detection](./aml_service/README_graph.md)

**Level 3 - Machine learning.** A LightGBM classifier, trained on account-level graph features, catches patterns the first two levels can't generalize to. Two components were implemented from scratch as part of the feature pipeline: the **GARG-index** (an interpretable smurfing risk score from second-order neighborhood topology) and **Louvain community detection** (verified for correctness and benchmarked against `networkx`, which won on speed and is used live). The model was thresholded on a high-β Fβ score (β=7), prioritizing recall since missing a launderer is far costlier than a false positive.

→ [full details](./aml_service/README.md)

---

## Running Locally

```bash
make run-all
```

Installs the AML service's virtualenv, starts Redis and HDFS via Docker, and runs all four services in parallel (logs land in `logs/`). For manual per-service startup and prerequisites, see the [Makefile](./Makefile) and each service's README.

---

## Future Work

- Scale beyond a single LedgerProxy/LedgerServer instance with a ledger-master coordinator; evaluate Redis Cluster sharding for World State throughput.
- Reimplement the AML service in Go or C++ for faster graph analysis at higher cycle-detection thresholds.
- Add neighbor-aggregated features to the ML level (e.g. average GARG score or cycled money across an account's neighborhood).

## References

- LedgerDB: A Centralized Ledger Database for Universal Audit and Verification - VLDB 2020
- GARG-AML Against Smurfing: A Scalable and Interpretable Graph-Based Framework for AML - 2026
- Fast Unfolding of Communities in Large Networks (Louvain) - 2008
- Realistic Synthetic Financial Transactions for Anti-Money Laundering Models - NeurIPS 2023 (IBM AMLWorld dataset)
- Money Laundering Schemes: 7 Common Criminal Tactics - Linkurious, 2024
