package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type TransactionCheckResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type GraphCheckResult struct {
	Account string `json:"account"`
	Reason  string `json:"reason"`
}
type MLCheckResult struct {
	Account string  `json:"account"`
	Score   float64 `json:"score"`
}
type BatchAccountCheckResponse struct {
	Status            string             `json:"status"`
	Reason            string             `json:"reason"`
	GraphCheckResults []GraphCheckResult `json:"level_2"`
	MLCheckResults    []MLCheckResult    `json:"level_3"`
}

type Transaction struct {
	Sender              string    `json:"sender"`
	Receiver            string    `json:"receiver"`
	Amount              float64   `json:"amount"`
	Timestamp           time.Time `json:"timestamp"`
	SenderAccountType   string    `json:"sender_account_type"`
	ReceiverAccountType string    `json:"receiver_account_type"`
}

var checkTransactionClient = &http.Client{
	Timeout: 2 * time.Second,
}
var batchAccountChecksClient = &http.Client{
	Timeout: 2 * time.Hour,
}

func checkTransaction(tx Transaction, URL string) (*TransactionCheckResponse, error) {

	payload, err := json.Marshal(tx) // tx to JSON
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction: %w", err)
	}
	// create request body
	requestBody := bytes.NewBuffer(payload)

	// HTTP client with timeout because http default has no timeout

	response, err := checkTransactionClient.Post(URL+"check_transaction", "application/json", requestBody)

	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	defer response.Body.Close()
	// check on status code
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-OK response: %s", response.Status)
	}

	var transactionResponse TransactionCheckResponse

	// decode json streams bytes not allocates memory
	// unmarshal reads all bytes into memory which is not efficient for large responses (might be necessary for batch account)
	err = json.NewDecoder(response.Body).Decode(&transactionResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode json response: %w", err)
	}

	return &transactionResponse, nil
}

func checkAccountsBatch(URL string) (*BatchAccountCheckResponse, error) {

	// HTTP client with timeout because http default has no timeout

	response, err := batchAccountChecksClient.Post(URL+"check_accounts", "application/json", nil)

	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	defer response.Body.Close()
	// check on status code
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-OK response: %s", response.Status)
	}

	var batchAccountResponse BatchAccountCheckResponse

	// decode json streams bytes not allocates memory
	// unmarshal reads all bytes into memory which is not efficient for large responses (might be necessary for batch account)
	err = json.NewDecoder(response.Body).Decode(&batchAccountResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to decode json response: %w", err)
	}

	return &batchAccountResponse, nil
}

// TODO: Test more testcases with cycles, suspicious patters and level 1 failures
func main() {
	URL := "http://localhost:8000/"
	tx := Transaction{
		Sender:              "Zeyad",
		Receiver:            "Hamza",
		Amount:              1000.0,
		Timestamp:           time.Now(),
		SenderAccountType:   "PERSON",
		ReceiverAccountType: "MERCHANT",
	}

	response, err := checkTransaction(tx, URL)
	if err != nil {
		log.Fatalf("Error checking transaction: %v", err)
	}

	log.Printf("Transaction Check Response: %+v", response)

	tx = Transaction{
		Sender:              "Roma",
		Receiver:            "Paris",
		Amount:              10000000.0,
		Timestamp:           time.Now(),
		SenderAccountType:   "MERCHANT",
		ReceiverAccountType: "MERCHANT",
	}

	response, err = checkTransaction(tx, URL)
	if err != nil {
		log.Fatalf("Error checking transaction: %v", err)
	}

	log.Printf("Transaction Check Response: %+v", response)

	batchResponse, err := checkAccountsBatch(URL)
	if err != nil {
		log.Fatalf("Error checking accounts batch: %v", err)
	}

	log.Printf("Batch Account Check Response: %+v", batchResponse)

}
