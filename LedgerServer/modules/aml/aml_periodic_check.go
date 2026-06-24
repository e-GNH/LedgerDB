package aml_periodic_check

import (
	aml_checker "LedgerDB/services/aml"
	l "LedgerDB/services/logging"
	worldstate "StorageKernel/proto/worldstate"
	"context"
	"fmt"
	"time"
)

var storageKernelTimeout = int64(5)

func PeriodicAMLCheck(URL string, duration_seconds int64, logger *l.Logger, storageKernelClient worldstate.WorldStateServiceClient) {

	ticker := time.NewTicker(time.Duration(duration_seconds) * time.Second)
	defer ticker.Stop()
	timeouts := 0
	for {
		select {
		case <-ticker.C:
			response, err := aml_checker.CheckAccountsBatch(URL)
			if err != nil {
				if "request timed out" == err.Error() {
					currentTimeout := aml_checker.GetBatchClientTimeout()
					timeouts += 1
					if timeouts >= 3 {
						timeouts = 0
						if currentTimeout*2 < time.Duration(duration_seconds)*time.Second {
							aml_checker.SetBatchClientTimeout(currentTimeout * 2)
						} else {
							aml_checker.SetBatchClientTimeout(time.Duration(max(int64(1), duration_seconds-10)) * time.Second)
						}
					}
				}
				continue
			} else {
				logger.Info("Batch account checks completed successfully")
				// +v for field names + values instead of %v for values only
				logger.Info(fmt.Sprintf("Response: %+v", response))
				timeouts = 0
				if response.Status != "success" {
					logger.Error(fmt.Sprintf("Batch account checks failed %+v", response))
					continue
				}
				level_2_results := response.GraphCheckResults
				level_3_results := response.MLCheckResults

				logger.Info(fmt.Sprintf("Level 2 results: %+v", level_2_results))
				logger.Info(fmt.Sprintf("Level 3 results: %+v", level_3_results))

				for _, result := range level_2_results {
					// create context for timeout
					ctx, cancel := context.WithTimeout(context.Background(), time.Duration(storageKernelTimeout)*time.Second)
					account, reason := result.Account, result.Reason
					logger.Info(fmt.Sprintf("Flagging account %s for reason: %s", account, reason))
					_, err := storageKernelClient.ChangeAccountStatus(
						ctx,
						&worldstate.ChangeAccountStatusRequest{
							AccountId: account,
							Reason:    &reason,
							Status:    "flagged",
							// score is omitted
						},
					)
					if err != nil {
						logger.Error(fmt.Sprintf("Failed to update account status for account %s: %v", account, err))
					} else {
						logger.Info(fmt.Sprintf("Updated account status for account %s to flagged based on Graph Reason %s", account, reason))
					}
					cancel()
				}

				for _, result := range level_3_results {
					// create context for timeout
					ctx, cancel := context.WithTimeout(context.Background(), time.Duration(storageKernelTimeout)*time.Second)
					account, score := result.Account, result.Score
					logger.Info(fmt.Sprintf("Level 3 result for account %s: score %f", account, score))
					status := "flagged"
					if score > 0.6 {
						status = "banned"
					}
					floatScore := float32(score)
					_, err := storageKernelClient.ChangeAccountStatus(
						ctx,
						&worldstate.ChangeAccountStatusRequest{
							AccountId: account,
							Score:     &floatScore,
							Status:    status,
							// reason is omitted
						},
					)
					if err != nil {
						logger.Error(fmt.Sprintf("Failed to update account status for account %s: %v", account, err))
					} else {
						logger.Info(fmt.Sprintf("Updated account status for account %s to %s based on ML score %f", account, status, score))
					}
					cancel()
				}
			}

		}
	}

}
