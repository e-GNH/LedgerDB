
# Request Payload 
```
mobile_phone: str
card_number: str
bank_account: str
time_stamp: str
nonce: str
amount: float
message?: str
```

# Proxy-Master Payload
```
status: bool
transaction_ptr: str
```

# Master-Server Payload
```
status: bool
transaction_ptr: str
tx_id: str
```

# Receipt Payload
```
tx_id: str
from: str
to: str
amount: float
message?: str
```

## General Notes

1. `Request Payload` is `Journal-Data` in the paper.
2. `Receipt Payload` is `Journal-Receipt` in the paper.
3. `Proxy-Server Payload` `is Server-Journal` in the paper.
4. `status` in `Proxy-Server Payload` is because the paper says everything must enter the execute phase even if it failed.
5. `ledger Master` is assumed to be a load balancer and global sequentier that assigns tx_id to each transaction so data order is preserved.


## Issues and Required to Discuss

1. what about mobile_phone field, should we add enum[mobile_phone, card_number, bank_account]?
2. World State needs to be discussed (check miro board)
3. `transaction_ptr` should be a pointer to the transaction in the Transactions store. (will depend on the DB used (which is most likely HDFS))
4. Account at the bank level and the CB level

