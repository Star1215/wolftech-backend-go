package controllers

import (
	"context"
	"echo-mongo-api/configs"
	"echo-mongo-api/models"
	"echo-mongo-api/responses"
	"echo-mongo-api/services"
	"fmt"
	"log"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/ws"
	"github.com/go-redis/redis/v8"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type TokenController struct {
	helpService    *services.HelperService
	jupitorService *services.JupiterService
	jitoService    *services.JitoService
	taskService    *services.TaskService
	rpcClient      *rpc.Client
	wsClient       *ws.Client
	redisClient    *redis.Client
	logger         *slog.Logger
	debug          bool
}

func NewTokenController(
	helpService *services.HelperService,
	jupitorService *services.JupiterService,
	jitoService *services.JitoService,
	taskService *services.TaskService,
	rpcClient *rpc.Client,
	wsClient *ws.Client,
	redisClient *redis.Client,
) *TokenController {
	var logger *slog.Logger
	logger = slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug, // This enables debug logging
		}),
	)
	return &TokenController{
		helpService:    helpService,
		jupitorService: jupitorService,
		jitoService:    jitoService,
		taskService:    taskService,
		rpcClient:      rpcClient,
		wsClient:       wsClient,
		redisClient:    redisClient,
		logger:         logger,
		debug:          true,
	}
}

type TokenTransaction struct {
	Transaction  *solana.Transaction
	TokenAddress string
	TokenTicker  string
}

type BuyTokensRequest struct {
	SolAmount     float64 `json:"solAmount" validate:"required"`
	BundleID      int     `json:"bundleId" validate:"required"`
	UserPublicKey string  `json:"userPublicKey" validate:"required"`
}

const (
	LAMPORTS_PER_SOL    = 1000000000
	CHUNK_SIZE          = 4
	MAIN_RETRIES        = 1
	DEPOSIT_RETRIES     = 3
	INITIAL_RETRY_DELAY = 1 * time.Second
	BACKOFF_FACTOR      = 2
)

var userDepositCollection *mongo.Collection = configs.GetCollection(configs.DB, "user_deposits")

