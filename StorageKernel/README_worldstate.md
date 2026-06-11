# World State Manager (Redis)

This module handles the core "World State" of the ledger system. It is responsible for maintaining account balances, processing transfers, and managing offline/online funds in real-time. 

To guarantee high throughput, strict atomicity, and idempotency, all state mutations are pushed directly into Redis using embedded **Lua scripts**. The module exposes these operations to the rest of the network via a fast gRPC interface.

---

## 🏛️ Architecture & Data Model

The world state is strictly maintained in Redis. The schema is designed for speed and atomicity:

### Redis Keys & Schema
1. **Accounts (`account:{id}`):** Stored as Redis Hashes (`HSET`). Every account contains three fields:
   * `balance`: The current cleared, online balance available to the user.
   * `pending`: Funds transferred to the account that are waiting for commit
   * `offline`: Funds locked in the user's offline/hardware wallet state.
2. **Nonces (`nonce:{nonce}`):** Stored as simple key-value pairs. Used to enforce strict transaction idempotency. If a nonce exists, the transaction is rejected as a duplicate.
3. **Global Sequence (`global:sequence`):** An atomic integer counter used to order and sequence every successful state mutation across the entire network.

---

## 🔌 gRPC API Reference

The component implements the `WorldStateServiceServer`. Below are the core endpoints, their expected inputs, outputs, and behaviors.

### 1. `CreateAccount`
Initializes a new account in the world state.
* **Input (`CreateAccountRequest`):** 
  * `Nonce` (string): Idempotency key.
  * `AccountId` (string): The unique identifier for the new account.
  * `Balance` (int): The initial starting balance.
* **Output (`CreateAccountResponse`):** * `Ok` (bool), `Message` (string containing the transaction sequence).
* **Behavior:** Fails if the account already exists or the balance is negative.

### 2. `Transfer`
Moves funds between two accounts.
* **Input (`TransferRequest`):** * `Nonce` (string): Idempotency key.
  * `FromId` (string): Source account.
  * `ToId` (string): Destination account.
  * `Amount` (int): The transaction amount.
  * `OfflineTransaction` (bool): Flag indicating if this is an offline wallet transfer.
* **Output (`TransferResponse`):** * `Ok` (bool), `Message` (string containing the transaction sequence).
* **Behavior:** Fails if funds are insufficient or accounts do not exist.

### 3. `OfflineDeposit`
Locks funds from the main online balance into the offline balance for a single account.
* **Input (`OfflineDepositRequest`):** `Nonce` (string), `AccountId` (string), `Amount` (int).
* **Output (`OfflineDepositResponse`):** `Ok` (bool), `Message` (string).

### 4. `OfflineWithdraw`
Unlocks funds from the offline balance and returns them to the main online balance.
* **Input (`OfflineWithdrawRequest`):** `Nonce` (string), `AccountId` (string), `Amount` (int).
* **Output (`OfflineWithdrawResponse`):** `Ok` (bool), `Message` (string).

---

## 📜 Redis Lua Scripts (Atomic Operations)

Because checking balances and deducting funds requires multiple steps (Read-Modify-Write), doing this in Go code would create race conditions. Instead, we use Redis Lua scripts. Redis runs Lua scripts **atomically**, meaning no other command can run while the script is executing, guaranteeing perfect consistency.

### `createAccountScript`
* **How it works:** 1. Checks if the `balance` is valid (>= 0).
  2. Checks if the `nonce` already exists (Idempotency check).
  3. Checks if the `account` already exists.
  4. If all checks pass, it creates a Hash with `balance=ARGV[1]`, `pending=0`, and `offline=0`.
  5. Records the nonce and increments the `global:sequence`.

### `transferScript`
* **How it works:** 1. Validates the amount (> 0), nonce, and ensures both source and destination accounts exist.
  2. **Dynamic Routing:** It checks the `offline` boolean flag. 
     * If `true`, it targets the `offline` field of the sender and receiver.
     * If `false`, it targets the `balance` field of the sender and the `pending` field of the receiver.
  3. Verifies the sender has sufficient funds.
  4. Uses `HINCRBY` to atomically decrement the sender and increment the receiver.
  5. Records the nonce and returns the new `global:sequence`.

### `offlineDepositScript` & `offlineWithdrawScript`
* **How they work:** * These scripts act on a single account.
  * **Deposit:** Verifies the user has enough main `balance`. It then subtracts from `balance` and adds to `offline`.
  * **Withdraw:** Verifies the user has enough `offline` funds. It subtracts from `offline` and adds back to `balance`.
  * Both scripts enforce nonce idempotency and generate a sequence number.

---

## 🛑 Error Handling

The module provides robust error mapping between the Lua execution environment and the gRPC client network. 

When a Lua script encounters an invalid state, it returns a string error (e.g., `"INSUFFICIENT_FUNDS"`). The `errors.go` layer maps these strings into strongly-typed Go errors, and then translates them into standard gRPC HTTP status codes for the client:

| Lua Error | gRPC Status Code | Description |
| :--- | :--- | :--- |
| `NONCE_ALREADY_USED` | `AlreadyExists` (409) | Prevented a replay attack / duplicate request. |
| `FROM_ACCOUNT_NOT_FOUND` | `NotFound` (404) | The source account does not exist. |
| `TO_ACCOUNT_NOT_FOUND` | `NotFound` (404) | The destination account does not exist. |
| `INSUFFICIENT_FUNDS` | `FailedPrecondition` (412) | Sender does not have enough funds for the operation. |
| `INVALID_AMOUNT` | `Internal` (500) | Amount requested was <= 0. |
## TODO
make nonces expire for normal transactions but offline transactions won't expire except after both parties sync