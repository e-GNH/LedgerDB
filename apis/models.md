# Restful API endpoints

## ```/send```
This is the endpoint on which the commercial bank is going to send us, it will be a background task with status `pending` till completed.

### Method
```
post
```
### Request
```
data: Encrypt(
    Encrypt(
        request_payload,
        commercial_banks_private_key
    ),
    central_banks_public_key
),
hash: Hash(
    request_payload
)
```

### Response
```
data: Encrypt(
    Encrypt(
        receipt_payload,
        central_banks_private_key
    ),
    commercial_banks_public_key
),
hash: Hash(
    receipt_payload
)
```

## ```/receive```

### Method
```
post
```
### Request
```
data: Encrypt(
    Encrypt(
        receipt_payload,
        central_banks_private_key
    ),
    commercial_banks_public_key
),
hash: Hash(
    receipt_payload
)
```


## General Notes

1. `from` and `to` are wallet_id
2. ```SHA256``` is used for hashing
3. ```RSA``` is used for encrypting and decrypting

## Issues and Required to Discuss

1. Who will generate nonce?
2. What if any request failed in the middle? 
3. Timestamp saved in DB must be server side one, not the client side one.(how to handle this with many servers? ledger master?)
    SOLVED: the HDFS will generate the unified timestamp
4. Are mobile_phone, card_number, bank_account valid or we need our naming ```wallet``` for example?
    SOLVED: we will unify it by mobile_phone