func (tc *TokenController) BuyTokens(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Starting buy tokens processing")
	var req BuyTokensRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, responses.TokenResponse{
			Status:  http.StatusBadRequest,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	// Validate input
	if req.SolAmount <= 0 || req.UserPublicKey == "" {
		return c.JSON(http.StatusBadRequest, responses.TokenResponse{
			Status:  http.StatusBadRequest,
			Message: "Invalid required parameters",
			Data:    &echo.Map{"data": req.UserPublicKey}, //fix it
		})
	}

	// Validate user public key
	userPubkey, err := solana.PublicKeyFromBase58(req.UserPublicKey)
	if err != nil {
		log.Printf("Invalid user wallet address: %s", userPubkey)
		return c.JSON(http.StatusBadRequest, responses.TokenResponse{
			Status:  http.StatusBadRequest,
			Message: "Invalid user public key",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	relayer, err := solana.PrivateKeyFromBase58(configs.EnvRelayerPrivateKey())
	if err != nil {
		tc.logger.Error("Failed to load relayer private key", "error", err)
		return c.JSON(http.StatusInternalServerError, responses.TokenResponse{
			Status:  http.StatusInternalServerError,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	bundleId := req.BundleID
	solDeposit := req.SolAmount
	// feeAmount := solDeposit * 0.05

	// Verify bundle exists
	bundle := bundleCollection.FindOne(ctx, bson.M{"_bid": bundleId})
	if bundle.Err() != nil {
		tc.logger.Error("Bundle not found", "bundleId", bundleId, "error", bundle.Err())
		return c.JSON(http.StatusNotFound, responses.TokenResponse{
			Status:  http.StatusNotFound,
			Message: "Bundle not found",
			Data:    &echo.Map{"data": bundle.Err().Error()},
		})
	}

	// Get memes for this bundle
	log.Printf("Querying memes for bundle ID: %d", bundleId)
	cursor, err := memeCollection.Find(ctx, bson.M{"_bid": strconv.Itoa(bundleId)})
	if err != nil {
		tc.logger.Error("Failed to find memes for bundle", "bundleId", bundleId, "error", err)
		return c.JSON(http.StatusInternalServerError, responses.TokenResponse{
			Status:  http.StatusInternalServerError,
			Message: "Memes for requested bundle not found",
			Data:    &echo.Map{"data": err.Error()},
		})
	}
	defer cursor.Close(ctx)

	var memes []models.Meme
	if err = cursor.All(ctx, &memes); err != nil {
		tc.logger.Error("Failed to decode memes", "error", err)
		return c.JSON(http.StatusInternalServerError, responses.TokenResponse{
			Status:  http.StatusInternalServerError,
			Message: "Iterating error in MongoDB",
			Data:    &echo.Map{"data": err.Error()},
		})
	}
	tc.logger.Debug("Verified tokens in bundle", "count", len(memes), "bundle", bundleId)

	// Create token accounts if needed
	accountCreationFailedTokens := make([]string, 0)
	processableTokens := make([]models.Meme, 0)
	for _, meme := range memes {
		tc.logger.Debug("Checking token account", "ticker", meme.MemeTicker)
		memeMint, err := solana.PublicKeyFromBase58(meme.MemeAddress)
		if err != nil {
			tc.logger.Error("Invalid meme address", "ticker", meme.MemeTicker, "error", err)
			accountCreationFailedTokens = append(accountCreationFailedTokens, meme.MemeTicker)
			continue
		}
		tokenAccount, _, err := tc.helpService.GetOrCreateTokenAccount(ctx, &relayer, memeMint, relayer.PublicKey())
		if err != nil {
			tc.logger.Error("Failed to get/create token account", "ticker", meme.MemeTicker, "error", err)
			accountCreationFailedTokens = append(accountCreationFailedTokens, meme.MemeTicker)
			continue
		}
		tc.logger.Debug("Token account ready", "ticker", meme.MemeTicker, "account", tokenAccount.String())
		processableTokens = append(processableTokens, meme)
	}

	cntProcessableTokens := len(processableTokens)
	tc.logger.Info("Successfully prepared for processing", "memes:", cntProcessableTokens)

	if cntProcessableTokens == 0 {
		return c.JSON(http.StatusBadRequest, responses.TokenResponse{
			Status:  http.StatusBadRequest,
			Message: "Failed to prepare all token accounts",
			Data:    &echo.Map{"data": accountCreationFailedTokens},
		})
	}
	// Calculate SOL per token
	solPerToken := uint64(solDeposit * LAMPORTS_PER_SOL / float64(len(processableTokens)))
	tc.logger.Debug("Calculated SOL per token", "solPerToken", solPerToken)

	// Phase 2: Process buy transactions
	transactionFailedTokens := make([]string, 0)
	jitoFailedTokens := make([]string, 0)
	retryMainProcessing := 0
	transactionBuffer := make([]TokenTransaction, 0)
	successfulTransactions := make([]TokenTransaction, 0)
	transactionSignatures := make([]string, 0)
	tokenAddresses := make([]string, 0)

	// Main processing loop
	for len(processableTokens) > 0 || (len(jitoFailedTokens) > 0 && retryMainProcessing < MAIN_RETRIES) {
		tc.logger.Debug("Processing loop",
			"remainingTokens", len(processableTokens),
			"jitoFailed", len(jitoFailedTokens),
			"retryCount", retryMainProcessing)
		// When remaining tokens are less than chunk size, add Jito failed tokens to queue
		if len(processableTokens) < CHUNK_SIZE && len(jitoFailedTokens) > 0 && retryMainProcessing < MAIN_RETRIES {
			retryMainProcessing++
			for _, address := range jitoFailedTokens {
				for _, token := range memes {
					if token.MemeAddress == address {
						processableTokens = append(processableTokens, token)
						break
					}
				}
			}
			jitoFailedTokens = make([]string, 0)
		}

		// Process tokens until we fill the buffer or run out
		for len(transactionBuffer) < CHUNK_SIZE && len(processableTokens) > 0 {
			token := processableTokens[0]
			processableTokens = processableTokens[1:]

			params := services.SwapParams{
				UserWalletAddress:       relayer.PublicKey().String(),
				InputMintStr:            "So11111111111111111111111111111111111111112",
				OutputMintStr:           token.MemeAddress,    // USDC
				Amount:                  float32(solPerToken), // 0.1 SOL
				SlippageBps:             50,                   // 0.5%
				PriorityFee:             1000,                 // 1000 lamports priority fee
				DynamicComputeUnitLimit: true,
			}

			result, err := tc.jupitorService.GetSwapTransaction(ctx, params)
			if err != nil {
				tc.logger.Error("Failed to create buy transaction", "token", token.MemeTicker, "error", err)
				transactionFailedTokens = append(transactionFailedTokens, token.MemeAddress)
				continue
			}
			if err != nil {
				tc.logger.Error("Failed to create buy transaction", "token", token.MemeTicker, "error", err)
				transactionFailedTokens = append(transactionFailedTokens, token.MemeAddress)
				continue
			}

			// Sign the transaction immediately
			_, err = result.Transaction.Sign(func(key solana.PublicKey) *solana.PrivateKey {
				if key == relayer.PublicKey() {
					return &relayer
				}
				return nil
			})
			if err != nil {
				tc.logger.Error("Failed to sign swap transaction", "token", token.MemeTicker, "error", err)
				transactionFailedTokens = append(transactionFailedTokens, token.MemeAddress)
				continue
			}

			_, err = tc.rpcClient.SimulateTransaction(ctx, result.Transaction)
			if err != nil {
				tc.logger.Error("Transaction simulation failed", "token", token.MemeTicker, "error", err)
				transactionFailedTokens = append(transactionFailedTokens, token.MemeAddress)
				continue
			}
			tc.logDebug("Transaction simulation success", "token", token.MemeTicker)

			// Only add to buffer if signed successfully
			if len(result.Transaction.Signatures) > 0 {
				transactionBuffer = append(transactionBuffer, TokenTransaction{
					Transaction:  result.Transaction,
					TokenAddress: token.MemeAddress,
					TokenTicker:  token.MemeTicker,
				})
			} else {
				tc.logger.Error("Transaction not signed", "token", token.MemeTicker)
				transactionFailedTokens = append(transactionFailedTokens, token.MemeAddress)
			}
		}

		// Submit bundle when we have a full chunk or no more tokens to process
		if len(transactionBuffer) == CHUNK_SIZE || (len(processableTokens) == 0 && len(transactionBuffer) > 0) {
			// Submit bundle via Jito

			// Create tip transaction
			tipAmount := uint64(100000) // 0.00001 SOL as tip
			tipTx, err := tc.helpService.CreateTipTransaction(
				ctx,
				relayer.PublicKey(),
				tipAmount,
				solana.MustPublicKeyFromBase58(configs.EnvJitoTipAccount()),
			)
			if err != nil {
				tc.logger.Error("Failed to create tip transaction", "error", err)
				// Mark all transactions in buffer as failed
				for _, tx := range transactionBuffer {
					jitoFailedTokens = append(jitoFailedTokens, tx.TokenAddress)
				}
				transactionBuffer = make([]TokenTransaction, 0)
				continue
			}

			// Sign the tip transaction
			_, err = tipTx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
				if key == relayer.PublicKey() {
					return &relayer
				}
				return nil
			})
			if err != nil {
				tc.logger.Error("Failed to sign tip transaction", "error", err)
				for _, tx := range transactionBuffer {
					jitoFailedTokens = append(jitoFailedTokens, tx.TokenAddress)
				}
				transactionBuffer = make([]TokenTransaction, 0)
				continue
			}

			bundleTransactions := make([]solana.Transaction, 0, len(transactionBuffer)+1)
			bundleTransactions = append(bundleTransactions, *tipTx)
			for _, tx := range transactionBuffer {
				bundleTransactions = append(bundleTransactions, *tx.Transaction)
			}

			bundleResult, err := tc.jitoService.SubmitBundle(ctx, bundleTransactions)
			if err != nil {
				tc.logger.Error("Failed to submit bundle", "error", err)
				// Record failed tokens
				for _, tx := range transactionBuffer {
					jitoFailedTokens = append(jitoFailedTokens, tx.TokenAddress)
				}
			} else {
				tc.logger.Info("Bundle submitted successfully",
					"bundleId", bundleResult.BundleID,
					"status", bundleResult.Status,
					"numTransactions", len(bundleTransactions),
					"tokens", func() []string {
						var tickers []string
						for _, tx := range transactionBuffer {
							tickers = append(tickers, tx.TokenTicker)
						}
						return tickers
					}())
				// Track successful transactions and their balances
				for _, tx := range transactionBuffer {
					successfulTransactions = append(successfulTransactions, tx)
					transactionSignatures = append(transactionSignatures, tx.Transaction.Signatures[0].String())
					tokenAddresses = append(tokenAddresses, tx.TokenAddress)
				}

			}
			transactionBuffer = make([]TokenTransaction, 0)
		}
	}

	// Phase 3: Process remaining failed tokens via normal RPC
	finalFailedTokens := make([]string, 0)
	finalFailedTokens = append(finalFailedTokens, transactionFailedTokens...)
	finalFailedTokens = append(finalFailedTokens, jitoFailedTokens...)

	tokensToRetry := make([]models.Meme, 0)
	for _, address := range finalFailedTokens {
		for _, token := range memes {
			if token.MemeAddress == address {
				tokensToRetry = append(tokensToRetry, token)
				break
			}
		}
	}

	// Create background task for processing the successful transactions
	if len(successfulTransactions) > 0 {
		taskPayload := models.BuyTokensTaskPayload{
			BundleID:       bundleId,
			UserPublicKey:  req.UserPublicKey,
			SolAmount:      req.SolAmount,
			TransactionIDs: transactionSignatures,
			TokenAddresses: tokenAddresses,
		}

		taskID, err := tc.taskService.CreateTask(ctx, "buy_tokens", taskPayload)
		if err != nil {
			tc.logger.Error("Failed to create background task", "error", err)
			// Still return success since swaps were completed
		}

		return c.JSON(http.StatusOK, responses.TokenResponse{
			Status:  http.StatusOK,
			Message: "success",
			Data: &echo.Map{
				"data":            "Token swaps completed successfully",
				"taskId":          taskID,
				"transactions":    transactionSignatures,
				"processedTokens": len(successfulTransactions),
			},
		})
	}

	return c.JSON(http.StatusOK, responses.TokenResponse{
		Status:  http.StatusOK,
		Message: "success",
		Data:    &echo.Map{"data": "Token purchase initiated"},
	})
}

// controllers/token_controller.go
func (tc *TokenController) GetTaskStatus(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	taskID := c.Param("taskId")
	if taskID == "" {
		return c.JSON(http.StatusBadRequest, responses.TokenResponse{
			Status:  http.StatusBadRequest,
			Message: "error",
			Data:    &echo.Map{"data": "task ID is required"},
		})
	}

	task, err := tc.taskService.GetTaskStatus(ctx, taskID)
	if err != nil {
		return c.JSON(http.StatusNotFound, responses.TokenResponse{
			Status:  http.StatusNotFound,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	return c.JSON(http.StatusOK, responses.TokenResponse{
		Status:  http.StatusOK,
		Message: "success",
		Data:    &echo.Map{"data": task},
	})
}

func (tc *TokenController) processBalanceChanges(
	ctx context.Context,
	transactions []TokenTransaction,
	relayerPubkey solana.PublicKey,
	bundleId int,
	userWallet string,
) error {
	// Get bundle details from database
	var bundle models.Bundle
	err := bundleCollection.FindOne(ctx, bson.M{"_bid": bundleId}).Decode(&bundle)
	if err != nil {
		tc.logger.Error("failed to find bundle: ", "error", err)
		return fmt.Errorf("failed to find bundle: %w", err)
	}

	// Get existing user deposit if any
	var existingDeposit models.UserDeposit
	err = userDepositCollection.FindOne(ctx, bson.M{"wallet": userWallet}).Decode(&existingDeposit)
	if err != nil && err != mongo.ErrNoDocuments {
		return fmt.Errorf("failed to query user deposit: %w", err)
	}

	// Find existing bundle holding if any
	var existingHolding *models.UserHolding
	for i, deposit := range existingDeposit.Deposits {
		if deposit.BundleID == bundleId {
			existingHolding = &existingDeposit.Deposits[i]
			break
		}
	}

	// Prepare token allocations map
	tokenAllocations := make(map[string]models.TokenAllocation)

	// Initialize with existing allocations if any
	if existingHolding != nil {
		for _, alloc := range existingHolding.Tokens {
			tokenAllocations[alloc.Mint] = alloc
		}
	}

	// Process transactions to calculate actual SOL spent per token
	tokenSOLSpent := make(map[string]float64)
	totalTokenSOLSpent := 0.0

	for _, tx := range transactions {
		txDetails, err := tc.rpcClient.GetTransaction(ctx, tx.Transaction.Signatures[0], nil)
		if err != nil || txDetails.Meta == nil {
			continue // Skip failed transactions
		}

		// Calculate SOL spent in this transaction (preBalance - postBalance - fee)
		solSpent := float64(txDetails.Meta.PreBalances[0]-txDetails.Meta.PostBalances[0])/float64(solana.LAMPORTS_PER_SOL) -
			float64(txDetails.Meta.Fee)/float64(solana.LAMPORTS_PER_SOL)

		// Distribute SOL spent among tokens in this transaction
		tokensInTx := make(map[string]bool)
		for _, tokenBalance := range txDetails.Meta.PostTokenBalances {
			if tokenBalance.Owner == nil || *tokenBalance.Owner != relayerPubkey {
				continue
			}
			tokensInTx[tokenBalance.Mint.String()] = true
		}

		solPerTokenInTx := solSpent / float64(len(tokensInTx))
		for mint := range tokensInTx {
			tokenSOLSpent[mint] += solPerTokenInTx
			totalTokenSOLSpent += solPerTokenInTx
		}
	}

	// Update token allocations with new purchases
	for _, memeInfo := range bundle.BundleMemesInfo {
		mint := memeInfo.TokenAddress
		currentAlloc, exists := tokenAllocations[mint]
		if !exists {
			currentAlloc = models.TokenAllocation{
				Mint:   mint,
				Ticker: memeInfo.TokenTicker,
			}
		}

		// Get token amount from transactions
		amount := "0"
		for _, tx := range transactions {
			txDetails, err := tc.rpcClient.GetTransaction(ctx, tx.Transaction.Signatures[0], nil)
			if err != nil || txDetails.Meta == nil {
				continue
			}

			for _, tokenBalance := range txDetails.Meta.PostTokenBalances {
				if tokenBalance.Mint.String() == mint && tokenBalance.Owner != nil && *tokenBalance.Owner == relayerPubkey {
					amount = tokenBalance.UiTokenAmount.Amount
					break
				}
			}
		}

		// Calculate pending amount
		pendingAmount := amount
		if exists {
			// If existing allocation, add to pending amount
			pendingAmountInt := new(big.Int)
			pendingAmountInt.SetString(currentAlloc.PendingAmount, 10)
			newAmountInt := new(big.Int)
			newAmountInt.SetString(amount, 10)
			pendingAmountInt.Add(pendingAmountInt, newAmountInt)
			pendingAmount = pendingAmountInt.String()
		}

		// Update allocation
		currentAlloc.PendingAmount = pendingAmount
		currentAlloc.Pending = true
		if spent, ok := tokenSOLSpent[mint]; ok {
			currentAlloc.PurchasedSOL += spent
		}
		tokenAllocations[mint] = currentAlloc
	}

	// Convert map to slice for storage
	var allocations []models.TokenAllocation
	for _, alloc := range tokenAllocations {
		allocations = append(allocations, alloc)
	}

	// Calculate pending status for the bundle
	pendingBundle := false
	for _, alloc := range allocations {
		if alloc.Pending {
			pendingBundle = true
			break
		}
	}

	// Create or update user holding for this bundle
	userHolding := models.UserHolding{
		BundleID:    bundleId,
		BundleName:  bundle.BundleMeta.BundleName,
		InvestedSOL: existingHolding.InvestedSOL + totalTokenSOLSpent,
		Tokens:      allocations,
		Pending:     pendingBundle,
		Active:      true,
		Timestamp:   time.Now(),
	}

	// Start MongoDB session
	session, err := configs.DB.StartSession()
	if err != nil {
		tc.logger.Error("failed to start MongoDB session: ", "error", err)
		return fmt.Errorf("failed to start MongoDB session: %w", err)
	}
	defer session.EndSession(ctx)

	err = session.StartTransaction()
	if err != nil {
		tc.logger.Error("failed to start transaction: ", "error", err)
		return fmt.Errorf("failed to start transaction: %w", err)
	}

	// Prepare update operation
	update := bson.M{
		"$inc": bson.M{"totalInvestedSol": totalTokenSOLSpent},
		"$set": bson.M{"lastUpdated": time.Now(), "pending": pendingBundle},
	}

	// Add to deposits if new, or replace if existing
	if existingHolding == nil {
		update["$push"] = bson.M{"deposits": userHolding}
	} else {
		update["$set"].(bson.M)["deposits.$[elem]"] = userHolding
	}

	// Use arrayFilters for updating specific array element
	opts := options.Update().SetUpsert(true)
	if existingHolding != nil {
		opts.SetArrayFilters(options.ArrayFilters{
			Filters: []interface{}{bson.M{"elem.bundleId": bundleId}},
		})
	}

	_, err = userDepositCollection.UpdateOne(
		ctx,
		bson.M{"wallet": userWallet},
		update,
		opts,
	)

	if err != nil {
		session.AbortTransaction(ctx)
		return fmt.Errorf("failed to update user deposit: %w", err)
	}

	if err := session.CommitTransaction(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	tc.logger.Info("Successfully updated user holdings",
		"wallet", userWallet,
		"bundleID", bundleId,
		"tokensPurchased", len(allocations),
		"totalInvestedSOL", totalTokenSOLSpent,
		"pendingStatus", pendingBundle)

	return nil
}

func (tc *TokenController) logDebug(msg string, args ...any) {
	if tc.debug {
		tc.logger.Debug(msg, args...)
	}
}

func (tc *TokenController) logInfo(msg string, args ...any) {
	if tc.debug {
		tc.logger.Info(msg, args...)
	}
}
