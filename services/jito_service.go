package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	jitorpc "github.com/jito-labs/jito-go-rpc"
)

// JitoService handles MEV bundle submissions to Jito
type JitoService struct {
	jitoClient *jitorpc.JitoJsonRpcClient
	logger     *slog.Logger
	debug      bool
}

// NewJitoService creates a new JitoService instance
func NewJitoService(jitoClient *jitorpc.JitoJsonRpcClient) *JitoService {
	var logger *slog.Logger
	logger = slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug, // This enables debug logging
		}),
	)
	return &JitoService{
		jitoClient: jitoClient,
		logger:     logger,
		debug:      true,
	}
}

// BundleResult contains the result of a bundle submission
type BundleResult struct {
	BundleID       string
	Status         string
	Error          error
	Transactions   []string
	BalanceChanges []TokenBalanceChange
}

// sendAndConfirmTransaction sends and confirms a transaction
func (js *JitoService) SubmitBundle(ctx context.Context /*bundle [][]string*/, transactions []solana.Transaction) (*BundleResult, error) {
	bundle := make([][]string, 1) // Single bundle with multiple transactions
	for _, tx := range transactions {
		// Serialize transaction
		serializedTx, err := tx.MarshalBinary()
		if err != nil {
			js.logger.Error("failed to serialize transaction:", "error", err)
			return nil, fmt.Errorf("failed to serialize transaction: %w", err)
		}
		// Encode to base64 and add to bundle
		bundle[0] = append(bundle[0], base64.StdEncoding.EncodeToString(serializedTx))
	}
	// Send the bundle
	bundleIdRaw, err := js.jitoClient.SendBundle(bundle)
	if err != nil {
		js.logger.Error("failed to send bundle:", "error", err)
		return nil, fmt.Errorf("failed to send bundle: %w", err)
	}

	var bundleId string
	if err := json.Unmarshal(bundleIdRaw, &bundleId); err != nil {
		js.logger.Error("failed to unmarshal bundle ID:", "error", err)
		return nil, fmt.Errorf("failed to unmarshal bundle ID: %w", err)
	}

	// Wait for bundle confirmation
	result, err := js.waitForBundleConfirmation(ctx, bundleId)
	if err != nil {
		js.logger.Error("bundle submission failed:", "error", err)
		return result, fmt.Errorf("bundle submission failed: %w", err)
	}

	return result, nil
	// if status.Success {
	// 	jitoSuccess = true
	// 	// Get balance changes for each transaction
	// 	for _, tx := range transactions {
	// 		sig := tx.Signatures[0].String()
	// 		txInfo, err := js.client.GetTransaction(ctx, sig, &rpc.GetTransactionOpts{
	// 			Encoding:   solana.EncodingBase64,
	// 			Commitment: rpc.CommitmentConfirmed,
	// 		})
	// 		if err != nil {
	// 			continue
	// 		}

	// 		// TODO: Implement token balance change detection
	// 		// This would require parsing the transaction metadata
	// 	}
	// } else {
	// 	// Fallback to individual transaction submission
	// 	for _, tx := range transactions {
	// 		sig, err := js.client.SendTransaction(ctx, tx)
	// 		if err != nil {
	// 			// TODO: Extract token address from failed transaction
	// 			failedTokens = append(failedTokens, "unknown-token")
	// 			continue
	// 		}

	// 		// Wait for confirmation
	// 		_, err = js.client.WaitForTransaction(ctx, sig, rpc.CommitmentConfirmed)
	// 		if err != nil {
	// 			failedTokens = append(failedTokens, "unknown-token")
	// 			continue
	// 		}

	// 		// Get balance changes
	// 		txInfo, err := js.client.GetTransaction(ctx, sig, &rpc.GetTransactionOpts{
	// 			Encoding:   solana.EncodingBase64,
	// 			Commitment: rpc.CommitmentConfirmed,
	// 		})
	// 		if err == nil {
	// 			// TODO: Parse balance changes from txInfo
	// 		}
	// 	}
	// }

	// return &BundleResult{
	// 	Success:        true,
	// 	BundleID:       bundleID,
	// 	BalanceChanges: balanceChanges,
	// 	FailedTokens:   failedTokens,
	// 	JitoSuccess:    jitoSuccess,
	// }, nil
}

// waitForBundleConfirmation polls for bundle status until finalized or timeout
func (js *JitoService) waitForBundleConfirmation(ctx context.Context, bundleId string) (*BundleResult, error) {
	result := &BundleResult{
		BundleID: bundleId,
	}

	maxAttempts := 60
	pollInterval := 2 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(pollInterval):
		}

		statusResponse, err := js.jitoClient.GetBundleStatuses([]string{bundleId})
		if err != nil {
			if js.debug {
				fmt.Printf("Attempt %d: Failed to get bundle status: %v\n", attempt, err)
			}
			continue
		}

		if len(statusResponse.Value) == 0 {
			if js.debug {
				fmt.Printf("Attempt %d: No bundle status available\n", attempt)
			}
			continue
		}

		bundleStatus := statusResponse.Value[0]
		result.Status = bundleStatus.ConfirmationStatus
		result.Transactions = bundleStatus.Transactions

		if js.debug {
			fmt.Printf("Attempt %d: Bundle status: %s\n", attempt, bundleStatus.ConfirmationStatus)
		}

		switch bundleStatus.ConfirmationStatus {
		case "processed", "confirmed":
			// Continue polling
		case "finalized":
			if bundleStatus.Err.Ok != nil {
				result.Error = fmt.Errorf("bundle execution failed: %v", bundleStatus.Err.Ok)
				return result, result.Error
			}
			return result, nil
		default:
			result.Error = fmt.Errorf("unexpected status: %s", bundleStatus.ConfirmationStatus)
			return result, result.Error
		}
	}

	result.Error = fmt.Errorf("maximum polling attempts reached")
	return result, result.Error
}

func (js *JitoService) logDebug(msg string, args ...any) {
	if js.debug {
		js.logger.Debug(msg, args...)
	}
}

func (js *JitoService) logInfo(msg string, args ...any) {
	if js.debug {
		js.logger.Info(msg, args...)
	}
}
