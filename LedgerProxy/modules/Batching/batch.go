package batching

import (
	"encoding/json"
	"os"
	"bufio"

	types "LedgerProxy/types"
)


var batch_size = 10 // To be changed Batch Size
var current_count = 0

func SaveBatchItem(item types.BatchItem) error {
	jsonData, err := json.Marshal(item)
	if err != nil {
		return err 
	}

	file, err := os.OpenFile("ledger_batches.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(append(jsonData, '\n'))
	if err != nil {
		return err
	}
	current_count++
	if current_count >= batch_size {
        _, err := GetBatch()
        if err != nil {
            return err
        }
        // sendToHDFS(batch)
    }

	return nil
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
		var item types.BatchItem
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