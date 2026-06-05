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
	pb "LedgerProxy/api"
	types "LedgerProxy/types"
	ledgerserverpb "LedgerServer/api"
)

// SendStream is the only method batching needs from a bank stream
type SendStream interface {
	Send(*pb.TransactionReceipt) error
}

// Registry is the interface batching uses to reach active bank streams
type Registry interface {
	Get(prefix string) SendStream
	Unregister(prefix string)
}

var (
	batchSize          = 10
	mu                 sync.Mutex
	LedgerServerClient ledgerserverpb.TransactionsServiceClient = nil
	reg                Registry
)

var logger = logging.New("batching/batch", "./")

func SetRegistry(r Registry) {
	reg = r
}

func SaveBatchItem(item *types.SecureMessage, client ledgerserverpb.TransactionsServiceClient) error {
	mu.Lock()
	defer mu.Unlock()

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

	// Stream receipt to bank(s) immediately after WAL write
	StreamReceipt(item)

	count, err := getFileLineCount(filename)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to get file line count: %v", err))
		return err
	}

	if count >= batchSize {
		batch, err := GetBatch()
		if err != nil {
			logger.Error(fmt.Sprintf("failed to get batch: %v", err))
			return err
		}

		grpcBatch := &ledgerserverpb.TransactionsBatch{}
		for _, tx := range batch.Items {
			grpcBatch.Transactions = append(grpcBatch.Transactions, &ledgerserverpb.Transaction{
				Status:     tx.Status,
				TimeStamp:  tx.Timestamp.Format(time.RFC3339),
				FromWallet: tx.From,
				ToWallet:   tx.To,
				Amount:     float32(tx.Amount),
				Message:    tx.Message,
				Nonce:      tx.Nonce,
				Hash:       hex.EncodeToString(tx.Hash),
			})
		}

		res, err := LedgerServerClient.BatchAppend(context.Background(), grpcBatch)
		if err != nil {
			logger.Error(fmt.Sprintf("failed to send batch: %v", err))
			return err
		}
		if !res.Success {
			logger.Error("LedgerServer returned failure on BatchAppend")
			return fmt.Errorf("batch append failed")
		}

		if err := os.Truncate(filename, 0); err != nil {
			logger.Error(fmt.Sprintf("failed to clear batch file: %v", err))
			return fmt.Errorf("failed to clear batch file: %v", err)
		}
	}

	return nil
}

func StreamReceipt(item *types.SecureMessage) {
	if reg == nil {
		logger.Info("No registry set, skipping receipt streaming")
		return
	}

	fromPrefix := walletPrefix(item.From)
	toPrefix := walletPrefix(item.To)

	receipt := &pb.TransactionReceipt{
		Hash:       hex.EncodeToString(item.Hash),
		Status:     item.Status,
		FromWallet: item.From,
		ToWallet:   item.To,
		Amount:     float32(item.Amount),
		Message:    item.Message,
		Nonce:      item.Nonce,
		TimeStamp:  item.Timestamp.Format(time.RFC3339),
	}

	sendReceipt(fromPrefix, receipt)
	if toPrefix != fromPrefix {
		sendReceipt(toPrefix, receipt)
	}
}

func sendReceipt(prefix string, receipt *pb.TransactionReceipt) {
	stream := reg.Get(prefix)
	if stream == nil {
		logger.Info(fmt.Sprintf("No active subscription for prefix %s, skipping", prefix))
		return
	}
	if err := stream.Send(receipt); err != nil {
		logger.Error(fmt.Sprintf("Failed to stream receipt to bank %s: %v", prefix, err))
		reg.Unregister(prefix)
	}
}

func walletPrefix(walletID string) string {
	if len(walletID) < 3 {
		return walletID
	}
	return walletID[:3]
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
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			continue
		}
		batch.Items = append(batch.Items, item)
	}

	return batch, scanner.Err()
}