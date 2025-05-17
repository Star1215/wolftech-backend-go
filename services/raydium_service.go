package services

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/gagliardetto/solana-go"
)

type RaydiumService struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
	debug      bool
}

func NewRaydiumService(httpClient *http.Client, baseURL string) *RaydiumService {
	var logger *slog.Logger
	logger = slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug, // This enables debug logging
		}),
	)
	return &RaydiumService{
		httpClient: httpClient,
		baseURL:    baseURL,
		logger:     logger,
		debug:      true,
	}
}

type ComputeSwapResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

type SwapTransactionResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Data    []TxData `json:"data"`
}

type TxData struct {
	Transaction string `json:"transaction"`
}

type RaydiumComputeParams struct {
	Amount      uint64
	SlippageBps uint64
	TxVersion   string
	MintAddress string
}

type RaydiumExecuteParams struct {
	ComputeResponse   *ComputeSwapResponse
	WalletAddress     string
	PriorityFee       uint64
	TxVersion         string
	WrapUnwrapOptions WrapUnwrapOption
	InputAccount      string
}

type WrapUnwrapOption int

const (
	NoWrapUnwrap WrapUnwrapOption = iota
	WrapSol
	UnwrapSol
)

func (rs *RaydiumService) ComputeBuySwap(params RaydiumComputeParams) (*ComputeSwapResponse, error) {
	rs.logDebug("Computing buy swap",
		"outputMint", params.MintAddress,
		"amount", params.Amount,
		"slippageBps", params.SlippageBps,
		"txVersion", params.TxVersion)
	url := fmt.Sprintf("%s/compute/swap-base-in?inputMint=So11111111111111111111111111111111111111112&outputMint=%s&amount=%d&slippageBps=%d&txVersion=%s",
		rs.baseURL, params.MintAddress, params.Amount, params.SlippageBps, params.TxVersion)

	return rs.ComputeSwap(url)
}

func (rs *RaydiumService) ComputeSellSwap(params RaydiumComputeParams) (*ComputeSwapResponse, error) {
	rs.logDebug("Computing sell swap",
		"inputMint", params.MintAddress,
		"amount", params.Amount,
		"slippageBps", params.SlippageBps,
		"txVersion", params.TxVersion)
	url := fmt.Sprintf("%s/compute/swap-base-in?inputMint=%s&outputMint=So11111111111111111111111111111111111111112&amount=%d&slippageBps=%d&txVersion=%s",
		rs.baseURL, params.MintAddress, params.Amount, params.SlippageBps, params.TxVersion)

	return rs.ComputeSwap(url)
}

func (rs *RaydiumService) ComputeSwap(url string) (*ComputeSwapResponse, error) {
	rs.logDebug("Making compute swap request", "url", url)
	resp, err := rs.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	var result ComputeSwapResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		rs.logger.Error("JSON decode failed", "error", err)
		return nil, fmt.Errorf("JSON decode failed: %w", err)
	}
	rs.logDebug("Making compute swap request", "Data", result.Data)
	if !result.Success {
		rs.logger.Error("Swap computation failed")
		return &result, fmt.Errorf("swap computation failed")
	}
	rs.logDebug("Swap computation successful")
	return &result, nil
}

func (rs *RaydiumService) GetSwapTransaction(params RaydiumExecuteParams) (*solana.Transaction, error) {
	// rs.logDebug("Temp", "ExpectedAmount", params.ComputeResponse.Data.data.expectedAmount)
	rs.logDebug("Executing swap",
		"computeUnitPriceMicroLamports", params.PriorityFee,
		"swapResponse", params.ComputeResponse.Data,
		"wrapUnwrap", params.WrapUnwrapOptions,
		"wallet", params.WalletAddress)
	executeURL := fmt.Sprintf("%s/transaction/swap-base-in", rs.baseURL)
	requestBody := map[string]any{
		"computeUnitPriceMicroLamports": fmt.Sprintf("%d", params.PriorityFee),
		"swapResponse":                  params.ComputeResponse.Data,
		"txVersion":                     params.TxVersion,
		"wallet":                        params.WalletAddress,
	}

	switch params.WrapUnwrapOptions {
	case WrapSol:
		requestBody["wrapSol"] = true
		rs.logDebug("Adding SOL wrap to transaction")
	case UnwrapSol:
		requestBody["unwrapSol"] = true
		requestBody["inputAccount"] = params.InputAccount
		rs.logDebug("Adding SOL unwrap to transaction")
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		rs.logger.Error("Failed to marshal request body", "error", err)
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	fmt.Printf("Request jsonBody: %s\n", jsonBody)
	rs.logDebug("Sending execute swap request", "url", executeURL)
	resp, err := rs.httpClient.Post(executeURL, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		rs.logger.Error("Failed to execute swap", "error", err)
		return nil, fmt.Errorf("failed to execute swap: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		rs.logger.Error("Swap execution failed",
			"statusCode", resp.StatusCode,
			"response", string(body))
		return nil, fmt.Errorf("swap execution failed with status %d: %s", resp.StatusCode, string(body))
	}

	var txResponse SwapTransactionResponse
	if err := json.NewDecoder(resp.Body).Decode(&txResponse); err != nil {
		rs.logger.Error("Failed to decode transaction response", "error", err)
		return nil, fmt.Errorf("failed to decode transaction response: %w", err)
	}
	rs.logDebug("Swap execution request", "body", txResponse.Data)
	if !txResponse.Success {
		rs.logger.Error("Swap execution failed", "message", txResponse.Message)
		return nil, fmt.Errorf("swap execution failed: %s", txResponse.Message)
	}

	if len(txResponse.Data) == 0 {
		rs.logger.Error("No transaction data in response")
		return nil, fmt.Errorf("no transaction data in response")
	}

	// Decode base64 transaction
	rs.logDebug("Decoding transaction data")
	txData, err := base64.StdEncoding.DecodeString(txResponse.Data[0].Transaction)
	if err != nil {
		rs.logger.Error("Failed to decode base64 transaction", "error", err)
		return nil, fmt.Errorf("failed to decode base64 transaction: %w", err)
	}

	// Method 1: Preferred way using solana-go's built-in decoder
	tx, err := solana.TransactionFromBytes(txData)
	if err != nil {
		rs.logger.Error("Failed to deserialize transaction", "error", err)
		return nil, fmt.Errorf("failed to deserialize transaction: %w", err)
	}
	rs.logInfo("Successfully executed swap",
		"transactionID", tx.Signatures[0].String())
	return tx, nil
}

func (rs *RaydiumService) logDebug(msg string, args ...any) {
	if rs.debug {
		rs.logger.Debug(msg, args...)
	}
}

func (rs *RaydiumService) logInfo(msg string, args ...any) {
	if rs.debug {
		rs.logger.Info(msg, args...)
	}
}
