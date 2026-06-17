package types

import "time"

type SecureMessage struct {
	Status    bool      `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Amount    int64   `json:"amount"`
	Message   string    `json:"message"`
	Nonce     string    `json:"nonce"`
	Hash      []byte    `json:"hash"`
}

type SecureOfflineWithdrawMessage struct {
	AccountId string    `json:"account_id"`
	Amount    int64   `json:"amount"`
	Nonce     string    `json:"nonce"`
	Timestamp time.Time `json:"timestamp"`
}
type SecureOfflineDepositMessage struct {
	AccountId string    `json:"account_id"`
	Amount    int64   `json:"amount"`
	Nonce     string    `json:"nonce"`
	Timestamp time.Time `json:"timestamp"`
}

type SecureCreateAccountMessage struct {
	AccountId string  `json:"account_id"`
	Balance   int64 `json:"balance"`
	Nonce     string  `json:"nonce"`
	Tier      string  `json:"tier"`
	Name      *string `json:"name,omitempty"`
}

type SecureSyncMessage struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Amount    int64   `json:"amount"`
	Nonce     string    `json:"nonce"`
	Timestamp time.Time `json:"timestamp"`
}

type LedgerSyncMessage struct {
	FromId             string  `json:"from_id"`
	ToId               string  `json:"to_id"`
	Amount             int64 `json:"amount"`
	Nonce              string  `json:"nonce"`
	OfflineTransaction bool    `json:"offine_transaction"`
}

type UnpackedMessage struct {
	Data       []byte `json:"data"`         
	Signature  []byte `json:"signature"`   
	Hash       []byte `json:"hash"`         
	BankPubKey []byte `json:"pub_key"` 
}
