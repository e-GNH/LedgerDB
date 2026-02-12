# Main LedgerDB Operators

**These will be called by the Ledger Proxy after authentication and authorization.**

## ```append_tx(tx_data)```

Appends a new transaction to the ledger.
**level**: User level function to set a transaction as a trusted anchor.\
**Module**: UNKNOWN\
**Server**: Ledger Proxy\
**Type**: RPC call to Storage Kernel

### Inputs
```
tx_data: dict(
    sender: str, # wallet_id
    receiver: str, # wallet_id
    amount: float,
    nonce: str,
    timestamp: str,
    hash: str
)
```

### Outputs
```
success: bool
```

## ```retreive_tx(tx_id)```

Retreives a transaction from the ledger.
**level**: User level function to set a transaction as a trusted anchor.\
**Module**: UNKNOWN\
**Server**: Ledger Server\
**Type**: RPC call to Storage Kernel

### Inputs
```
tx_id: str
```

### Outputs
```
tx_data: dict(
    sender: str, # wallet_id
    receiver: str, # wallet_id
    amount: float,
    nonce: str,
    timestamp: str,
    hash: str
)
```

## ```set_trusted_anchor(tx_id)```

**level**: System level function to set a transaction as a trusted anchor.\
**Module**: UNKNOWN\
**Server**: Ledger Server\
**Type**: RPC call to Storage Kernel

### Inputs
```
tx_id: str
```

### Outputs
```
tx_data: dict(
    sender: str, # wallet_id
    receiver: str, # wallet_id
    amount: float,
    nonce: str,
    timestamp: str,
    hash: str
)
```

## ```get_trusted_anchor(tx_id)```

**level**: System level function to set a transaction as a trusted anchor.\
**Module**: UNKNOWN\
**Server**: UNKNOWN\
**Type**: RPC call to Storage Kernel

### Inputs
```
tx_id: str
```

### Outputs

## ```verify_tx(data)```

**level**: System level function to verify if a trasaction is okay to be done \
**Module**: Authentication Module\
**Server**: Ledger Proxy\
**Type**: Pure Function

### Inputs
```
data: Encrypt(
    Encrypt(
        request_payload,
        users_private_key
    ),
    banks_public_key
),
hash: Hash(
    request_payload
)
```

### Outputs
```
safe: bool
```

## ```build_index(batch)```

**level**: System level function to build the block of the batch of transactions \
**Module**: Indexing Module\
**Server**: Ledger Server\
**Type**: Pure Function

### Inputs
```
tx_ids: list[str],
tx_hashes: list[str]    
```

### Outputs
```
block: Block(
    block_height: int,
    previous_block_hash: str,
    merkle_root: str, # new_global_root_hash after bAMT update
)
```

## ```update_bAMT(data)```

**level**: System level function to verify if a trasaction is okay to be done\ 
**Module**: Indexing Module\
**Server**: Ledger Server\
**Type**: Pure Function

### Inputs
```
tx_ids: list[str],
tx_hashes: list[str] 
```

### Outputs
```
root_hash: str
```

## General Notes

1. `transaction_id` is equivilant to ```jsn`` in the ledger paper.

## Issues and Required to Discuss

1. RPC? also we append to an open file, we don't open one for each transaction.