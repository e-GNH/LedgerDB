package types

import "time"

// SecureMessage represents the structure of our data AFTER decryption
type SecureMessage struct {
	Timestamp time.Time `json:"timestamp"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Amount    float64   `json:"amount"`
	Message   string    `json:"message"`
	Nonce     string    `json:"nonce"`
	Hash      []byte    `json:"hash"`
}

// type SignedMessage struct {
// 	MessageData []byte `json:"payload"`   // The actual payload (timestamp, etc.) 
// 	Signature   []byte `json:"signature"` // The sender's signature of the MessageData
// }

type UnpackedMessage struct {
	Data       []byte `json:"data"`         // The raw JSON bytes of the actual payload
	Signature  []byte `json:"signature"`    // The sender's signature of the hash
	Hash       []byte `json:"hash"`         // The hash provided by the sender
	BankPubKey []byte `json:"bank_pub_key"` // The public key of the bank
}