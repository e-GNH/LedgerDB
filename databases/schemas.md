# Database Schemas

# ```World State```
This is an in memory database.
## DB Type: Redis Key-Value

## Schema

| | | |
| --- | --- | --- |
| **UserID** | **Public Key** | **Amount** |

## Indexes
- None


## ```Users DB``` (TO BE SOLVED IN PHASE 2)
### DB Type: MySQL
### Schema

| | | | | | | |
| --- | --- | --- | --- | --- | --- | --- |
| **UserID** | **Mobile Phone** | **Public Key** | **Role** | **Wallet** | **Registraion Time** | **Geographic Location** |

### Indexes

## ```Servers DB```
This will be a database for monitoring the state of the servers.\
It will be at the Ledger Master.
### DB Type: MySQL

### Schema

| | | | | | |
| --- | --- | --- | --- | --- | --- |
| **Server** | **Type** | **Location** |**Registraion Time** | **Status** |
| | | | | | 

### Indexes
- None

## ``Banks DB``

### DB Type: Key-Value (Redis)
### Schema

| | | 
| --- | --- | 
| **wallet_id** | **bank_id** |

### Indexes
- None


## ```Journal Store```
### DB Type: HDFS

### Schema
| | | | | | |
| --- | --- | --- | --- | --- | --- |
| **TX_ID** | **From** | **To** | **Amount** | **Timestamp** | **Nonce** |

### Indexes
- TX_ID

## ```BlockInfo Store```
### DB Type: MySQL
| | | |
| --- | --- | --- |
| **Block Height** | **Previous Block Hash** | **Merkle Root** |

### Indexes
- None


## General Notes

1. `transaction_id` is str which is bad for indexes but the idea is that we retreive very little.
2. `Servers DB` should be at `Ledger Master` as it has info about the state of the ledger and should sync when a new server is added.
3. In `World State`, the `Public Key` and `Amount` will be tuple.


## Issues and Required to Discuss

1. what about mobile_phone field, should we add enum[mobile_phone, card_number, bank_account]?
    SOLVED: `mobile_phone` only.
2. World State needs to be discussed (check miro)
    SOLVED: It won't be sharded
3. `World State` should enable fast access because it is frequently called, so I think we should make proxy servers based on geographic location / number.
    SOLVED: It won't be sharded
4. `Commercial Bank DB` will be designed later when handling KYC modules.
5. When is `Users DB` going to reside, also how to sync?
6. We should discuss the data base types
    SOLVED: `Redis`, `MySQL`, `HDFS`
7. what will host the HDFS and what types of authentication will be used?
