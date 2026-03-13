package types


type BatchItem struct {
	Status bool `json:"status"`
	FromWallet string `json:"from_wallet"`
	ToWallet string `json:"to_wallet"`
	Amount float64 `json:"amount"`
	Message string `json:"message"`
	Nonce string `json:"nonce"`
	Hash string `json:"hash"`
}

type Batch struct {
	Items []BatchItem `json:"items"`
}