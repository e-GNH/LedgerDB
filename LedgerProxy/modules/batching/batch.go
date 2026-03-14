package batching

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"LedgerDB/services/logging"
	types "LedgerProxy/types"
	ledgerserverpb "LedgerServer/api"
	// ledgerserver "LedgerServer/api/ledgerserver"
)

var (
	batch_size = 10
	mu         sync.Mutex 
	LedgerServerClient ledgerserverpb.TransactionsServiceClient = nil

);

var logger = logging.New("batching/batch", "./")

func SaveBatchItem(item *types.SecureMessage, client ledgerserverpb.TransactionsServiceClient) error {
	mu.Lock()
	defer mu.Unlock()

	// TODO: Improvement, Implement a singleton
	if LedgerServerClient == nil {
		LedgerServerClient = client
	}

	const filename = "ledger_batches.jsonl"

	jsonData, err := json.Marshal(*item)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to marshal batch item: %v", err))
		return err
	}

	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to open batch file: %v", err))
		return err
	}

	if _, err := file.Write(append(jsonData, '\n')); err != nil {
		logger.Error(fmt.Sprintf("failed to write to batch file: %v", err))
		file.Close()
		return err
	}
	file.Close()

	count, err := getFileLineCount(filename)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to get file line count: %v", err))
		return err
	}

	
	if count >= batch_size {
		batch , err := GetBatch()
		if err != nil {
			logger.Error(fmt.Sprintf("failed to get batch: %v", err))
			return err
		}

		// TODO: Call your gRPC send to server here
		fmt.Print(batch)
		grpcBatch := &ledgerserverpb.TransactionsBatch{}

		for _, item := range batch.Items {
			grpcBatch.Transactions = append(grpcBatch.Transactions, &ledgerserverpb.Transaction{
				Status:     item.Status,
				TimeStamp:  item.Timestamp.Format(time.RFC3339), 
				FromWallet: item.From,
				ToWallet:   item.To,
				Amount:     float32(item.Amount), 
				Message:    item.Message,
				Nonce:      item.Nonce,
				Hash:       hex.EncodeToString(item.Hash), 
			})
		}

		res, err := LedgerServerClient.BatchAppend(context.Background(), grpcBatch)

		if res.Success && err != nil {
			logger.Error(fmt.Sprintf("failed to send batch: %v", err))
			return err
		}
		if err := os.Truncate(filename, 0); err != nil {
			logger.Error(fmt.Sprintf("failed to clear batch file: %v", err))
			return fmt.Errorf("failed to clear batch file: %v", err)
		}
	}

	return nil
}

func getFileLineCount(filename string) (int, error) {
	file, err := os.Open(filename)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

func GetBatch() (types.Batch, error) {

	filename := "ledger_batches.jsonl"
	var batch types.Batch

	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return batch, nil
		}
		return batch, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var item types.SecureMessage
		line := scanner.Bytes()

		if err := json.Unmarshal(line, &item); err != nil {
			continue
		}

		batch.Items = append(batch.Items, item)
	}

	if err := scanner.Err(); err != nil {
		return batch, err
	}

	return batch, nil
}
