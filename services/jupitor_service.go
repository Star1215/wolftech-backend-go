package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/ilkamo/jupiter-go/jupiter"
)

type JupiterService struct {
	jupClient *jupiter.ClientWithResponses
	logger    *slog.Logger
	debug     bool
}

type SwapResult struct {
	Transaction        *solana.Transaction
	EstimatedAmountIn  uint64
	EstimatedAmountOut uint64
	PriceImpactPct     float64
	Signature          solana.Signature
}

type SwapParams struct {
	UserWalletAddress       string
	InputMintStr            string
	OutputMintStr           string
	Amount                  float32 // in lamports or token base units
	SlippageBps             float32 // basis points (1 = 0.01%)
	PriorityFee             int     // micro lamports
	DynamicComputeUnitLimit bool
	AsLegacyTx              bool
}

func NewJupiterService(jupClient *jupiter.ClientWithResponses) *JupiterService {
	var logger *slog.Logger
	logger = slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug, // This enables debug logging
		}),
	)

	return &JupiterService{
		jupClient: jupClient,
		logger:    logger,
		debug:     true,
	}
}

func (js *JupiterService) GetSwapTransaction(ctx context.Context, params SwapParams) (*SwapResult, error) {
	// 1. Get quote
	js.logDebug("Getting quote for swap",
		"inputMint", params.InputMintStr,
		"outputMint", params.OutputMintStr,
		"amount", params.Amount,
		"slippage", params.SlippageBps)

	quoteResponse, err := js.jupClient.GetQuoteWithResponse(ctx, &jupiter.GetQuoteParams{
		InputMint:   params.InputMintStr,
		OutputMint:  params.OutputMintStr,
		Amount:      params.Amount,
		SlippageBps: &params.SlippageBps,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}

	if quoteResponse.JSON200 == nil {
		return nil, fmt.Errorf("invalid quote response: %s", string(quoteResponse.Body))
	}
	quote := quoteResponse.JSON200

	// 2. Prepare swap request
	js.logDebug("Preparing swap request", "expected amount:", quote.OutAmount)

	prioritizationFeeLamports := &struct {
		JitoTipLamports              *int `json:"jitoTipLamports,omitempty"`
		PriorityLevelWithMaxLamports *struct {
			MaxLamports   *int    `json:"maxLamports,omitempty"`
			PriorityLevel *string `json:"priorityLevel,omitempty"`
		} `json:"priorityLevelWithMaxLamports,omitempty"`
	}{
		PriorityLevelWithMaxLamports: &struct {
			MaxLamports   *int    `json:"maxLamports,omitempty"`
			PriorityLevel *string `json:"priorityLevel,omitempty"`
		}{
			MaxLamports:   new(int),
			PriorityLevel: new(string),
		},
	}

	*prioritizationFeeLamports.PriorityLevelWithMaxLamports.MaxLamports = 1000
	*prioritizationFeeLamports.PriorityLevelWithMaxLamports.PriorityLevel = "high"

	swapRequest := jupiter.PostSwapJSONRequestBody{
		PrioritizationFeeLamports: prioritizationFeeLamports,
		QuoteResponse:             *quote,
		UserPublicKey:             params.UserWalletAddress,
		DynamicComputeUnitLimit:   &params.DynamicComputeUnitLimit,
	}

	// 3. Get swap transaction
	js.logDebug("Getting swap transaction")
	swapResponse, err := js.jupClient.PostSwapWithResponse(ctx, swapRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get swap transaction: %w", err)
	}

	if swapResponse.JSON200 == nil {
		return nil, fmt.Errorf("invalid swap response: %s", string(swapResponse.Body))
	}
	swap := swapResponse.JSON200

	// 4. Deserialize transaction
	js.logDebug("Deserializing transaction")
	txData, err := base64.StdEncoding.DecodeString(swap.SwapTransaction)
	if err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	var tx solana.Transaction
	err = tx.UnmarshalWithDecoder(bin.NewBinDecoder(txData))
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize transaction: %w", err)
	}

	return &SwapResult{
		Transaction: &tx,
	}, nil
}

func (js *JupiterService) logDebug(msg string, args ...any) {
	if js.debug {
		js.logger.Debug(msg, args...)
	}
}

func (js *JupiterService) logInfo(msg string, args ...any) {
	if js.debug {
		js.logger.Info(msg, args...)
	}
}
