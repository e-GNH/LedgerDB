# Restful API endpoints

## ```/send```
This is the endpoint clients send to us, it will be a background task with status `pending` till completed.

### Method
```
post
```
### Request
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

### Response
```
data: Encrypt(
    Encrypt(
        receipt_payload,
        banks_private_key
    ),
    users_public_key
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
        banks_private_key
    ),
    users_public_key
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
4. Are mobile_phone, card_number, bank_account valid or we need our naming ```wallet``` for example?