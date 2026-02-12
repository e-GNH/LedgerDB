# Database Schemas

## ``Commercial Bank DB``
It will be addressed more later when handling KYC modules.
### DB Type: None
### Schema

| | | | | | | | |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **UserID** | **Name** | **Mobile Phone** | **Public Key** | **Role** | **Wallet** | **Registraion Time** | **Geographic Location** |

### Indexes
- None

## ```World State```
This is an in memory database. (3I)
### DB Type: Redis

### Schema

| | | |
| --- | --- | --- |
| **UserID** | **Public Key** | **Amount** |

### Indexes
- `UserID` index 


## ```Users DB```
### DB Type: None
### Schema

| | | | | | | |
| --- | --- | --- | --- | --- | --- | --- |
| **UserID** | **Mobile Phone** | **Public Key** | **Role** | **Wallet** | **Registraion Time** | **Geographic Location** |

### Indexes

## ```Servers DB```
### DB Type: None


### Schema

| | | | | | |
| --- | --- | --- | --- | --- | --- |
| **Server** | **Type** | **Location** |**Registraion Time** | **Status** |
| | | | | | 

### Indexes
- None


## ```JournalInfo Store```
### DB Type: None

### Schema
| | | |
| --- | --- | --- |
| **TX_ID** | **Pointer** | **Hash** |

### Indexes
- TX_ID

## ```Transaction Store```
### DB Type: HDFS

### Schema
| | | | | | 
| --- | --- | --- | --- | --- |
| **From** | **To** | **Amount** | **Timestamp** | **Nonce** |

### Indexes
- None as it is a log

## ```BlockInfo Store```
### DB Type: None
| | | |
| --- | --- | --- |
| **Block Height** | **Previous Block Hash** | **Merkle Root** |

### Indexes
- None


## General Notes

1. `transaction_id` is str which is bad for indexes but the idea is that we retreive very little.
2. `Servers DB` should be at `Ledger Master` as it has info about the state of the ledger and should sync when a new server is added.


## Issues and Required to Discuss

1. what about mobile_phone field, should we add enum[mobile_phone, card_number, bank_account]?
2. World State needs to be discussed (check miro)
3. `World State` should enable fast access because it is frequently called, so I think we should make proxy servers based on geographic location / number.
4. `Commercial Bank DB` will be designed later when handling KYC modules.
5. When is `Users DB` going to reside, also how to sync?
6. We should discuss the data base types
7. what will host the HDFS and what types of authentication will be used?
