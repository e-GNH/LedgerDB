package batching

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sync"

	"encoding/hex"

	"LedgerDB/services/logging"
	pb "LedgerProxy/api"
	types "LedgerProxy/types"
	ledgerserverpb "LedgerServer/api"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type SendStream interface {
	Send(*pb.TransactionReceipt) error
}

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

const filename = "ledger_batches.jsonl"

func SetRegistry(r Registry) {
	reg = r
}

func SaveBatchItem[T proto.Message](item T, client ledgerserverpb.TransactionsServiceClient) error {

	if LedgerServerClient == nil {
		LedgerServerClient = client
	}

	jsonData, err := protojson.Marshal(item)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to marshal batch item: %v", err))
		return err
	}

	mu.Lock()
	defer mu.Unlock()
	
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

	return nil
}

func FlushBatch() error {

	lines, err := readFileLines(filename)
	if err != nil {
		return err
	}
	if len(lines) < batchSize {
		return nil
	}
	batch := &ledgerserverpb.BatchToAppend{}

	for _, line := range lines {
		var anyMsg anypb.Any
		if err := protojson.Unmarshal([]byte(line), &anyMsg); err != nil {
			logger.Error(fmt.Sprintf("failed to unmarshal WAL line, skipping: %v", err))
			continue
		}
		batch.Logs = append(batch.Logs, &anyMsg)
	}

	if len(batch.Logs) == 0 {
		return nil
	}

	res, err := LedgerServerClient.BatchAppend(context.Background(), batch)
	if err != nil {
		return fmt.Errorf("failed to send batch: %w", err)
	}
	if !res.Success {
		return fmt.Errorf("batch append failed")
	}

	return os.Truncate(filename, 0)
}

func readFileLines(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func StreamReceipt(item *types.SecureMessage) {
	if reg == nil {
		logger.Info("No registry set, skipping receipt streaming")
		return
	}

	fromPrefix := walletPrefix(item.From)
	toPrefix := walletPrefix(item.To)

	isMerchant := false
	if item.MerchantName != nil {
		isMerchant = true
	}

	receipt := &pb.TransactionReceipt{
		Hash:       hex.EncodeToString(item.Hash),
		Status:     item.Status,
		FromWallet: item.From,
		ToWallet:   item.To,
		Amount:     int64(item.Amount),
		Message:    item.Message,
		Nonce:      item.Nonce,
		TimeStamp:  item.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		IsMerchant: isMerchant,
	}

	go sendReceipt(fromPrefix, receipt)
	if toPrefix != fromPrefix {
		go sendReceipt(toPrefix, receipt)
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
