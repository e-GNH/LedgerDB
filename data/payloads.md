
This is the payloads sent in the system started from the `Client` to the `Central Bank` and the way back passing by the `Commercial Banks`.
# Client-Bank Payload
```
from_mobile_phone: str
to_mobile_phone: str
time_stamp: str
nonce: str
amount: float
message?: str
hash: str
```

# Request Payload 
``` 
from_mobile_phone: str
to_mobile_phone: str
time_stamp: str
nonce: str
amount: float
message?: str
hash: str
```

# Proxy-Server Payload
```
status: bool
from_wallet: str
to_wallet: str
amount: float
message?: str
hash: str
```

# Receipt Payload
```
tx_id: uuid
from: str
to: str
amount: float
message?: str
hash: str
```

## General Notes

1. `Request Payload` is `Journal-Data` in the paper.
2. `Receipt Payload` is `Journal-Receipt` in the paper.
3. `Proxy-Server Payload` `is Server-Journal` in the paper.
4. `status` in `Proxy-Server Payload` is because the paper says everything must enter the execute phase even if it failed.
5. `ledger Master` is assumed to be a load balancer.
6. `wallet_id` will be sent from the commercial bank. (or direct Phase 2)


## Issues and Required to Discuss

1. what about mobile_phone field, should we add enum[mobile_phone, card_number, bank_account]?
2. World State needs to be discussed (check miro board)
3. `transaction_ptr` should be a pointer to the transaction in the Transactions store. (will depend on the DB used (which is most likely HDFS))
4. Account at the bank level and the CB level
5. Is there a need for wallet id to be anything rather than mobile phone?

